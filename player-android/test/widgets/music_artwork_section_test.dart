import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/widgets/music_artwork_section.dart';
import 'package:player_android/widgets/overflow_menu.dart';

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

  testWidgets(
      'hidden edit buttons stay reachable via editMenuItems; cancel never writes',
      (tester) async {
    final writes = <String>[];
    final key = GlobalKey<MusicArtworkSectionState>();
    final dio = Dio();
    dio.interceptors.add(InterceptorsWrapper(onRequest: (options, handler) {
      if (options.method != 'GET') writes.add(options.method);
      handler.resolve(Response(
          requestOptions: options,
          statusCode: 200,
          data: {'has_artwork': false, 'can_edit': true, 'revision': ''}));
    }));
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async => null);
    addTearDown(() => TestDefaultBinaryMessengerBinding
        .instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    await tester.pumpWidget(ProviderScope(
        overrides: [
          apiClientProvider.overrideWithValue(PlayerApiClient(dio: dio))
        ],
        child: MaterialApp(
            home: Scaffold(
                body: Column(children: [
              MusicArtworkSection(
                  key: key,
                  kind: 'artist',
                  entityKey: 'ar_a',
                  showEditButtons: false,
                  fallback: const Text('Cover')),
              OverflowMenuButton(
                  items: (_) => key.currentState?.editMenuItems() ?? const []),
            ])))));
    await tester.pumpAndSettle();
    expect(find.text('Change artwork'), findsNothing);
    expect(find.text('Remove artwork'), findsNothing);
    expect(key.currentState!.editMenuItems(), hasLength(1));
    await tester.tap(find.byKey(const Key('overflow_menu_button')));
    await tester.pumpAndSettle();
    expect(find.text('Change artwork'), findsOneWidget);
    await tester.tap(find.text('Change artwork'));
    await tester.pumpAndSettle();
    expect(writes, isEmpty);
    expect(find.text('Cover'), findsOneWidget);
  });

  testWidgets('editMenuItems empty when viewer cannot edit', (tester) async {
    final key = GlobalKey<MusicArtworkSectionState>();
    final dio = Dio();
    dio.interceptors.add(InterceptorsWrapper(onRequest: (options, handler) {
      handler.resolve(Response(
          requestOptions: options,
          statusCode: 200,
          data: {'has_artwork': true, 'can_edit': false, 'revision': '1'}));
    }));
    await tester.pumpWidget(ProviderScope(
        overrides: [
          apiClientProvider.overrideWithValue(PlayerApiClient(dio: dio))
        ],
        child: MaterialApp(
            home: Scaffold(
                body: MusicArtworkSection(
                    key: key,
                    kind: 'artist',
                    entityKey: 'ar_a',
                    showEditButtons: false)))));
    await tester.pumpAndSettle();
    expect(key.currentState!.editMenuItems(), isEmpty);
    expect(find.byType(OverflowMenuItem), findsNothing);
  });
}
