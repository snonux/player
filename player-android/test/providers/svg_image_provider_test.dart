// Tests for svgImageProvider (svg_image_provider.dart): caching, retry,
// cancellation, account separation and the session caches. Compilation,
// decoding and rasterisation run for real; only the download is scripted.

import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/svg_image_provider.dart';
import 'package:player_android/services/svg_compiler.dart';
import 'package:player_android/services/svg_document.dart';
import 'package:player_android/services/svg_fetcher.dart';

import '../support/svg_test_support.dart';

final _uri = Uri.parse('https://player.example/s/secret-token/stream');
final _request = SvgRequest(uri: _uri);

/// [source] rasterised for a 100x100 pixel box.
SvgImageRequest _image(SvgRequest source, {double side = 100}) =>
    SvgImageRequest(source: source, width: side, height: side);

/// Wraps the real compiler and counts how often it is asked to work.
class _CountingCompiler {
  final _inner = IsolateSvgCompiler();
  int calls = 0;

  Future<CompiledSvg> call(Uint8List bytes, {bool Function()? isCancelled}) {
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
      container.listen(svgImageProvider(_image(request)), (_, __) {});
  try {
    await container.read(svgImageProvider(_image(request)).future);
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
  group('bitmaps', _bitmapTests);
  group('shared downloads', _sharedLoadTests);
  group('bitmap lifetime', _bitmapLifetimeTests);
  group('retry and account separation', _retryTests);
  group('cancellation', _cancellationTests);
  group('content that is not SVG', _notSvgTests);
  group('content that is not SVG: scope', _notSvgScopeTests);
  group('session cache', _sessionCacheTests);
  group('work in flight', _inFlightTests);
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
        container.listen(svgImageProvider(_image(request)), (_, __) {});
    addTearDown(subscription.close);

    final info = await container.read(svgImageProvider(_image(request)).future);

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

/// Loads [request] at [side] pixels and returns the size of its bitmap.
Future<int> _bitmapWidth(
    ProviderContainer container, SvgRequest request, double side) async {
  final provider = svgImageProvider(_image(request, side: side));
  final subscription = container.listen(provider, (_, __) {});
  try {
    return (await container.read(provider.future)).image.width;
  } finally {
    subscription.close();
    await Future<void>.delayed(Duration.zero);
  }
}

void _bitmapTests() {
  test('a tile and the viewer get bitmaps of their own from one download',
      () async {
    final fetcher = RecordingSvgFetcher();
    final compiler = _CountingCompiler();
    final container = _container(fetcher, compiler: compiler);

    expect(await _bitmapWidth(container, _request, 144), 192);
    expect(await _bitmapWidth(container, _request, 1440), 1536);

    expect(fetcher.requests, hasLength(1));
    expect(compiler.calls, 1);
    expect(container.read(svgImageCacheProvider).length, 2);
  });
}

void _sharedLoadTests() {
  test('two sizes requested together share one download and compilation',
      () async {
    final fetcher = RecordingSvgFetcher()..gate = Completer<void>();
    final compiler = _CountingCompiler();
    final container = _container(fetcher, compiler: compiler);
    final tile = svgImageProvider(_image(_request, side: 144));
    final screen = svgImageProvider(_image(_request, side: 1440));
    final subscriptions = [
      container.listen(tile, (_, __) {}),
      container.listen(screen, (_, __) {}),
    ];
    addTearDown(() {
      for (final subscription in subscriptions) {
        subscription.close();
      }
    });
    await Future<void>.delayed(Duration.zero);

    fetcher.gate!.complete();
    final widths = [
      (await container.read(tile.future)).image.width,
      (await container.read(screen.future)).image.width,
    ];

    expect(widths, [192, 1536]);
    expect(fetcher.requests, hasLength(1));
    expect(compiler.calls, 1);
  });

  test('a shared download survives one of its two requesters leaving',
      () async {
    final fetcher = RecordingSvgFetcher()..gate = Completer<void>();
    final container = _container(fetcher);
    final tile = svgImageProvider(_image(_request, side: 144));
    final screen = svgImageProvider(_image(_request, side: 1440));
    final leaving = container.listen(tile, (_, __) {});
    final staying = container.listen(screen, (_, __) {});
    addTearDown(staying.close);
    await Future<void>.delayed(Duration.zero);

    leaving.close();
    await Future<void>.delayed(Duration.zero);
    expect(fetcher.requests.single.cancel.isCancelled, isFalse);

    fetcher.gate!.complete();
    expect((await container.read(screen.future)).image.width, 1536);
  });
}

void _bitmapLifetimeTests() {
  test('a cached bitmap is reused without the compiled drawing', () async {
    final container = _container(RecordingSvgFetcher());
    await _showOnce(container, _request);
    // Drop the compiled drawing; only the bitmap is left.
    container.read(svgMemoryCacheProvider).clear();

    await _showOnce(container, _request);

    expect(container.read(svgMemoryCacheProvider).get(_request), isNull);
  });

  test('the bitmap is released when nothing shows it or caches it', () async {
    final container = _container(RecordingSvgFetcher());
    final provider = svgImageProvider(_image(_request));
    final subscription = container.listen(provider, (_, __) {});
    final raster = await container.read(provider.future);
    expect(raster.image.debugDisposed, isFalse);

    container.read(svgImageCacheProvider).clear();
    subscription.close();
    await Future<void>.delayed(Duration.zero);

    expect(raster.image.debugDisposed, isTrue);
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
        container.listen(svgImageProvider(_image(_request)), (_, __) {});
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
}

void _notSvgScopeTests() {
  test('is remembered per credential, not per URL alone', () async {
    final fetcher = RecordingSvgFetcher(body: 'BBBB');
    final container = _container(fetcher);
    final other = SvgRequest(uri: _uri, headers: const {'Cookie': 's=2'});

    await expectLater(_showOnce(container, _request), _rejects('Not an SVG'));
    fetcher.body = kValidSvg;
    await _showOnce(container, other);

    expect(fetcher.requests, hasLength(2));
  });

  test('noticed by the downloader in the first bytes is remembered too',
      () async {
    final fetcher = RecordingSvgFetcher(error: const NotSvgException());
    final container = _container(fetcher);

    await expectLater(_showOnce(container, _request), _rejects('Not an SVG'));
    await expectLater(_showOnce(container, _request), _rejects('Not an SVG'));

    expect(fetcher.requests, hasLength(1));
  });

  test('an embedded bitmap is refused, not decoded', () async {
    final png = await makePng(2, 2);
    final container = _container(
        RecordingSvgFetcher(body: svgDocument(body: embeddedImage(png))));

    await expectLater(_showOnce(container, _request),
        _rejects('element <image> is not supported'));
  });
}

/// Auth state that a test can switch, as login and logout do in the app.
class _SwitchableAuth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated();

  void logOut() => state = const AsyncData(AuthState.unauthenticated());
}

final _serverUrl = StateProvider<Uri>((ref) => Uri.parse('https://a.example'));

/// A container wired like the app: the reset provider is kept alive, as
/// the root widget does, and auth state and server URL can be changed.
Future<ProviderContainer> _sessionContainer(
    [RecordingSvgFetcher? fetcher]) async {
  final container = ProviderContainer(overrides: [
    svgFetcherProvider
        .overrideWithValue((fetcher ?? RecordingSvgFetcher()).fetcher),
    authStateProvider.overrideWith(_SwitchableAuth.new),
    playerBaseUrlProvider.overrideWith((ref) => ref.watch(_serverUrl)),
  ]);
  addTearDown(container.dispose);
  container.listen(svgCacheResetProvider, (_, __) {});
  await container.read(authStateProvider.future);
  return container;
}

void _sessionCacheTests() {
  final notSvg = SvgRequest(uri: Uri.parse('https://player.example/photo'));

  /// Fills the cache with one drawing and one not-SVG note.
  Future<SvgMemoryCache> filledCache(ProviderContainer container) async {
    await _showOnce(container, _request);
    final cache = container.read(svgMemoryCacheProvider)..markNotSvg(notSvg);
    expect(cache.get(_request), isNotNull);
    expect(container.read(svgImageCacheProvider).length, 1);
    return cache;
  }

  test('is emptied at once on logout', () async {
    final container = await _sessionContainer();
    final cache = await filledCache(container);

    (container.read(authStateProvider.notifier) as _SwitchableAuth).logOut();
    await Future<void>.delayed(Duration.zero);

    // The same object the widgets hold is empty, not merely replaced.
    expect(cache.get(_request), isNull);
    expect(cache.isKnownNotSvg(notSvg), isFalse);
    expect(container.read(svgImageCacheProvider).length, 0);
  });

  test('is emptied at once when the server changes', () async {
    final container = await _sessionContainer();
    final cache = await filledCache(container);

    container.read(_serverUrl.notifier).state = Uri.parse('https://b.example');
    await Future<void>.delayed(Duration.zero);

    expect(cache.get(_request), isNull);
    expect(cache.isKnownNotSvg(notSvg), isFalse);
    expect(container.read(svgImageCacheProvider).length, 0);
  });
}

void _inFlightTests() {
  test('work in flight during a logout does not refill the caches', () async {
    final fetcher = RecordingSvgFetcher()..gate = Completer<void>();
    final container = await _sessionContainer(fetcher);
    final provider = svgImageProvider(_image(_request));
    final subscription = container.listen(provider, (_, __) {});
    addTearDown(subscription.close);
    await Future<void>.delayed(Duration.zero);

    // The download is still running when the user logs out.
    (container.read(authStateProvider.notifier) as _SwitchableAuth).logOut();
    await Future<void>.delayed(Duration.zero);
    fetcher.gate!.complete();
    await container.read(provider.future);

    expect(container.read(svgMemoryCacheProvider).get(_request), isNull);
    expect(container.read(svgImageCacheProvider).length, 0);
  });

  test('bytes that fail to decode are never cached', () async {
    final container = ProviderContainer(overrides: [
      svgFetcherProvider.overrideWithValue(RecordingSvgFetcher().fetcher),
      // A compiler result that is not valid vector_graphics data.
      svgCompilerProvider.overrideWithValue((bytes, {isCancelled}) async =>
          CompiledSvg(Uint8List.fromList([1, 2, 3, 4]), 1)),
    ]);
    addTearDown(container.dispose);

    await expectLater(_showOnce(container, _request), throwsA(anything));

    expect(container.read(svgMemoryCacheProvider).get(_request), isNull);
  });
}

void _memoryCacheTests() {
  CompiledSvg bytes(int length) => CompiledSvg(Uint8List(length), 1);
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
