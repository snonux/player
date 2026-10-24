import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/server_plugins_api.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/screens/server_plugins_screen.dart';

class _Auth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 1, username: 'admin', isAdmin: true));
  void regularUser() => state = const AsyncData(AuthState.authenticated(
      user: User(id: 2, username: 'listener', isAdmin: false)));
}

class _Transport implements HttpClientAdapter {
  final requests = <RequestOptions>[];
  bool enabled = false;
  bool restarted = false;
  bool empty = false;
  int? listStatus;
  bool loseWriteResponse = false;
  bool longText = false;
  Completer<void>? holdList;

  @override
  Future<ResponseBody> fetch(RequestOptions options, Stream<Uint8List>? body,
      Future<void>? cancelFuture) async {
    requests.add(options);
    if (options.method == 'GET') {
      await holdList?.future;
      return ResponseBody.fromString(
          jsonEncode(listStatus != null
              ? {'error': 'denied'}
              : empty
                  ? []
                  : [
                      {
                        'id': 'probe-plugin',
                        'name': longText
                            ? 'Long plugin name ' * 12
                            : 'Probe plugin',
                        'version': '1.0.0',
                        'enabled': enabled,
                        'status': enabled && !restarted ? 'active' : 'disabled',
                        'capabilities': ['probe'],
                        'description': longText ? 'Long description ' * 30 : '',
                      }
                    ]),
          listStatus ?? 200,
          headers: {
            Headers.contentTypeHeader: ['application/json']
          });
    }
    if (options.path.endsWith('/enable')) enabled = true;
    if (options.path.endsWith('/disable')) enabled = false;
    if (loseWriteResponse) {
      throw DioException(
          requestOptions: options, type: DioExceptionType.connectionError);
    }
    return ResponseBody.fromString(
        jsonEncode(options.path.endsWith('/invoke') ? {'result': 42} : {}), 200,
        headers: {
          Headers.contentTypeHeader: ['application/json']
        });
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  late _Transport transport;
  late ServerPluginsApi api;
  late _Auth auth;
  late int epoch;

  setUp(() {
    epoch = 7;
    transport = _Transport();
    final dio = Dio(BaseOptions(baseUrl: 'http://fixture.local'))
      ..httpClientAdapter = transport;
    api = ServerPluginsApi(dio, currentEpoch: () => epoch);
    auth = _Auth();
  });

  Future<void> open(WidgetTester tester,
      {Brightness brightness = Brightness.light, double scale = 1}) async {
    await tester.pumpWidget(ProviderScope(
        overrides: [
          authStateProvider.overrideWith(() => auth),
          serverPluginsApiProvider.overrideWithValue(api),
          playerBaseUrlProvider
              .overrideWithValue(Uri.parse('http://fixture.local')),
        ],
        child: MaterialApp(
            theme: ThemeData(brightness: brightness),
            builder: (context, child) => MediaQuery(
                data: MediaQuery.of(context)
                    .copyWith(textScaler: TextScaler.linear(scale)),
                child: child!),
            home: const ServerPluginsScreen())));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 10));
  }

  Future<void> press(WidgetTester tester, String label) async {
    final dialog = find.byType(AlertDialog);
    final target = (dialog.evaluate().isEmpty
            ? find.text(label)
            : find.descendant(of: dialog, matching: find.text(label)))
        .first;
    await tester.ensureVisible(target);
    await tester.pumpAndSettle();
    await tester.tap(target);
    await tester.pumpAndSettle();
  }

  testWidgets('loading and empty states use real list response',
      (tester) async {
    transport.holdList = Completer<void>();
    transport.empty = true;
    await open(tester);
    expect(find.text('Loading server plugins…'), findsOneWidget);
    transport.holdList!.complete();
    await tester.pumpAndSettle();
    expect(find.textContaining('No server plugins installed'), findsOneWidget);
    expect(transport.requests.single.path, '/api/v1/admin/plugins');
    expect(transport.requests.single.extra['credentialEpoch'], 7);
  });

  testWidgets(
      'confirmation, enable, self-check and disable update saved status',
      (tester) async {
    await open(tester);
    await press(tester, 'Enable');
    expect(find.text('Enable Probe plugin?'), findsOneWidget);
    await press(tester, 'Cancel');
    expect(transport.enabled, false);
    expect(transport.requests.where((r) => r.method == 'POST'), isEmpty);
    await press(tester, 'Enable');
    await press(tester, 'Enable');
    expect(transport.enabled, true);
    expect(find.text('Status: Active'), findsOneWidget);
    await press(tester, 'Run self-check');
    expect(
        find.textContaining('Self-check passed (result 42)'), findsOneWidget);
    final invoke =
        transport.requests.singleWhere((r) => r.path.endsWith('/invoke'));
    expect(invoke.data, {'capability': 'probe'});
    expect(invoke.extra['credentialEpoch'], 7);
    await press(tester, 'Disable');
    await press(tester, 'Disable');
    expect(transport.enabled, false);
    expect(find.text('Status: Disabled'), findsOneWidget);
    expect(find.text('Run self-check'), findsNothing);
  });

  testWidgets('restart keeps enabled plugin self-check available',
      (tester) async {
    transport.enabled = true;
    transport.restarted = true;
    await open(tester);
    expect(find.text('Status: Enabled · ready for self-check'), findsOneWidget);
    expect(find.text('Disable'), findsOneWidget);
    await press(tester, 'Run self-check');
    expect(find.textContaining('Self-check passed'), findsOneWidget);
  });

  testWidgets('403 clears plugin rows and actions', (tester) async {
    await open(tester);
    transport.listStatus = 403;
    await press(tester, 'Refresh');
    expect(find.text('Administrator access required.'), findsOneWidget);
    expect(find.text('Probe plugin'), findsNothing);
    expect(find.text('Enable'), findsNothing);
  });

  testWidgets('lost mutation response refreshes status without replay',
      (tester) async {
    await open(tester);
    transport.loseWriteResponse = true;
    await press(tester, 'Enable');
    await press(tester, 'Enable');
    expect(find.textContaining('Cannot reach the server'), findsOneWidget);
    expect(find.text('Status: Active'), findsOneWidget);
    expect(transport.requests.where((r) => r.method == 'POST').length, 1);
    transport.loseWriteResponse = false;
    await press(tester, 'Refresh');
    expect(find.textContaining('Cannot reach the server'), findsNothing);
    expect(transport.requests.where((r) => r.method == 'POST').length, 1);
  });

  testWidgets('account change hides rows and dismisses pending confirmation',
      (tester) async {
    await open(tester);
    await press(tester, 'Enable');
    epoch++;
    auth.regularUser();
    await tester.pumpAndSettle();
    expect(find.text('Administrator access required.'), findsOneWidget);
    expect(find.text('Enable Probe plugin?'), findsNothing);
    expect(find.text('Probe plugin'), findsNothing);
    expect(transport.requests.where((r) => r.method == 'POST'), isEmpty);
  });

  testWidgets('late list result cannot expose previous account plugins',
      (tester) async {
    transport.holdList = Completer<void>();
    await open(tester);
    epoch++;
    auth.regularUser();
    await tester.pumpAndSettle();
    transport.holdList!.complete();
    await tester.pumpAndSettle();
    expect(find.text('Probe plugin'), findsNothing);
    expect(find.text('Administrator access required.'), findsOneWidget);
  });

  for (final brightness in Brightness.values) {
    testWidgets('long content fits narrow $brightness with large text',
        (tester) async {
      tester.view.physicalSize = const Size(320, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      transport.longText = true;
      await open(tester, brightness: brightness, scale: 1.5);
      await tester.pumpAndSettle();
      await press(tester, 'Enable');
      expect(tester.takeException(), isNull);
      await press(tester, 'Cancel');
      expect(tester.takeException(), isNull);
    });
  }

  test('client rejects old epoch before any request', () async {
    epoch++;
    await expectLater(
        api.setEnabled('probe-plugin', true, 7), throwsStateError);
    await expectLater(api.check('probe-plugin', 7), throwsStateError);
    await expectLater(api.list(7), throwsStateError);
    expect(transport.requests, isEmpty);
  });
}
