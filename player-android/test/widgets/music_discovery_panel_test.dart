import 'dart:convert';
import 'package:dio/dio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/models/user.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/widgets/music_discovery_panel.dart';

class _Auth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 1, username: 'viewer', isAdmin: false));
}

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio());
  @override
  Future<List<Map<String, dynamic>>> playlists() async => [];
}

void main() {
  testWidgets(
      'unavailable pins stay anonymous and restore defaults survives remount',
      (tester) async {
    const scope = 'player.music-home.v1:http://fixture.test:1';
    SharedPreferences.setMockInitialValues({
      scope: jsonEncode({
        'order': ['pinned'],
        'hidden': ['recently_added'],
        'pins': ['revoked']
      })
    });
    Future<void> mount(int identity) async {
      await tester.pumpWidget(ProviderScope(
          key: ValueKey(identity),
          overrides: [
            apiClientProvider.overrideWithValue(_Client()),
            playerBaseUrlProvider
                .overrideWithValue(Uri.parse('http://fixture.test')),
            authStateProvider.overrideWith(_Auth.new)
          ],
          child: MaterialApp(
              home: Scaffold(
                  body: MusicDiscoveryPanel(
                      collections: const {},
                      onViewAll: (_, __) {},
                      onOpenAlbum: (_) {},
                      onBrowse: () {},
                      onRefresh: () async {})))));
      await tester.pumpAndSettle();
    }

    await mount(1);
    expect(find.text('Remove 1 unavailable pins'), findsOneWidget);
    expect(find.text('revoked'), findsNothing);
    await tester.tap(find.text('Remove 1 unavailable pins'));
    await tester.pumpAndSettle();
    final prefs = await SharedPreferences.getInstance();
    expect((jsonDecode(prefs.getString(scope)!) as Map)['pins'], isEmpty);
    await tester.tap(find.text('Customize home'));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('Restore defaults'), 300,
        scrollable: find.byType(Scrollable).last);
    await tester.tap(find.text('Restore defaults'));
    await tester.pumpAndSettle();
    expect((jsonDecode(prefs.getString(scope)!) as Map)['hidden'], isEmpty);
    await tester.tap(find.text('Done'));
    await tester.pumpAndSettle();
    await mount(2);
    expect(find.text('Recently added'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
      'narrow discovery keeps empty sources and View all snapshot accessible',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    tester.view.physicalSize = const Size(390, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final snapshot = <Map<String, dynamic>>[
      {
        'album_key': 'b',
        'title': 'A very long album title ${List.filled(80, '長').join()}',
        'artist': 'Artist'
      },
      {'album_key': 'a', 'title': 'Second album', 'artist': 'Another artist'},
    ];
    String? chosen;
    List<Map<String, dynamic>>? chosenItems;
    await tester.pumpWidget(ProviderScope(
        overrides: [
          apiClientProvider.overrideWithValue(_Client()),
          playerBaseUrlProvider
              .overrideWithValue(Uri.parse('http://fixture.test')),
          authStateProvider.overrideWith(_Auth.new)
        ],
        child: MaterialApp(
            theme: ThemeData.dark(),
            home: Scaffold(
                body: MusicDiscoveryPanel(
              collections: {'random': snapshot},
              onViewAll: (key, items) {
                chosen = key;
                chosenItems = items;
              },
              onOpenAlbum: (_) {},
              onBrowse: () {},
              onRefresh: () async {},
            )))));
    await tester.pumpAndSettle();
    expect(find.text('Music home'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('View all random albums'), 300,
        scrollable: find.byType(Scrollable).first);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('View all random albums'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('View all random albums'));
    expect(chosen, 'random');
    expect(identical(chosenItems, snapshot), isTrue,
        reason: 'View all retains displayed ordered snapshot');
    expect(tester.takeException(), isNull);
    await tester.scrollUntilVisible(
        find.text('Rate an album to add it here.'), 300,
        scrollable: find.byType(Scrollable).first);
    expect(find.text('Rate an album to add it here.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
