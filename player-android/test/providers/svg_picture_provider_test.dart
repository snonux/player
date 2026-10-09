// Tests for svgPictureProvider (svg_picture_provider.dart): caching, retry,
// cancellation, account separation and the session cache. Compilation and
// picture decoding run for real; only the download is scripted.

import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/svg_picture_provider.dart';
import 'package:player_android/services/svg_compiler.dart';
import 'package:player_android/services/svg_document.dart';
import 'package:player_android/services/svg_fetcher.dart';

import '../support/svg_test_support.dart';

final _uri = Uri.parse('https://player.example/s/secret-token/stream');
final _request = SvgRequest(uri: _uri);

/// Wraps the real compiler and counts how often it is asked to work.
class _CountingCompiler {
  final _inner = IsolateSvgCompiler();
  int calls = 0;

  Future<Uint8List> call(Uint8List bytes, {bool Function()? isCancelled}) {
    calls++;
    return _inner(bytes, isCancelled: isCancelled);
  }
}

ProviderContainer _container(
  RecordingSvgFetcher fetcher, {
  _CountingCompiler? compiler,
}) {
  final container = ProviderContainer(overrides: [
    svgFetcherProvider.overrideWithValue(fetcher.fetcher),
    if (compiler != null) svgCompilerProvider.overrideWithValue(compiler.call),
  ]);
  addTearDown(container.dispose);
  return container;
}

/// Loads [request] the way a widget does: listen, await, stop listening.
/// The provider is auto-disposed once the event loop turns.
Future<void> _showOnce(ProviderContainer container, SvgRequest request) async {
  final subscription =
      container.listen(svgPictureProvider(request), (_, __) {});
  try {
    await container.read(svgPictureProvider(request).future);
  } finally {
    subscription.close();
    await Future<void>.delayed(Duration.zero);
  }
}

Matcher _rejects(String message) => throwsA(
    isA<SvgException>().having((e) => e.message, 'message', contains(message)));

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  group('SvgRequest', _requestTests);
  group('loading', _loadingTests);
  group('retry and account separation', _retryTests);
  group('cancellation', _cancellationTests);
  group('content that is not SVG', _notSvgTests);
  group('session cache', _sessionCacheTests);
  group('SvgMemoryCache', _memoryCacheTests);
}

void _requestTests() {
  test('is equal only for the same URL and headers', () {
    final withToken = SvgRequest(uri: _uri, headers: const {'Cookie': 'a'});
    expect(SvgRequest(uri: _uri), _request);
    expect(SvgRequest(uri: _uri).hashCode, _request.hashCode);
    expect(SvgRequest(uri: _uri, headers: const {'Cookie': 'a'}), withToken);
    expect(withToken, isNot(_request));
    expect(SvgRequest(uri: _uri, headers: const {'Cookie': 'b'}),
        isNot(withToken));
    expect(SvgRequest(uri: Uri.parse('https://player.example/other')),
        isNot(_request));
  });

  test('does not print the share token or credentials', () {
    final text = SvgRequest(uri: _uri, headers: const {'Cookie': 'session=abc'})
        .toString();
    expect(text, isNot(contains('secret-token')));
    expect(text, isNot(contains('abc')));
  });
}

void _loadingTests() {
  test('produces a picture with the size declared by the SVG', () async {
    final fetcher =
        RecordingSvgFetcher(body: svgDocument(size: 'width="30" height="20"'));
    final container = _container(fetcher);
    final request = SvgRequest(uri: _uri, headers: const {'Cookie': 's=1'});
    final subscription =
        container.listen(svgPictureProvider(request), (_, __) {});
    addTearDown(subscription.close);

    final info = await container.read(svgPictureProvider(request).future);

    expect(info.size.width, 30);
    expect(info.size.height, 20);
    expect(fetcher.requests.single.uri, _uri);
    expect(fetcher.requests.single.headers, {'Cookie': 's=1'});
  });

  test('a compiled SVG is reused when it is shown again', () async {
    final fetcher = RecordingSvgFetcher();
    final compiler = _CountingCompiler();
    final container = _container(fetcher, compiler: compiler);

    await _showOnce(container, _request);
    await _showOnce(container, _request);

    expect(fetcher.requests, hasLength(1));
    expect(compiler.calls, 1);
  });
}

void _retryTests() {
  test('a failure is not cached: showing it again downloads again', () async {
    final fetcher = RecordingSvgFetcher(error: StateError('offline'));
    final container = _container(fetcher);

    await expectLater(_showOnce(container, _request), throwsStateError);
    fetcher.error = null;
    await _showOnce(container, _request);

    expect(fetcher.requests, hasLength(2));
  });

  test('a rejected document is not cached either', () async {
    final fetcher = RecordingSvgFetcher(body: kMalformedSvg);
    final container = _container(fetcher);

    await expectLater(_showOnce(container, _request), _rejects('Invalid SVG'));
    fetcher.body = kValidSvg;
    await _showOnce(container, _request);

    expect(fetcher.requests, hasLength(2));
  });

  test('another account never reuses a cached picture', () async {
    final fetcher = RecordingSvgFetcher();
    final container = _container(fetcher);
    const accountA = {'Authorization': 'Bearer a'};
    const accountB = {'Authorization': 'Bearer b'};

    await _showOnce(container, SvgRequest(uri: _uri, headers: accountA));
    await _showOnce(container, SvgRequest(uri: _uri, headers: accountB));
    await _showOnce(container, SvgRequest(uri: _uri, headers: accountA));

    expect([for (final r in fetcher.requests) r.headers], [accountA, accountB]);
  });
}

void _cancellationTests() {
  test('disposing mid-download cancels it and skips compilation', () async {
    final fetcher = RecordingSvgFetcher()..gate = Completer<void>();
    final compiler = _CountingCompiler();
    final container = _container(fetcher, compiler: compiler);

    final subscription =
        container.listen(svgPictureProvider(_request), (_, __) {});
    await Future<void>.delayed(Duration.zero);
    expect(fetcher.requests.single.cancel.isCancelled, isFalse);

    subscription.close();
    await Future<void>.delayed(Duration.zero);
    expect(fetcher.requests.single.cancel.isCancelled, isTrue);

    // Even if the transport ignores the token and delivers the body, the
    // dropped request must not start compiling.
    fetcher.gate!.complete();
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(compiler.calls, 0);
    expect(container.read(svgMemoryCacheProvider).get(_request), isNull);
  });
}

void _notSvgTests() {
  test('is remembered: a corrupt bitmap is probed once per session', () async {
    final fetcher = RecordingSvgFetcher(body: 'BBBB not an image');
    final container = _container(fetcher, compiler: _CountingCompiler());

    await expectLater(_showOnce(container, _request), _rejects('Not an SVG'));
    await expectLater(_showOnce(container, _request), _rejects('Not an SVG'));

    expect(fetcher.requests, hasLength(1));
  });

  test('is rejected before an isolate is started for it', () async {
    final compiler = _CountingCompiler();
    final container = _container(RecordingSvgFetcher(body: '{"error":"gone"}'),
        compiler: compiler);

    await expectLater(_showOnce(container, _request), _rejects('Not an SVG'));

    expect(compiler.calls, 0);
  });

  test('is remembered per credential, not per URL alone', () async {
    final fetcher = RecordingSvgFetcher(body: 'BBBB');
    final container = _container(fetcher);
    final other = SvgRequest(uri: _uri, headers: const {'Cookie': 's=2'});

    await expectLater(_showOnce(container, _request), _rejects('Not an SVG'));
    fetcher.body = kValidSvg;
    await _showOnce(container, other);

    expect(fetcher.requests, hasLength(2));
  });

  test('an embedded bitmap is refused, not decoded', () async {
    final png = await makePng(2, 2);
    final container = _container(
        RecordingSvgFetcher(body: svgDocument(body: embeddedImage(png))));

    await expectLater(_showOnce(container, _request),
        _rejects('embedded bitmaps are not supported'));
  });
}

/// Auth state that a test can switch, as login and logout do in the app.
class _SwitchableAuth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated();

  void logOut() => state = const AsyncData(AuthState.unauthenticated());
}

void _sessionCacheTests() {
  test('is emptied when the auth state changes', () async {
    final fetcher = RecordingSvgFetcher();
    final container = ProviderContainer(overrides: [
      svgFetcherProvider.overrideWithValue(fetcher.fetcher),
      authStateProvider.overrideWith(_SwitchableAuth.new),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);

    await _showOnce(container, _request);
    expect(container.read(svgMemoryCacheProvider).get(_request), isNotNull);

    (container.read(authStateProvider.notifier) as _SwitchableAuth).logOut();

    expect(container.read(svgMemoryCacheProvider).get(_request), isNull);
    await _showOnce(container, _request);
    expect(fetcher.requests, hasLength(2));
  });
}

void _memoryCacheTests() {
  ByteData bytes(int length) => ByteData(length);
  SvgRequest request(int id) =>
      SvgRequest(uri: Uri.parse('https://player.example/$id'));

  test('evicts the least recently used entry when full', () {
    final cache = SvgMemoryCache(maxBytes: 100)
      ..put(request(1), bytes(20))
      ..put(request(2), bytes(20))
      ..put(request(3), bytes(20))
      ..put(request(4), bytes(20))
      ..put(request(5), bytes(20));
    expect(cache.get(request(1)), isNotNull); // now the most recently used
    cache.put(request(6), bytes(20));

    expect(cache.get(request(2)), isNull);
    expect(cache.get(request(1)), isNotNull);
    expect(cache.get(request(6)), isNotNull);
  });

  test('forgets the oldest not-SVG note when its list is full', () {
    final cache = SvgMemoryCache(maxNotSvg: 2)
      ..markNotSvg(request(1))
      ..markNotSvg(request(2))
      ..markNotSvg(request(3));
    expect(cache.isKnownNotSvg(request(1)), isFalse);
    expect(cache.isKnownNotSvg(request(2)), isTrue);
    expect(cache.isKnownNotSvg(request(3)), isTrue);
  });

  test('replacing an entry does not double-count its size', () {
    final cache = SvgMemoryCache(maxBytes: 100);
    for (var i = 0; i < 10; i++) {
      cache.put(request(1), bytes(25));
    }
    cache.put(request(2), bytes(25));
    expect(cache.get(request(1)), isNotNull);
    expect(cache.get(request(2)), isNotNull);
  });
}
