import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/app_routes.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/media_playback_navigation.dart';

Media _media(int id, String type, {bool transcoded = false}) => Media(
      id: id,
      setId: 1,
      relPath: 'dir/item$id',
      fileName: 'item$id',
      absPath: '/media/dir/item$id',
      type: type,
      duration: 10,
      codec: '',
      resolution: '',
      bitrate: 0,
      fileSizeBytes: 1,
      width: 0,
      height: 0,
      thumbnailPath: '',
      playCount: 0,
      transcoded: transcoded,
    );

class _Client extends PlayerApiClient {
  _Client({this.byId = const {}})
      : super(dio: Dio(BaseOptions(baseUrl: 'https://player.example')));
  final Map<int, Media> byId;

  @override
  Future<Media> getMedia(int mediaId, {CancelToken? cancelToken}) async =>
      byId[mediaId] ?? (throw StateError('missing $mediaId'));
}

/// Where the last navigation landed, and with which extras.
String? _location;
Map<dynamic, dynamic>? _extra;

Widget _app(_Client client, void Function(BuildContext, WidgetRef) onTap) {
  Widget recorder(GoRouterState state) {
    _location = state.uri.path;
    _extra = state.extra as Map?;
    return Scaffold(body: Text('at ${state.uri.path}'));
  }

  final router = GoRouter(routes: [
    GoRoute(
      path: '/',
      builder: (_, __) => Consumer(
        builder: (context, ref, _) => Scaffold(
          body: TextButton(
              onPressed: () => onTap(context, ref), child: const Text('open')),
        ),
      ),
    ),
    for (final path in [
      AppRoutes.audioPlayer,
      AppRoutes.videoPlayer,
      AppRoutes.imageViewer,
      AppRoutes.mediaDetail,
    ])
      GoRoute(path: path, builder: (_, state) => recorder(state)),
  ]);
  return ProviderScope(
    overrides: [apiClientProvider.overrideWithValue(client)],
    child: MaterialApp.router(routerConfig: router),
  );
}

void main() {
  setUp(() {
    _location = null;
    _extra = null;
  });

  Future<void> open(WidgetTester tester, _Client client,
      void Function(BuildContext, WidgetRef) onTap) async {
    await tester.pumpWidget(_app(client, onTap));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
  }

  testWidgets('audio opens the player with its sequence and folder flag',
      (tester) async {
    final first = _media(1, 'audio');
    final second = _media(2, 'audio');
    await open(
        tester,
        _Client(),
        (context, ref) => openMediaPlayback(context, ref, second,
            sequence: [first, second], traverseFolders: true));

    expect(_location, '/audio/2');
    expect((_extra!['sequence'] as List<Media>).map((m) => m.id), [1, 2]);
    expect(_extra!['traverseFolders'], isTrue);
    expect(_extra!['title'], second.displayTitle);
    expect(_extra!['sourceName'], 'dir/item2');
  });

  testWidgets('audio without a sequence plays just that item', (tester) async {
    await open(tester, _Client(),
        (context, ref) => openMediaPlayback(context, ref, _media(3, 'audio')));
    expect((_extra!['sequence'] as List<Media>).single.id, 3);
    expect(_extra!['traverseFolders'], isFalse);
  });

  testWidgets('transcoded video opens the compatibility stream',
      (tester) async {
    await open(
        tester,
        _Client(),
        (context, ref) => openMediaPlayback(
            context, ref, _media(4, 'video', transcoded: true)));
    expect(_location, '/video/4');
    expect(_extra!['mediaUrl'], 'https://player.example/api/v1/media/4/compat');
  });

  testWidgets('image opens the original stream', (tester) async {
    await open(tester, _Client(),
        (context, ref) => openMediaPlayback(context, ref, _media(5, 'image')));
    expect(_location, '/image/5');
    expect(_extra!['mediaUrl'], 'https://player.example/api/v1/media/5/stream');
  });

  testWidgets('an unknown media type falls back to the details screen',
      (tester) async {
    await open(tester, _Client(),
        (context, ref) => openMediaPlayback(context, ref, _media(6, 'pdf')));
    expect(_location, '/media/6');
  });

  testWidgets('by ID: metadata decides which player opens', (tester) async {
    await open(tester, _Client(byId: {7: _media(7, 'video')}),
        (context, ref) => openMediaPlaybackById(context, ref, 7));
    expect(_location, '/video/7');
  });

  testWidgets('by ID: a failed lookup stays put and says so', (tester) async {
    await open(tester, _Client(),
        (context, ref) => openMediaPlaybackById(context, ref, 8));
    expect(_location, isNull);
    expect(find.text('Could not open this item.'), findsOneWidget);
  });

  testWidgets('details button opens details marked as from playback',
      (tester) async {
    await open(tester, _Client(),
        (context, ref) => openPlaybackDetails(context, 9));
    expect(_location, '/media/9');
    expect(_extra!['fromPlayback'], isTrue);
  });
}
