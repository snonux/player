// Screen-level tests for SVG rendering and image error states (task a83).
//
// The server returns the original SVG for both the stream and the thumbnail
// of an SVG file. These tests check that every screen showing such an image
// draws it as a vector with the right credentials, and that an image that
// cannot be decoded (malformed SVG, corrupt bitmap) shows a visible error.
//
// Run with: flutter test test/screens/svg_image_rendering_test.dart

import 'dart:async';
import 'dart:io';

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
import 'package:player_android/screens/image_viewer_screen.dart';
import 'package:player_android/screens/media_detail_screen.dart';
import 'package:player_android/screens/media_grid_screen.dart';
import 'package:player_android/screens/share_viewer_screen.dart';
import 'package:player_android/widgets/network_svg_image.dart';
import 'package:vector_graphics/vector_graphics.dart';

import '../support/svg_test_support.dart';

const _base = 'https://player.example';
const _shareStream = '$_base/s/tok/stream';
const _errorKey = Key('image_viewer_error');

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

class _TokenStorage implements TokenStorage {
  @override
  Future<String?> readToken() async => 'pt-svg-test';
  @override
  Future<void> writeToken(String token) async {}
  @override
  Future<void> deleteToken() async {}
}

/// Library client holding one image whose thumbnail and stream exist.
class _Client extends PlayerApiClient {
  _Client({Media? media})
      : media = media ?? _svgMedia,
        super(dio: Dio());

  final Media media;

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
        'folders': <Map<String, dynamic>>[],
        'media': [_svgJson],
      };

  @override
  Future<Media> getMedia(int mediaId) async => media;

  @override
  Future<List<Tag>> listTags() async => [];

  @override
  String thumbnailUrl(int mediaId) => '$_base/api/v1/media/$mediaId/thumbnail';

  @override
  String streamUrl(int mediaId) => '$_base/api/v1/media/$mediaId/stream';
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

/// Serves [body] with status 200 to `Image.network`, so the test reaches the
/// real bitmap decoder instead of stopping at an HTTP error.
class _BytesHttpClient implements HttpClient {
  _BytesHttpClient(this.body);

  final List<int> body;
  final List<Uri> requests = [];

  @override
  bool autoUncompress = true;

  @override
  Future<HttpClientRequest> getUrl(Uri url) async {
    requests.add(url);
    return _BytesRequest(body);
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => null;
}

class _BytesRequest implements HttpClientRequest {
  _BytesRequest(this.body);

  final List<int> body;

  @override
  HttpHeaders get headers => _IgnoredHeaders();

  @override
  Future<HttpClientResponse> close() async => _BytesResponse(body);

  @override
  dynamic noSuchMethod(Invocation invocation) => null;
}

class _IgnoredHeaders implements HttpHeaders {
  @override
  dynamic noSuchMethod(Invocation invocation) => null;
}

class _BytesResponse extends Stream<List<int>> implements HttpClientResponse {
  _BytesResponse(this.body);

  final List<int> body;

  @override
  int get statusCode => HttpStatus.ok;

  @override
  int get contentLength => body.length;

  @override
  HttpClientResponseCompressionState get compressionState =>
      HttpClientResponseCompressionState.notCompressed;

  @override
  StreamSubscription<List<int>> listen(
    void Function(List<int> event)? onData, {
    Function? onError,
    void Function()? onDone,
    bool? cancelOnError,
  }) =>
      Stream<List<int>>.value(body).listen(onData,
          onError: onError, onDone: onDone, cancelOnError: cancelOnError);

  @override
  dynamic noSuchMethod(Invocation invocation) => null;
}

/// Overrides for screens that load protected library images.
List<Override> _libraryOverrides(RecordingSvgFetcher fetcher, {Media? media}) =>
    [
      apiClientProvider.overrideWithValue(_Client(media: media)),
      playerBaseUrlProvider.overrideWithValue(Uri.parse(_base)),
      tokenStorageProvider.overrideWithValue(_TokenStorage()),
      cookieJarProvider.overrideWithValue(CookieJar()),
      credentialMutationQueueProvider.overrideWithValue(
        CredentialMutationQueue(credentialsEnabled: true),
      ),
      svgFetcherProvider.overrideWithValue(fetcher.fetcher),
    ];

Future<void> _pumpPublicViewer(
  WidgetTester tester, {
  required String? title,
  RecordingSvgFetcher? fetcher,
}) =>
    tester.pumpWidget(ProviderScope(
      overrides: [
        if (fetcher != null)
          svgFetcherProvider.overrideWithValue(fetcher.fetcher),
      ],
      child: MaterialApp(
        home: ImageViewerScreen(
          mediaId: '0',
          imageUrl: _shareStream,
          mediaTitle: title,
          isPublicShare: true,
        ),
      ),
    ));

void main() {
  group('image viewer', () {
    testWidgets('library SVG renders from the authenticated stream',
        (tester) async {
      final fetcher = RecordingSvgFetcher();
      await tester.pumpWidget(ProviderScope(
        overrides: _libraryOverrides(fetcher),
        child: const MaterialApp(home: ImageViewerScreen(mediaId: '9')),
      ));
      await pumpUntilFound(tester, find.byType(VectorGraphic));

      expect(find.byType(VectorGraphic), findsOneWidget);
      expect(find.byType(CachedNetworkImage), findsNothing);
      expect(find.byKey(_errorKey), findsNothing);
      final (uri, headers) = fetcher.requests.single;
      expect(uri.toString(), '$_base/api/v1/media/9/stream');
      expect(headers['Authorization'], 'Bearer pt-svg-test');
    });

    testWidgets('library malformed SVG shows the error state', (tester) async {
      await tester.pumpWidget(ProviderScope(
        overrides: _libraryOverrides(RecordingSvgFetcher(body: kMalformedSvg)),
        child: const MaterialApp(home: ImageViewerScreen(mediaId: '9')),
      ));
      await pumpUntilFound(tester, find.byKey(_errorKey));

      expect(find.byKey(_errorKey), findsOneWidget);
      expect(find.text('Image unavailable'), findsOneWidget);
      expect(find.byIcon(Icons.broken_image_outlined), findsOneWidget);
    });

    testWidgets('public SVG share renders without credentials', (tester) async {
      final fetcher = RecordingSvgFetcher();
      await _pumpPublicViewer(tester,
          title: 'sample-svg.svg', fetcher: fetcher);
      await pumpUntilFound(tester, find.byType(VectorGraphic));

      expect(find.byType(VectorGraphic), findsOneWidget);
      final (uri, headers) = fetcher.requests.single;
      expect(uri.toString(), _shareStream);
      expect(headers, isEmpty);
    });

    testWidgets('public malformed SVG shows the error state', (tester) async {
      await _pumpPublicViewer(tester,
          title: 'sample-svg.svg',
          fetcher: RecordingSvgFetcher(body: kMalformedSvg));
      await pumpUntilFound(tester, find.byKey(_errorKey));

      expect(find.byKey(_errorKey), findsOneWidget);
      expect(find.byType(VectorGraphic), findsNothing);
    });

    testWidgets('bitmap that fails to decode shows the error state',
        (tester) async {
      // A 200 response whose body is not an image in any supported format.
      final client = _BytesHttpClient(List<int>.filled(64, 0x42));
      await HttpOverrides.runZoned(
        () async {
          await _pumpPublicViewer(tester, title: 'photo.jpg');
          await pumpUntilFound(tester, find.byKey(_errorKey));
        },
        createHttpClient: (_) => client,
      );

      // The body was really downloaded, so the error comes from decoding.
      expect(client.requests.single.toString(), _shareStream);
      expect(find.byType(VectorGraphic), findsNothing);
      expect(find.byKey(_errorKey), findsOneWidget);
      expect(find.text('Image unavailable'), findsOneWidget);
    });

    testWidgets('library bitmap error builder is the visible error state',
        (tester) async {
      final fetcher = RecordingSvgFetcher();
      final photo = Media.fromJson({..._svgJson, 'file_name': 'photo.jpg'});
      await tester.pumpWidget(ProviderScope(
        overrides: _libraryOverrides(fetcher, media: photo),
        child: const MaterialApp(home: ImageViewerScreen(mediaId: '9')),
      ));
      await tester.pump();
      await tester.pump();
      expect(fetcher.requests, isEmpty);

      // The bitmap cache needs platform plugins that tests do not have, so
      // the decode failure itself cannot be produced here; check instead
      // that such a failure is routed to the labelled error state.
      final image =
          tester.widget<CachedNetworkImage>(find.byType(CachedNetworkImage));
      final context = tester.element(find.byType(CachedNetworkImage));
      final error = image.errorWidget!(context, image.imageUrl, 'decode');
      await tester.pumpWidget(MaterialApp(home: error));

      expect(find.byKey(_errorKey), findsOneWidget);
      expect(find.text('Image unavailable'), findsOneWidget);
    });
  });

  testWidgets('grid card and detail preview render the SVG thumbnail',
      (tester) async {
    final fetcher = RecordingSvgFetcher();
    final router = GoRouter(
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
    await tester.pumpWidget(ProviderScope(
      overrides: _libraryOverrides(fetcher),
      child: MaterialApp.router(routerConfig: router),
    ));
    final gridSvg = find.descendant(
      of: find.byKey(const Key('media_card_9')),
      matching: find.byType(VectorGraphic),
    );
    await pumpUntilFound(tester, gridSvg);
    expect(gridSvg, findsOneWidget);

    await tester.tap(find.byKey(const Key('media_card_9')));
    final detailSvg = find.descendant(
      of: find.byKey(const Key('media_detail_thumbnail')),
      matching: find.byType(VectorGraphic),
    );
    await pumpUntilFound(tester, detailSvg);

    expect(detailSvg, findsOneWidget);
    expect(find.byType(CachedNetworkImage), findsNothing);
    for (final (uri, headers) in fetcher.requests) {
      expect(uri.toString(), '$_base/api/v1/media/9/thumbnail');
      expect(headers['Authorization'], 'Bearer pt-svg-test');
    }
  });

  testWidgets('grid card shows the broken-image icon for a malformed SVG',
      (tester) async {
    await tester.pumpWidget(ProviderScope(
      overrides: _libraryOverrides(RecordingSvgFetcher(body: kMalformedSvg)),
      child: const MaterialApp(home: MediaGridScreen(setId: 1)),
    ));
    final broken = find.descendant(
      of: find.byKey(const Key('media_card_9')),
      matching: find.byIcon(Icons.broken_image_outlined),
    );
    await pumpUntilFound(tester, broken);

    expect(broken, findsOneWidget);
  });

  testWidgets('folder browser tile renders the SVG thumbnail', (tester) async {
    await tester.pumpWidget(ProviderScope(
      overrides: _libraryOverrides(RecordingSvgFetcher()),
      child: const MaterialApp(home: FolderBrowserScreen(setId: 1)),
    ));
    final tileSvg = find.descendant(
      of: find.byKey(const Key('media_thumb_9')),
      matching: find.byType(VectorGraphic),
    );
    await pumpUntilFound(tester, tileSvg);

    expect(tileSvg, findsOneWidget);
  });

  group('share viewer thumbnail', () {
    Future<void> pumpShare(WidgetTester tester, RecordingSvgFetcher fetcher) =>
        tester.pumpWidget(ProviderScope(
          overrides: [
            publicApiClientProvider.overrideWithValue(_PublicClient()),
            svgFetcherProvider.overrideWithValue(fetcher.fetcher),
          ],
          child: const MaterialApp(home: ShareViewerScreen(token: 'tok')),
        ));

    testWidgets('SVG share renders its thumbnail without credentials',
        (tester) async {
      final fetcher = RecordingSvgFetcher();
      await pumpShare(tester, fetcher);
      await pumpUntilFound(tester, find.byType(VectorGraphic));

      expect(find.byType(VectorGraphic), findsOneWidget);
      expect(find.byKey(const Key('share_viewer_thumbnail')), findsOneWidget);
      final (uri, headers) = fetcher.requests.single;
      expect(uri.toString(), '$_base/s/tok/thumbnail');
      expect(headers, isEmpty);
    });

    testWidgets('malformed SVG thumbnail falls back to the type icon',
        (tester) async {
      await pumpShare(tester, RecordingSvgFetcher(body: kMalformedSvg));
      final fallback =
          find.byKey(const Key('share_viewer_thumbnail_placeholder'));
      await pumpUntilFound(tester, fallback);

      expect(fallback, findsOneWidget);
      expect(find.byType(VectorGraphic), findsNothing);
    });
  });
}
