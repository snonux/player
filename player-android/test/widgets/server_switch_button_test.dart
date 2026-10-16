// Widget tests for ServerSwitchButton, the quick server switch in app bars.
//
// Run with: flutter test test/widgets/server_switch_button_test.dart

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/app_routes.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/first_run_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/widgets/server_switch_button.dart';

const _serverA = 'https://a.example';
const _serverB = 'https://b.example';

class _FakeSettingsNotifier extends SettingsNotifier {
  _FakeSettingsNotifier(this._initial);
  final AppSettings _initial;

  @override
  Future<AppSettings> build() async => _initial;
}

class _FakeAuthNotifier extends AuthStateNotifier {
  _FakeAuthNotifier({this.unlocks = false, this.failSwitch = false});
  final bool unlocks;
  final bool failSwitch;
  final switched = <String>[];
  var unlockCalls = 0;

  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 1, username: 'alice', isAdmin: false));

  @override
  Future<void> switchServer(String url) async {
    if (failSwitch) throw StateError('switch failed');
    switched.add(url);
    state = const AsyncData(AuthState.unauthenticated());
  }

  @override
  Future<bool> unlockSavedServer() async {
    unlockCalls++;
    if (unlocks) {
      state = const AsyncData(AuthState.authenticated(
          user: User(id: 2, username: 'bob', isAdmin: false)));
    }
    return unlocks;
  }
}

AppSettings _settings(List<String> servers,
        {LibraryDestination destination = LibraryDestination.server}) =>
    AppSettings(
      serverBaseUrl: servers.first,
      savedServers: servers,
      destination: destination,
    );

Future<GoRouter> _pump(
  WidgetTester tester, {
  required AppSettings settings,
  required _FakeAuthNotifier auth,
}) async {
  final router = GoRouter(
    initialLocation: AppRoutes.home,
    routes: [
      GoRoute(
        path: AppRoutes.home,
        builder: (_, __) => Scaffold(
          appBar: AppBar(
            title: const Text('Library'),
            actions: const [ServerSwitchButton()],
          ),
        ),
      ),
      GoRoute(
        path: AppRoutes.login,
        builder: (_, __) => const Scaffold(body: Text('login page')),
      ),
    ],
  );
  await tester.pumpWidget(ProviderScope(
    overrides: [
      settingsProvider.overrideWith(() => _FakeSettingsNotifier(settings)),
      authStateProvider.overrideWith(() => auth),
      firstRunProvider.overrideWith((ref) async => false),
    ],
    child: MaterialApp.router(routerConfig: router),
  ));
  await tester.pumpAndSettle();
  return router;
}

void main() {
  testWidgets('is hidden when only one server is saved', (tester) async {
    await _pump(tester,
        settings: _settings([_serverA]), auth: _FakeAuthNotifier());

    expect(find.byKey(const Key('server_switch_button')), findsNothing);
  });

  testWidgets('lists saved servers and marks the current one', (tester) async {
    await _pump(tester,
        settings: _settings([_serverA, _serverB]), auth: _FakeAuthNotifier());

    await tester.tap(find.byKey(const Key('server_switch_button')));
    await tester.pumpAndSettle();

    final current = tester.widget<CheckedPopupMenuItem<String>>(
        find.byKey(const Key('server_switch_option_$_serverA')));
    final other = tester.widget<CheckedPopupMenuItem<String>>(
        find.byKey(const Key('server_switch_option_$_serverB')));
    expect(current.checked, isTrue);
    expect(current.enabled, isFalse);
    expect(other.checked, isFalse);
    expect(other.enabled, isTrue);
  });

  testWidgets('no server is marked current while browsing local files',
      (tester) async {
    await _pump(tester,
        settings: _settings([_serverA, _serverB],
            destination: LibraryDestination.local),
        auth: _FakeAuthNotifier());

    await tester.tap(find.byKey(const Key('server_switch_button')));
    await tester.pumpAndSettle();

    for (final server in [_serverA, _serverB]) {
      final item = tester.widget<CheckedPopupMenuItem<String>>(
          find.byKey(Key('server_switch_option_$server')));
      expect(item.checked, isFalse);
      expect(item.enabled, isTrue);
    }
  });

  testWidgets('switches server, opens sign-in and offers fingerprint unlock',
      (tester) async {
    final auth = _FakeAuthNotifier();
    await _pump(tester, settings: _settings([_serverA, _serverB]), auth: auth);

    await tester.tap(find.byKey(const Key('server_switch_button')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('server_switch_option_$_serverB')));
    await tester.pumpAndSettle();

    expect(auth.switched, [_serverB]);
    expect(auth.unlockCalls, 1);
    expect(find.text('login page'), findsOneWidget);
  });

  testWidgets('a successful unlock signs in to the new server', (tester) async {
    final auth = _FakeAuthNotifier(unlocks: true);
    await _pump(tester, settings: _settings([_serverA, _serverB]), auth: auth);

    await tester.tap(find.byKey(const Key('server_switch_button')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('server_switch_option_$_serverB')));
    await tester.pumpAndSettle();

    final container = ProviderScope.containerOf(
        tester.element(find.text('login page')),
        listen: false);
    final state = container.read(authStateProvider).requireValue;
    expect(state.isAuthenticated, isTrue);
    expect(state.user?.username, 'bob');
  });

  testWidgets('shows an error and stays put when switching fails',
      (tester) async {
    final auth = _FakeAuthNotifier(failSwitch: true);
    await _pump(tester, settings: _settings([_serverA, _serverB]), auth: auth);

    await tester.tap(find.byKey(const Key('server_switch_button')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('server_switch_option_$_serverB')));
    await tester.pumpAndSettle();

    expect(find.text('Could not switch server.'), findsOneWidget);
    expect(find.text('login page'), findsNothing);
    expect(auth.unlockCalls, 0);
  });
}
