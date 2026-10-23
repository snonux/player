import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/widgets/music_artwork_section.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const channel = MethodChannel('zone.foo.player_android/artwork_picker');
  testWidgets(
      'cancel image picker preserves cover and never writes; viewer cannot edit',
      (tester) async {
    final writes = <String>[];
    bool editable = true;
    final dio = Dio();
    dio.interceptors.add(InterceptorsWrapper(onRequest: (options, handler) {
      if (options.method != 'GET') writes.add(options.method);
      handler.resolve(Response(
          requestOptions: options,
          statusCode: 200,
          data: {'has_artwork': false, 'can_edit': editable, 'revision': ''}));
    }));
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async => null);
    addTearDown(() => TestDefaultBinaryMessengerBinding
        .instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    final api = PlayerApiClient(dio: dio);
    Widget screen(String key) => ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(api)],
        child: MaterialApp(
            home: Scaffold(
                body: MusicArtworkSection(
                    kind: 'artist',
                    entityKey: key,
                    fallback: const Text('Original cover')))));
    await tester.pumpWidget(screen('one'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Change artwork'));
    await tester.pumpAndSettle();
    expect(writes, isEmpty);
    expect(find.text('Original cover'), findsOneWidget);
    editable = false;
    await tester.pumpWidget(screen('two'));
    await tester.pumpAndSettle();
    expect(find.text('Change artwork'), findsNothing);
    expect(find.text('Original cover'), findsOneWidget);
  });
}
