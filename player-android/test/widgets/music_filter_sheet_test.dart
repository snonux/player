import 'dart:convert';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/widgets/music_filter_sheet.dart';

class _Auth extends AuthStateNotifier {
  _Auth(this.id);
  final int id;
  @override
  Future<AuthState> build() async => AuthState.authenticated(
      user: User(id: id, username: 'viewer', isAdmin: false));
}

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio());
  @override
  Future<Map<String, dynamic>> musicFacets() async => {
        'genre': ['Ambient', 'Jazz'],
        'year': ['2021'],
        'role': ['composer'],
        'label': ['Label A; B'],
        'compilation': ['false', 'true']
      };
}

void main() {
  testWidgets(
      'restores by account, draft Back discards, Apply and Clear persist',
      (tester) async {
    SharedPreferences.setMockInitialValues({
      'music_facets:http://fixture.test:1': jsonEncode({'genre': 'Ambient'}),
      'music_facets:http://fixture.test:2': jsonEncode({'genre': 'Jazz'})
    });
    final changed = <Map<String, String>>[];
    Future<void> mount(int id) async {
      await tester.pumpWidget(ProviderScope(
          key: ValueKey(id),
          overrides: [
            apiClientProvider.overrideWithValue(_Client()),
            playerBaseUrlProvider
                .overrideWithValue(Uri.parse('http://fixture.test')),
            authStateProvider.overrideWith(() => _Auth(id))
          ],
          child: MaterialApp(
              home: Scaffold(
                  appBar: AppBar(
                      bottom: MusicFilterBar(onChanged: changed.add))))));
      await tester.pumpAndSettle();
    }

    await mount(1);
    expect(find.text('Genre: Ambient'), findsOneWidget);
    await tester.tap(find.text('Filters (1)'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Clear'));
    await tester.pumpAndSettle();
    Navigator.of(tester.element(find.text('Music filters'))).pop();
    await tester.pumpAndSettle();
    expect(find.text('Genre: Ambient'), findsOneWidget);
    await tester.tap(find.text('Filters (1)'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Clear'));
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    expect(changed.last, isEmpty);
    expect(
        (await SharedPreferences.getInstance())
            .getString('music_facets:http://fixture.test:1'),
        '{}');
    await mount(2);
    expect(find.text('Genre: Jazz'), findsOneWidget);
    await mount(1);
    expect(find.text('Filters (0)'), findsOneWidget);
    expect(find.text('Genre: Jazz'), findsNothing);
  });
}
