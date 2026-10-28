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
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/screens/image_viewer_screen.dart';
import 'package:player_android/screens/audio_player_screen.dart';
import 'package:player_android/screens/media_detail_screen.dart';
import 'package:player_android/screens/media_grid_screen.dart';
import 'package:player_android/widgets/authenticated_network_image.dart';
import '../support/idle_playback.dart';
import '../support/server_endpoint.dart';

const _image = Media(
  id: 9,
  setId: 1,
  relPath: 'cygnus-loop-pia17172.jpg',
  fileName: 'cygnus-loop-pia17172.jpg',
  absPath: '/media/cygnus-loop-pia17172.jpg',
  type: 'image',
  duration: 0,
  codec: 'jpeg',
  resolution: '1024x768',
  bitrate: 0,
  fileSizeBytes: 1000,
  width: 1024,
  height: 768,
  thumbnailPath: '',
  playCount: 0,
);

class _TokenStorage implements TokenStorage {
  @override
  Future<String?> readToken() async => 'pt-image-test';
  @override
  Future<void> writeToken(String token) async {}
  @override
  Future<void> deleteToken() async {}
}

class _Client extends PlayerApiClient {
  _Client({this.media = _image}) : super(dio: Dio());

  Media media;
  int streamUrlCalls = 0;
  final mediaIds = <int>[];

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
      [_image];

  @override
  Future<Media> getMedia(int mediaId, {CancelToken? cancelToken}) async {
    mediaIds.add(mediaId);
    return media;
  }

  @override
  Future<bool> toggleFavorite(int mediaId) async {
    media = Media.fromJson(media.toJson()..['favorite'] = !media.favorite);
    return media.favorite;
  }

  @override
  Future<List<Tag>> listTags() async => [];

  @override
  String thumbnailUrl(int mediaId) => '';

  @override
  String streamUrl(int mediaId) {
    streamUrlCalls++;
    return 'https://player.example/api/v1/media/$mediaId/stream';
  }
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));
  testWidgets(
      'grid opens the full image directly; details link back to the viewer',
      (tester) async {
    final client = _Client();
    final router = GoRouter(
      initialLocation: AppRoutes.mediaGridPath(1),
      routes: [
        GoRoute(
          path: AppRoutes.mediaGrid,
          builder: (_, __) => const MediaGridScreen(setId: 1),
        ),
        GoRoute(
          path: AppRoutes.mediaDetail,
          builder: (_, state) => MediaDetailScreen(
              mediaId: state.pathParameters['id']!,
              fromPlayback: (state.extra as Map?)?['fromPlayback'] == true),
        ),
        GoRoute(
          path: AppRoutes.imageViewer,
          builder: (_, state) {
            final extra = state.extra as Map<String, dynamic>;
            return ImageViewerScreen(
              mediaId: state.pathParameters['mediaId']!,
              imageUrl: extra['mediaUrl'] as String?,
              mediaTitle: extra['title'] as String?,
            );
          },
        ),
        GoRoute(
          path: AppRoutes.audioPlayer,
          builder: (_, __) => const Text('Unexpected audio route'),
        ),
      ],
    );

    await tester.pumpWidget(ProviderScope(
      overrides: [
        ...idlePlaybackOverrides(),
        unpairedServerEndpointOverride(),
        apiClientProvider.overrideWithValue(client),
        playerBaseUrlProvider
            .overrideWithValue(Uri.parse('https://player.example')),
        tokenStorageProvider.overrideWithValue(_TokenStorage()),
        cookieJarProvider.overrideWithValue(CookieJar()),
        credentialMutationQueueProvider.overrideWithValue(
          CredentialMutationQueue(credentialsEnabled: true),
        ),
      ],
      child: MaterialApp.router(routerConfig: router),
    ));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('media_card_9')));
    await tester.pump();
    await tester.pump();
    await tester.pump();

    expect(find.byType(MediaDetailScreen), findsNothing);
    expect(find.byType(ImageViewerScreen), findsOneWidget);
    expect(find.byType(AudioPlayerScreen), findsNothing);
    expect(find.byKey(const Key('image_viewer_zoom')), findsOneWidget);
    expect(
      find.descendant(
        of: find.descendant(
          of: find.byType(ImageViewerScreen),
          matching: find.byType(AppBar),
        ),
        matching: find.text('cygnus-loop-pia17172.jpg'),
      ),
      findsOneWidget,
    );
    expect(find.text('Unexpected audio route'), findsNothing);
    for (var frame = 0;
        frame < 20 && find.byType(CachedNetworkImage).evaluate().isEmpty;
        frame++) {
      await tester.pump(const Duration(milliseconds: 10));
    }
    final image = tester.widget<CachedNetworkImage>(
      find.byType(CachedNetworkImage),
    );
    expect(image.imageUrl, client.streamUrl(9));
    expect(image.httpHeaders?['Authorization'], 'Bearer pt-image-test');
    for (var i = 0; i < 8; i++) {
      await tester.pump();
    }
    await tester.pump(const Duration(milliseconds: 500));
    await tester.ensureVisible(find.byTooltip('Add to favorites'));
    await tester.tap(find.byTooltip('Add to favorites'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    expect(client.media.favorite, isTrue);

    await tester.tap(find.byTooltip('More options'));
    await _pumpFrames(tester);
    await tester.tap(find.byKey(const Key('playback_details_button')));
    await _pumpFrames(tester);
    expect(find.byType(MediaDetailScreen), findsOneWidget);
    expect(find.text('View Image'), findsOneWidget);
    expect(find.byTooltip('Remove from favourites'), findsOneWidget);

    // Play from details returns to the open viewer instead of stacking one.
    await tester.ensureVisible(find.byKey(const Key('media_detail_play')));
    await tester.tap(find.byKey(const Key('media_detail_play')));
    await _pumpFrames(tester);
    expect(find.byType(MediaDetailScreen), findsNothing);
    expect(find.byType(ImageViewerScreen), findsOneWidget);

    await tester.tap(find.descendant(
      of: find.byType(ImageViewerScreen),
      matching: find.byTooltip('Back'),
    ));
    await _pumpFrames(tester);
    expect(find.byType(ImageViewerScreen), findsNothing);
    expect(find.byKey(const Key('media_card_9')), findsOneWidget);
  });

  testWidgets('swipes navigate images through errors and stop at sequence ends',
      (tester) async {
    final second = Media.fromJson(_image.toJson()
      ..['id'] = 10
      ..['file_name'] = 'second.jpg');
    final audio = Media.fromJson(_image.toJson()
      ..['id'] = 11
      ..['type'] = 'audio');
    final client = _Client(media: audio);
    await tester.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client)],
      child: MaterialApp(
          home: ImageViewerScreen(
              mediaId: '9', sequence: [_image, audio, second])),
    ));
    await tester.pumpAndSettle();
    expect(find.text('Image unavailable'), findsOneWidget);
    final area = find.byKey(const Key('image_viewer_error'));
    await tester.drag(area, const Offset(-150, 0));
    await tester.pumpAndSettle();
    expect(client.mediaIds.last, 10);
    expect(find.text('2 / 2'), findsOneWidget);
    final count = client.mediaIds.length;
    await tester.drag(area, const Offset(-150, 0));
    await tester.pumpAndSettle();
    expect(client.mediaIds.length, count);
    await tester.drag(area, const Offset(0, 150));
    await tester.pumpAndSettle();
    expect(client.mediaIds.length, count);
    await tester.tap(find.byTooltip('Previous image'));
    await tester.pumpAndSettle();
    expect(client.mediaIds.last, 9);
    await tester.tap(find.byTooltip('More options'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Zoom in'));
    await tester.pumpAndSettle();
    final beforePan = client.mediaIds.length;
    await tester.drag(area, const Offset(-150, 0));
    await tester.pumpAndSettle();
    expect(client.mediaIds.length, beforePan,
        reason: 'Zoomed panning must not change images');
    await tester.tap(find.byTooltip('More options'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Reset zoom'));
    await tester.pumpAndSettle();
    await tester.drag(area, const Offset(-150, 0));
    await tester.pumpAndSettle();
    await tester.drag(area, const Offset(150, 0));
    await tester.pumpAndSettle();
    expect(client.mediaIds.last, 9);
  });

  testWidgets(
      'two fingers pinch without navigation and zoom has menu equivalents',
      (tester) async {
    await tester.pumpWidget(const ProviderScope(
        child: MaterialApp(
            home: ImageViewerScreen(
                mediaId: 'shared',
                isPublicShare: true,
                imageUrl: 'https://example.invalid/image.jpg'))));
    await tester.pump();
    final zoom = find.byKey(const Key('image_viewer_zoom'));
    final center = tester.getCenter(zoom);
    final a =
        await tester.startGesture(center - const Offset(8, 0), pointer: 1);
    final b =
        await tester.startGesture(center + const Offset(8, 0), pointer: 2);
    await a.moveTo(center - const Offset(40, 0));
    await b.moveTo(center + const Offset(40, 0));
    await tester.pump();
    await a.up();
    await b.up();
    final controller =
        tester.widget<InteractiveViewer>(zoom).transformationController!;
    expect(controller.value.getMaxScaleOnAxis(), greaterThan(1));
    await tester.tap(find.byTooltip('More options'));
    await _pumpFrames(tester);
    expect(find.text('Details'), findsNothing);
    await tester.tap(find.text('Reset zoom'));
    await _pumpFrames(tester);
    expect(controller.value.getMaxScaleOnAxis(), 1);
    expect(find.byTooltip('Next image'), findsNothing);
  });

  testWidgets('direct image route rejects an audio item before requesting it',
      (tester) async {
    final client = _Client(
      media: Media.fromJson(_image.toJson()..['type'] = 'audio'),
    );
    await tester.pumpWidget(ProviderScope(
      overrides: [apiClientProvider.overrideWithValue(client)],
      child: const MaterialApp(
        home: ImageViewerScreen(mediaId: '9'),
      ),
    ));
    await tester.pump();

    expect(find.text('Image unavailable'), findsOneWidget);
    expect(find.byType(AuthenticatedNetworkImage), findsNothing);
    expect(find.byType(AudioPlayerScreen), findsNothing);
    expect(client.streamUrlCalls, 0);
  });
  testWidgets(
      'reused image route changes personal owner and clears account actions for public shares',
      (tester) async {
    final client =
        _Client(media: Media.fromJson(_image.toJson()..['type'] = 'audio'));
    Future<void> show(String id, {bool public = false}) =>
        tester.pumpWidget(ProviderScope(
            overrides: [apiClientProvider.overrideWithValue(client)],
            child: MaterialApp(
                home: ImageViewerScreen(mediaId: id, isPublicShare: public))));
    await show('9');
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('More options'));
    await tester.pumpAndSettle();
    expect(find.text('Notes'), findsOneWidget);
    expect(find.byKey(const Key('playback_details_button')), findsOneWidget);
    Navigator.of(tester.element(find.text('Notes'))).pop();
    await tester.pumpAndSettle();
    await show('10');
    await tester.pumpAndSettle();
    expect(client.mediaIds.where((id) => id == 10), isNotEmpty);
    await show('shared', public: true);
    await tester.pumpAndSettle();
    final calls = client.mediaIds.length;
    expect(find.byTooltip('More options'), findsOneWidget,
        reason: 'public shares retain accessible zoom actions');
    expect(find.byTooltip('Add to favorites'), findsNothing);
    await tester.pump();
    expect(client.mediaIds.length, calls);
  });
}

/// Advances route transitions without waiting for image loaders to idle.
Future<void> _pumpFrames(WidgetTester tester) async {
  for (var i = 0; i < 10; i++) {
    await tester.pump(const Duration(milliseconds: 100));
  }
}
