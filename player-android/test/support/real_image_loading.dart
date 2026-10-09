// Lets widget tests run the real bitmap pipeline of `Image.network` and
// `CachedNetworkImage`: HTTP answers come from a handler, the image disk
// cache writes to a temporary directory and its index uses SQLite over FFI.
// Without this the cache needs platform plugins and never completes, so a
// decode failure could not be produced in a test.

import 'dart:io';

import 'package:flutter/painting.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

import 'fake_http.dart';

/// One server for the whole test file. The image cache keeps its HTTP
/// client for the lifetime of the process, so the overrides it was created
/// under must stay valid; only the handler changes per test.
final FakeHttpOverrides _server = FakeHttpOverrides((_, __) => _notFound);

const _notFound = FakeHttpReply([], status: HttpStatus.notFound);
const _pathProvider = MethodChannel('plugins.flutter.io/path_provider');

/// Directory of the image disk cache for this test file.
late Directory _cacheDirectory;

/// Call once in a group (or in `main`) whose tests use
/// [startRealImageLoading]. The image cache keeps its directory for the
/// lifetime of the process, so it is created once per test file and removed
/// at the end.
///
/// Restriction: only ONE test per file may load through
/// `CachedNetworkImage`. Its cache manager is a process-wide singleton and
/// keeps work pending in the fake-async zone of the test that first used
/// it; a second test would wait for that work forever. Put several
/// scenarios into that one test, each with its own image URL (cached files
/// outlive a scenario). `Image.network` has no such state and may be used
/// by any number of tests.
void useRealImageLoading() {
  setUpAll(() {
    _cacheDirectory = Directory.systemTemp.createTempSync('player_images');
  });
  tearDownAll(() => _cacheDirectory.deleteSync(recursive: true));
}

/// Starts answering image requests with [handler] for the current test and
/// returns the server, whose `requests` list shows what was fetched.
/// Finish the test with [settleImageCache].
FakeHttpOverrides startRealImageLoading(
  WidgetTester tester,
  FakeHttpHandler handler,
) {
  // The in-process factory: the isolate-based one binds its reply port to
  // the fake-async zone of the first test and never answers a later test.
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfiNoIsolate;
  tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      _pathProvider, (_) async => _cacheDirectory.path);

  final previous = HttpOverrides.current;
  HttpOverrides.global = _server;
  debugNetworkImageHttpClientProvider = () => _server.createHttpClient(null);
  _server
    ..handler = handler
    ..requests.clear();
  addTearDown(() {
    HttpOverrides.global = previous;
    _server.handler = (_, __) => _notFound;
  });
  return _server;
}

/// Ends a test that called [startRealImageLoading].
///
/// The framework checks at the end of the test body that debug hooks are
/// back to their defaults and that no timer is pending, so the HTTP hook is
/// cleared here and the image cache's housekeeping timers, which run on
/// the test's fake clock, are given time to fire.
Future<void> settleImageCache(WidgetTester tester) {
  debugNetworkImageHttpClientProvider = null;
  return tester.pump(const Duration(seconds: 11));
}
