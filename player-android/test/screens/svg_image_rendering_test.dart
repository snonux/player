// Screen-level tests for SVG rendering and image error states (task a83).
//
// The server returns the original SVG for both the stream and the thumbnail
// of an SVG file. These tests check that every screen showing such an image
// paints it as a vector with the right credentials, and that an image that
// cannot be decoded (malformed SVG, corrupt bitmap) shows a visible error.
//
// SVG downloads are scripted through `svgFetcherProvider`; parsing, decoding
// and painting are real. Bitmap tests run the real decoders against a fake
// HTTP server (see real_image_loading.dart).
//
// Run with: flutter test test/screens/svg_image_rendering_test.dart

import 'package:cached_network_image/cached_network_image.dart';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/app_routes.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/public_api_client_provider.dart';
import 'package:player_android/screens/folder_browser_screen.dart';
import 'package:player_android/screens/home_screen.dart';
import 'package:player_android/screens/image_viewer_screen.dart';
import 'package:player_android/screens/media_detail_screen.dart';
import 'package:player_android/screens/media_grid_screen.dart';
import 'package:player_android/screens/share_viewer_screen.dart';
import 'package:player_android/services/svg_fetcher.dart';
import 'package:player_android/widgets/network_svg_image.dart';

import '../support/fake_http.dart';
import '../support/real_image_loading.dart';
import '../support/svg_test_support.dart';

const _base = 'https://player.example';
const _shareStream = '$_base/s/tok/stream';
const _errorKey = Key('image_viewer_error');
final _picture = find.byType(SvgPictureBox);

const _svgJson = <String, dynamic>{
  'id': 9,
  'set_id': 1,
  'rel_path': 'sample-svg.svg',
  'file_name': 'sample-svg.svg',
  'abs_path': '/media/sample-svg.svg',
  'type': 'image',
  'codec': 'svg',
};

final _svgMedia = Media.fromJson(_svgJson);
final _photo = Media.fromJson({..._svgJson, 'file_name': 'photo.jpg'});

class _TokenStorage implements TokenStorage {
  @override
  Future<String?> readToken() async => 'pt-svg-test';
  @override
  Future<void> writeToken(String token) async {}
  @override
  Future<void> deleteToken() async {}
}

/// Library client holding one image and one set whose thumbnail, stream and
/// cover URLs exist. [withFolder] adds a folder with a cover; covers load
/// through the bitmap cache, which only one test per file may use.
class _Client extends PlayerApiClient {
  _Client({Media? media, this.withFolder = false})
      : media = media ?? _svgMedia,
        super(dio: Dio());

  final Media media;
  final bool withFolder;

  @override
  Future<List<Media>> listMedia({
    String? search,
    int? setId,
    List<int>? setIds,
    String? type,
    bool? favorites,
    List<String>? tags,
    double? minDuration,
    double? maxDuration,
    int? fileSizeMin,
    int? fileSizeMax,
    String? sort,
    int? limit,
    int? offset,
    String? folder,
    String? parent,
  }) async =>
      [media];

  @override
  Future<Map<String, dynamic>> browseSet(int setId, {String? parent}) async => {
        'current_path': '',
        'folders': [
          if (withFolder) {'name': 'Icons', 'has_cover': true},
        ],
        'media': [_svgJson],
      };

  @override
  Future<List<MediaSet>> listSets() async => const [
        MediaSet(
          id: 1,
          name: 'Drawings',
          rootPath: 'drawings',
          coverThumbnailPath: '',
          isPodcast: false,
        ),
      ];

  @override
  Future<Media> getMedia(int mediaId) async => media;

  @override
  Future<List<Tag>> listTags() async => [];

  @override
  String thumbnailUrl(int mediaId) => '$_base/api/v1/media/$mediaId/thumbnail';

  @override
  String streamUrl(int mediaId) => '$_base/api/v1/media/$mediaId/stream';

  @override
  String setFolderCoverUrl(int setId, {String? folder}) =>
      '$_base/api/v1/sets/$setId/cover${folder == null ? '' : '/$folder'}';
}

/// Public client returning the share page of an SVG image.
class _PublicClient extends PlayerApiClient {
  _PublicClient() : super(dio: Dio(BaseOptions(baseUrl: _base)));

  @override
  String get baseUrl => _base;

  @override
  Future<String> getSharedMediaPage(String token) async => '''
{
  "media": {"id": 9, "file_name": "sample-svg.svg", "type": "image"},
  "has_thumb": true,
  "stream_url": "/s/tok/stream",
  "download_url": "/s/tok/download",
  "thumb_url": "/s/tok/thumbnail"
}''';
}

/// A screen showing protected library images of a signed-in account.
Widget _library(
  Widget home,
  RecordingSvgFetcher fetcher, {
  Media? media,
  bool withFolder = false,
}) =>
    ProviderScope(
      // A new scope per screen, so no provider state leaks between them.
      key: UniqueKey(),
      overrides: [
        apiClientProvider
            .overrideWithValue(_Client(media: media, withFolder: withFolder)),
        playerBaseUrlProvider.overrideWithValue(Uri.parse(_base)),
        tokenStorageProvider.overrideWithValue(_TokenStorage()),
        cookieJarProvider.overrideWithValue(CookieJar()),
        credentialMutationQueueProvider.overrideWithValue(
          CredentialMutationQueue(credentialsEnabled: true),
        ),
        svgFetcherProvider.overrideWithValue(fetcher.fetcher),
      ],
      child: home is MaterialApp ? home : MaterialApp(home: home),
    );

/// The public share image viewer; [title] is the file name, when known.
Widget _publicViewer(RecordingSvgFetcher fetcher, {String? title}) =>
    ProviderScope(
      overrides: [svgFetcherProvider.overrideWithValue(fetcher.fetcher)],
      child: MaterialApp(
        home: ImageViewerScreen(
          mediaId: '0',
          imageUrl: _shareStream,
          mediaTitle: title,
          isPublicShare: true,
        ),
      ),
    );

/// Asserts that the SVG below [boundary] is really painted: red in the
/// middle. [boundary] must match a `RepaintBoundary`.
Future<void> _expectPaintedRed(WidgetTester tester, Finder boundary) async {
  expect(await pixelAt(tester, boundary.first), kSvgRed);
}

/// The nearest `RepaintBoundary` above the SVG picture.
Finder get _pictureBoundary =>
    find.ancestor(of: _picture, matching: find.byType(RepaintBoundary));

void main() {
  group('image viewer, SVG', _viewerSvgTests);
  group('public image viewer, SVG', _publicViewerSvgTests);
  group('image viewer, bitmaps', _viewerBitmapTests);
  group('library thumbnails', _thumbnailTests);
  group('share viewer thumbnail', _shareThumbnailTests);
  group('cached bitmap pipeline', _cachedBitmapTests);
}

void _viewerSvgTests() {
  testWidgets('library SVG is painted from the authenticated stream',
      (tester) async {
    final fetcher = RecordingSvgFetcher();
    await tester
        .pumpWidget(_library(const ImageViewerScreen(mediaId: '9'), fetcher));
    await pumpUntilFound(tester, _picture);

    expect(find.byType(CachedNetworkImage), findsNothing);
    expect(find.byKey(_errorKey), findsNothing);
    await _expectPaintedRed(tester, _pictureBoundary);
    final request = fetcher.requests.single;
    expect(request.uri.toString(), '$_base/api/v1/media/9/stream');
    expect(request.headers['Authorization'], 'Bearer pt-svg-test');
  });

  testWidgets('a small SVG fills the viewer instead of staying tiny',
      (tester) async {
    await tester.pumpWidget(
        _library(const ImageViewerScreen(mediaId: '9'), RecordingSvgFetcher()));
    await pumpUntilFound(tester, _picture);

    // The 10x10 drawing is laid out in the whole body below the app bar.
    final size = tester.getSize(_picture);
    expect(size.width, 800);
    expect(size.height, greaterThan(500));
  });

  testWidgets('library malformed SVG shows the error state', (tester) async {
    await tester.pumpWidget(_library(const ImageViewerScreen(mediaId: '9'),
        RecordingSvgFetcher(body: kMalformedSvg)));
    await pumpUntilFound(tester, find.byKey(_errorKey));

    expect(find.text('Image unavailable'), findsOneWidget);
    expect(find.byIcon(Icons.broken_image_outlined), findsOneWidget);
    expect(_picture, findsNothing);
  });
}

void _publicViewerSvgTests() {
  testWidgets('public SVG share is painted without credentials',
      (tester) async {
    final fetcher = RecordingSvgFetcher();
    await tester.pumpWidget(_publicViewer(fetcher, title: 'sample-svg.svg'));
    await pumpUntilFound(tester, _picture);

    await _expectPaintedRed(tester, _pictureBoundary);
    expect(fetcher.requests.single.uri.toString(), _shareStream);
    expect(fetcher.requests.single.headers, isEmpty);
  });

  testWidgets('public malformed SVG shows the error state', (tester) async {
    await tester.pumpWidget(_publicViewer(
        RecordingSvgFetcher(body: kMalformedSvg),
        title: 'sample-svg.svg'));
    await pumpUntilFound(tester, find.byKey(_errorKey));

    expect(find.text('Image unavailable'), findsOneWidget);
    expect(_picture, findsNothing);
  });
}

/// The public viewer loads bitmaps with `Image.network`; these tests serve
/// real bytes to the real decoder.
void _viewerBitmapTests() {
  useRealImageLoading();

  testWidgets('a bitmap that fails to decode shows the error state',
      (tester) async {
    // A 200 response whose body is not an image in any supported format.
    final server = startRealImageLoading(
        tester, (_, __) => FakeHttpReply(List.filled(64, 0x42)));
    final fetcher = RecordingSvgFetcher(body: 'BBBB');
    await tester.pumpWidget(_publicViewer(fetcher, title: 'photo.jpg'));
    await pumpUntilFound(tester, find.byKey(_errorKey));

    // The body was downloaded, so the error comes from decoding; the
    // content was then checked for SVG once, without credentials.
    expect(server.requests.single.$1.toString(), _shareStream);
    expect(fetcher.requests.single.headers, isEmpty);
    expect(find.text('Image unavailable'), findsOneWidget);
    expect(_picture, findsNothing);
    await settleImageCache(tester);
  });

  testWidgets('an SVG share opened without its file name is still painted',
      (tester) async {
    // After route restoration the title is gone; the content decides.
    startRealImageLoading(
        tester, (_, __) => FakeHttpReply(svgBytes(kValidSvg)));
    await tester.pumpWidget(_publicViewer(RecordingSvgFetcher()));
    await pumpUntilFound(tester, _picture);

    expect(find.byKey(_errorKey), findsNothing);
    await _expectPaintedRed(tester, _pictureBoundary);
    await settleImageCache(tester);
  });

  testWidgets('a small bitmap keeps its own size', (tester) async {
    final png = (await tester.runAsync(() => makePng(20, 10)))!;
    startRealImageLoading(tester, (_, __) => FakeHttpReply(png));
    await tester
        .pumpWidget(_publicViewer(RecordingSvgFetcher(), title: 'photo.png'));
    final decoded = find.byWidgetPredicate(
        (widget) => widget is RawImage && widget.image != null);
    await pumpUntilFound(tester, decoded);

    // Not stretched to the 800x600 test screen.
    expect(tester.getSize(decoded), const Size(20, 10));
    await settleImageCache(tester);
  });
}

/// Media grid whose cards open the media detail screen.
GoRouter _gridAndDetailRouter() => GoRouter(
      initialLocation: AppRoutes.mediaGridPath(1),
      routes: [
        GoRoute(
          path: AppRoutes.mediaGrid,
          builder: (_, __) => const MediaGridScreen(setId: 1),
        ),
        GoRoute(
          path: AppRoutes.mediaDetail,
          builder: (_, state) =>
              MediaDetailScreen(mediaId: state.pathParameters['id']!),
        ),
      ],
    );

void _thumbnailTests() {
  testWidgets('grid card and detail preview paint the SVG thumbnail',
      (tester) async {
    final fetcher = RecordingSvgFetcher();
    final router = _gridAndDetailRouter();
    addTearDown(router.dispose);
    await tester.pumpWidget(
        _library(MaterialApp.router(routerConfig: router), fetcher));
    final card = find.byKey(const Key('media_card_9'));
    await pumpUntilFound(tester, find.descendant(of: card, matching: _picture));
    await _expectPaintedRed(tester, _pictureBoundary);

    await tester.tap(card);
    final banner = find.descendant(
        of: find.byKey(const Key('media_detail_thumbnail')),
        matching: _picture);
    await pumpUntilFound(tester, banner);

    expect(find.byType(CachedNetworkImage), findsNothing);
    // Both screens show the same thumbnail: one download, with credentials.
    final request = fetcher.requests.single;
    expect(request.uri.toString(), '$_base/api/v1/media/9/thumbnail');
    expect(request.headers['Authorization'], 'Bearer pt-svg-test');
  });

  testWidgets('grid card shows the broken-image icon for a malformed SVG',
      (tester) async {
    await tester.pumpWidget(_library(const MediaGridScreen(setId: 1),
        RecordingSvgFetcher(body: kMalformedSvg)));
    final broken = find.descendant(
      of: find.byKey(const Key('media_card_9')),
      matching: find.byIcon(Icons.broken_image_outlined),
    );
    await pumpUntilFound(tester, broken);

    expect(_picture, findsNothing);
  });

  testWidgets('folder browser tile paints the SVG thumbnail', (tester) async {
    await tester.pumpWidget(
        _library(const FolderBrowserScreen(setId: 1), RecordingSvgFetcher()));
    final tile = find.descendant(
        of: find.byKey(const Key('media_thumb_9')), matching: _picture);
    await pumpUntilFound(tester, tile);

    expect(tester.getSize(tile), const Size(48, 48));
  });
}

void _shareThumbnailTests() {
  Widget share(RecordingSvgFetcher fetcher) => ProviderScope(
        overrides: [
          publicApiClientProvider.overrideWithValue(_PublicClient()),
          svgFetcherProvider.overrideWithValue(fetcher.fetcher),
        ],
        child: const MaterialApp(home: ShareViewerScreen(token: 'tok')),
      );

  testWidgets('SVG share paints its thumbnail without credentials',
      (tester) async {
    final fetcher = RecordingSvgFetcher();
    await tester.pumpWidget(share(fetcher));
    await pumpUntilFound(tester, _picture);

    expect(find.byKey(const Key('share_viewer_thumbnail')), findsOneWidget);
    await _expectPaintedRed(tester, _pictureBoundary);
    expect(fetcher.requests.single.uri.toString(), '$_base/s/tok/thumbnail');
    expect(fetcher.requests.single.headers, isEmpty);
  });

  testWidgets('malformed SVG thumbnail falls back to the type icon',
      (tester) async {
    await tester.pumpWidget(share(RecordingSvgFetcher(body: kMalformedSvg)));
    final fallback =
        find.byKey(const Key('share_viewer_thumbnail_placeholder'));
    await pumpUntilFound(tester, fallback);

    expect(_picture, findsNothing);
  });
}

/// Library bitmaps and covers load through `CachedNetworkImage`. The
/// scenarios share one test because only one test per file may use its disk
/// cache (see real_image_loading.dart).
void _cachedBitmapTests() {
  useRealImageLoading();

  testWidgets('corrupt library bitmap, SVG folder cover and SVG set cover',
      (tester) async {
    final garbage = FakeHttpReply(List.filled(64, 0x42));
    final svg = FakeHttpReply(svgBytes(kValidSvg));
    final server = startRealImageLoading(
        tester, (uri, _) => uri.path.contains('/cover') ? svg : garbage);

    // A library bitmap the decoder rejects ends in the labelled error.
    await tester.pumpWidget(_library(const ImageViewerScreen(mediaId: '9'),
        RecordingSvgFetcher(body: 'BBBB'),
        media: _photo));
    await pumpUntilFound(tester, find.byKey(_errorKey));
    expect(server.requests.single.$2['authorization'], 'Bearer pt-svg-test');
    expect(find.text('Image unavailable'), findsOneWidget);
    expect(find.byIcon(Icons.broken_image_outlined), findsOneWidget);

    // Covers have no file name; an SVG cover is recognised by content.
    var fetcher = RecordingSvgFetcher();
    await tester.pumpWidget(_library(
        const FolderBrowserScreen(setId: 1), fetcher,
        withFolder: true));
    final cover = find.descendant(
        of: find.byKey(const Key('folder_cover_Icons')), matching: _picture);
    await pumpUntilFound(tester, cover);
    expect([for (final r in fetcher.requests) r.uri.path],
        contains('/api/v1/sets/1/cover/Icons'));

    fetcher = RecordingSvgFetcher();
    await tester.pumpWidget(_library(const SetsListScreen(), fetcher));
    await pumpUntilFound(tester, _picture);
    expect(fetcher.requests.single.uri.path, '/api/v1/sets/1/cover');
    expect(
        fetcher.requests.single.headers['Authorization'], 'Bearer pt-svg-test');
    await settleImageCache(tester);
  });
}
