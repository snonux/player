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
import 'package:player_android/screens/image_viewer_screen.dart';
import 'package:player_android/screens/audio_player_screen.dart';
import 'package:player_android/screens/media_detail_screen.dart';
import 'package:player_android/screens/media_grid_screen.dart';
import 'package:player_android/widgets/authenticated_network_image.dart';

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
  Future<Media> getMedia(int mediaId) async => media;

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
  testWidgets('grid to detail to full image uses authenticated stream',
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
          builder: (_, state) =>
              MediaDetailScreen(mediaId: state.pathParameters['id']!),
        ),
        GoRoute(
          path: AppRoutes.imageViewer,
          builder: (_, state) => ImageViewerScreen(
            mediaId: state.pathParameters['mediaId']!,
            imageUrl: state.extra as String?,
          ),
        ),
        GoRoute(
          path: AppRoutes.audioPlayer,
          builder: (_, __) => const Text('Unexpected audio route'),
        ),
      ],
    );

    await tester.pumpWidget(ProviderScope(
      overrides: [
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
    await tester.pumpAndSettle();
    expect(find.text('View Image'), findsOneWidget);

    await tester.ensureVisible(find.byKey(const Key('media_detail_play')));
    await tester.tap(find.byKey(const Key('media_detail_play')));
    await tester.pump();
    await tester.pump();
    await tester.pump();

    expect(find.byType(ImageViewerScreen), findsOneWidget);
    expect(find.byType(AudioPlayerScreen), findsNothing);
    expect(find.byKey(const Key('image_viewer_zoom')), findsOneWidget);
    expect(find.text('Unexpected audio route'), findsNothing);
    final image = tester.widget<CachedNetworkImage>(
      find.byType(CachedNetworkImage),
    );
    expect(image.imageUrl, client.streamUrl(9));
    expect(image.httpHeaders?['Authorization'], 'Bearer pt-image-test');

    await tester.tap(find.descendant(
      of: find.byType(ImageViewerScreen),
      matching: find.byTooltip('Back'),
    ));
    await tester.pumpAndSettle();
    expect(find.byType(MediaDetailScreen), findsOneWidget);
    expect(find.byType(ImageViewerScreen), findsNothing);
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
}
