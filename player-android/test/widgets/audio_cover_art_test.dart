import 'package:cookie_jar/cookie_jar.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/server_endpoint_provider.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/widgets/podcast_artwork.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/widgets/audio_cover_art.dart';
import 'package:player_android/widgets/authenticated_network_image.dart';

import '../support/fake_http.dart';
import '../support/real_image_loading.dart';
import '../support/svg_test_support.dart';

const _origin = 'https://player.example';

class _Tokens implements TokenStorage {
  @override
  Future<String?> readToken() async => 'artwork-token';
  @override
  Future<void> writeToken(String token) async {}
  @override
  Future<void> deleteToken() async {}
}

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio(BaseOptions(baseUrl: _origin)));
}

class _Auth extends AuthStateNotifier {
  _Auth(this.userId);
  final int userId;
  @override
  Future<AuthState> build() async => _state(userId);
  AuthState _state(int id) => AuthState.authenticated(
      user: User(id: id, username: 'user$id', isAdmin: false));
  void changeUser(int id) => state = AsyncData(_state(id));
}

PlaybackRequest _server(int id) => ServerPlaybackRequest(
      mediaId: id,
      serverOrigin: _origin,
      userId: 1,
      sourceUri: Uri.parse('$_origin/api/v1/media/$id/stream'),
      title: 'song.mp3',
    );

Widget _art(PlaybackRequest request,
        {bool allowAccount = true, int userId = 1}) =>
    ProviderScope(
      overrides: [
        authStateProvider.overrideWith(() => _Auth(userId)),
        playerBaseUrlProvider.overrideWithValue(Uri.parse(_origin)),
        apiClientProvider.overrideWith((_) {
          if (!allowAccount) throw StateError('Account API accessed');
          return _Client();
        }),
        tokenStorageProvider.overrideWith((_) {
          if (!allowAccount) throw StateError('Account token accessed');
          return _Tokens();
        }),
        cookieJarProvider.overrideWithValue(CookieJar()),
        credentialMutationQueueProvider.overrideWithValue(
            CredentialMutationQueue(credentialsEnabled: true)),
        // Artwork routing is covered with endpoint-router tests. Keep this
        // widget test about image credentials and rendering, rather than
        // starting the router's platform connectivity subscription.
        routedServerUriProvider.overrideWith((_, uri) async => uri),
      ],
      child: MaterialApp(home: Scaffold(body: AudioCoverArt(request: request))),
    );

Finder get _decoded => find
    .byWidgetPredicate((widget) => widget is RawImage && widget.image != null);

void main() {
  useRealImageLoading();

  testWidgets('renders authenticated artwork, missing fallback and public art',
      (tester) async {
    final png = (await tester.runAsync(() => makePng(4, 4)))!;
    final server = startRealImageLoading(
        tester,
        (uri, _) => uri.path.contains('/2/')
            ? const FakeHttpReply([], status: 404)
            : FakeHttpReply(png));
    await tester.pumpWidget(_art(_server(1)));
    await pumpUntilFound(tester, _decoded);
    expect(server.requests.single.$1.path, '/api/v1/media/1/thumbnail');
    expect(server.requests.single.$2['authorization'], 'Bearer artwork-token');

    // Keep real CachedNetworkImage scenarios in this one fake-async zone;
    // its cache singleton cannot cross widget-test zones.
    await tester.pumpWidget(const SizedBox.shrink());
    final podcast = ServerPlaybackRequest(
        mediaId: 5043,
        serverOrigin: _origin,
        userId: 1,
        sourceUri: Uri.parse('$_origin/api/v1/media/5043/stream'),
        title: 'Episode',
        podcast: const MediaPodcastMetadata(title: 'Episode'));
    await tester.pumpWidget(_art(podcast));
    await pumpUntilFound(tester, _decoded);
    expect(server.requests.last.$1.path, '/api/v1/media/5043/thumbnail');
    expect(server.requests.last.$2['authorization'], 'Bearer artwork-token');

    // A retained route must drop its image instead of using the next user's
    // credentials, even when the server origin has not changed.
    final container =
        ProviderScope.containerOf(tester.element(find.byType(AudioCoverArt)));
    (container.read(authStateProvider.notifier) as _Auth).changeUser(2);
    await tester.pump();
    expect(find.byType(AuthenticatedNetworkImage), findsNothing);
    expect(_decoded, findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpWidget(_art(_server(2)));
    await pumpUntilFound(tester, find.byType(AuthenticatedNetworkImage));
    await pumpUntilFound(
        tester, find.byKey(const Key('audio_artwork_unavailable')));
    expect(server.requests.last.$1.path, '/api/v1/media/2/thumbnail');
    expect(_decoded, findsNothing);
    expect(find.byIcon(Icons.headphones), findsOneWidget);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpWidget(_art(
      PublicSharePlaybackRequest(
        shareToken: 'one-use',
        sourceUri:
            Uri.parse('$_origin/prefix/s/one-use/compat?view=credential'),
        title: 'shared.mp3',
      ),
      allowAccount: false,
    ));
    await pumpUntilFound(tester, _decoded);
    final (url, headers) = server.requests.last;
    expect(
        url.toString(), '$_origin/prefix/s/one-use/thumbnail?view=credential');
    expect(headers.containsKey('authorization'), isFalse);
    expect(headers.containsKey('cookie'), isFalse);
    await tester.pumpWidget(const SizedBox.shrink());
    await settleImageCache(tester);
  });

  testWidgets(
      'podcast artwork prefers publisher metadata and device copies avoid the account API',
      (tester) async {
    const metadata = MediaPodcastMetadata(
        title: 'Canonical episode',
        imageUrl: 'https://publisher.example/episode.jpg',
        feedImageUrl: 'https://publisher.example/feed.jpg');
    for (final request in <PlaybackRequest>[
      ServerPlaybackRequest(
          mediaId: 5043,
          serverOrigin: _origin,
          userId: 1,
          sourceUri: Uri.parse('$_origin/api/v1/media/5043/stream'),
          title: 'Canonical episode',
          podcast: metadata),
      PodcastPlaybackRequest(
          identity: 'podcast:local:feed:episode',
          sourceUri: Uri.file('/private/episode.mp3'),
          title: 'Canonical episode',
          podcast: metadata),
    ]) {
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pumpWidget(
          _art(request, allowAccount: request is ServerPlaybackRequest));
      await tester.pump();
      await tester.pump();
      final art = tester.widget<PodcastArtwork>(find.byType(PodcastArtwork));
      expect(art.imageUrl, metadata.imageUrl);
      expect(art.fallbackUrl, metadata.feedImageUrl);
      expect(tester.takeException(), isNull);
    }
  });

  testWidgets('device files and offline copies never access the account',
      (tester) async {
    for (final request in <PlaybackRequest>[
      LocalPlaybackRequest(
          localMediaId: 1,
          sourceUri: Uri.parse('content://song'),
          title: 'song'),
      OfflinePlaybackRequest(
          downloadKey: 'download',
          sourceUri: Uri.file('/private/song.mp3'),
          title: 'song'),
      ServerPlaybackRequest(
          mediaId: 1,
          serverOrigin: 'https://other.example',
          userId: 1,
          sourceUri: Uri.parse('https://other.example/api/v1/media/1/stream'),
          title: 'song'),
    ]) {
      await tester.pumpWidget(_art(request, allowAccount: false));
      await tester.pump();
      expect(find.byIcon(Icons.headphones), findsOneWidget);
      expect(tester.takeException(), isNull);
    }
  });

  testWidgets('same server with a different account never requests artwork',
      (tester) async {
    await tester.pumpWidget(_art(_server(1), allowAccount: false, userId: 2));
    await tester.pump();
    expect(find.byType(AuthenticatedNetworkImage), findsNothing);
    expect(find.byIcon(Icons.headphones), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
