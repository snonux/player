import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/providers/theme_provider.dart';
import 'package:player_android/screens/home_screen.dart';
import 'package:player_android/screens/settings_screen.dart';

import '../support/idle_playback.dart';
import 'screenshot_support.dart';

class _Storage implements TokenStorage {
  @override
  Future<String?> readToken() async => 't';
  @override
  Future<void> writeToken(String token) async {}
  @override
  Future<void> deleteToken() async {}
}

class ShotApi extends PlayerApiClient {
  ShotApi() : super(dio: Dio());
  @override
  Future<List<MediaSet>> listSets() async => [
        for (final (i, name, podcast) in [
          (1, 'Movies', false),
          (2, 'TV Shows', false),
          (3, 'Music', false),
          (4, 'Podcasts', true),
          (5, 'Family photos', false),
          (6, 'Audiobooks', false),
        ])
          MediaSet(
              id: i,
              name: name,
              rootPath: name,
              coverThumbnailPath: '',
              isPodcast: podcast)
      ];
  @override
  String setFolderCoverUrl(int setId, {String? folder}) => '';
  @override
  Future<List<Map<String, dynamic>>> musicAlbums(
          {String? artist, int? mediaId, List<int>? libraryIds}) async =>
      [];
}

class _ServerSettings extends SettingsNotifier {
  @override
  Future<AppSettings> build() async =>
      const AppSettings(serverBaseUrl: 'http://test.invalid');
}

Widget app(Widget home) => ProviderScope(
        overrides: [
          ...idlePlaybackOverrides(),
          tokenStorageProvider.overrideWithValue(_Storage()),
          credentialMutationQueueProvider.overrideWithValue(
              CredentialMutationQueue(credentialsEnabled: true)),
          playerBaseUrlProvider
              .overrideWithValue(Uri.parse('http://test.invalid')),
          apiClientProvider.overrideWithValue(ShotApi()),
          settingsProvider.overrideWith(_ServerSettings.new),
        ],
        child: MaterialApp(
            debugShowCheckedModeBanner: false,
            theme: buildLightTheme(),
            home: home));

void main() {
  setUpAll(loadScreenshotFonts);
  testWidgets('home', (tester) async {
    usePhone(tester);
    await tester.pumpWidget(app(const SetsListScreen()));
    await tester.pumpAndSettle();
    await expectLater(
        find.byType(MaterialApp), matchesGoldenFile('out/home.png'));
    await tester.tap(find.byTooltip('Open navigation menu'));
    await tester.pumpAndSettle();
    await expectLater(
        find.byType(MaterialApp), matchesGoldenFile('out/home_drawer.png'));
  });
  testWidgets('settings', (tester) async {
    usePhone(tester);
    await tester.pumpWidget(app(const SettingsScreen()));
    await tester.pumpAndSettle();
    await expectLater(
        find.byType(MaterialApp), matchesGoldenFile('out/settings.png'));
  });
}
