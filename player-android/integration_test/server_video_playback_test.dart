import 'dart:convert';
import 'dart:io';

import 'package:chewie/chewie.dart';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:player_android/api/dio_player_api_client.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/video_player_screen.dart';
import 'package:player_android/services/playback_request.dart';

// Credentials come from --dart-define-from-file outside the repository.
// Omit progress callbacks to preserve the account's resume positions.
const _origin = String.fromEnvironment('VIDEO_E2E_ORIGIN',
    defaultValue: 'https://player.f3s.buetow.org');
const _ids = String.fromEnvironment('VIDEO_E2E_IDS',
    defaultValue: '3433,3444,3488,3489');

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets('server video streams initialize, advance and resume',
      (tester) async {
    final origin = Uri.parse(_origin);
    final http = HttpClient();
    final login = await http.postUrl(origin.resolve('/api/v1/auth/login'));
    login.headers.contentType = ContentType.json;
    login.write(jsonEncode({
      'username': const String.fromEnvironment('username'),
      'password': const String.fromEnvironment('password'),
    }));
    final response = await login.close();
    expect(response.statusCode, 200);
    final cookies = response.cookies;
    await response.drain<void>();
    http.close();
    final jar = CookieJar();
    await jar.saveFromResponse(origin, cookies);
    final dio = Dio(BaseOptions(baseUrl: _origin, headers: {
      'Cookie': cookies.map((c) => '${c.name}=${c.value}').join('; '),
    }));
    final api = DioPlayerApiClient(dio: dio);
    addTearDown(() async {
      await api.logout();
      dio.close();
    });
    final mutations = CredentialMutationQueue(credentialsEnabled: true);
    for (final id in _ids.split(',').map(int.parse)) {
      final media = await api.getMedia(id);
      debugPrint('Video E2E: opening $id (${media.codec})');
      await tester.pumpWidget(ProviderScope(
        overrides: [
          playerBaseUrlProvider.overrideWithValue(origin),
          apiClientProvider.overrideWithValue(api),
          cookieJarProvider.overrideWithValue(jar),
          credentialMutationQueueProvider.overrideWithValue(mutations),
        ],
        child: MaterialApp(
            home: VideoPlayerScreen(
          key: ValueKey(id),
          mediaId: '$id',
          request: ServerPlaybackRequest(
            mediaId: id,
            serverOrigin: _origin,
            userId: 0,
            sourceUri: Uri.parse(api.playbackUrl(media)),
            title: media.fileName,
            startPosition: 0,
          ),
        )),
      ));
      final deadline = DateTime.now().add(const Duration(seconds: 60));
      while (find.byType(Chewie).evaluate().isEmpty &&
          DateTime.now().isBefore(deadline)) {
        await tester.pump(const Duration(milliseconds: 200));
        final error = find.byKey(const Key('video_player_error_message'));
        if (error.evaluate().isNotEmpty) {
          fail('Video $id: ${tester.widget<Text>(error).data}');
        }
      }
      expect(find.byType(Chewie), findsOneWidget,
          reason: 'Video $id must initialize');
      final controller = tester
          .widget<Chewie>(find.byType(Chewie))
          .controller
          .videoPlayerController;
      while (controller.value.position.inSeconds < 3 &&
          DateTime.now().isBefore(deadline)) {
        await tester.pump(const Duration(milliseconds: 200));
      }
      expect(controller.value.errorDescription, isNull);
      expect(controller.value.size.width, greaterThan(0));
      expect(controller.value.position.inSeconds, greaterThanOrEqualTo(3));
      await controller.pause();
      await tester.pump(const Duration(milliseconds: 300));
      final paused = controller.value.position;
      await controller.play();
      final resumed = DateTime.now().add(const Duration(seconds: 10));
      while (controller.value.position <= paused &&
          DateTime.now().isBefore(resumed)) {
        await tester.pump(const Duration(milliseconds: 200));
      }
      expect(controller.value.position, greaterThan(paused));
      debugPrint(
          'Video E2E: PASS $id size=${controller.value.size} position=${controller.value.position}');
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 500));
    }
  }, timeout: const Timeout(Duration(minutes: 6)));
}
