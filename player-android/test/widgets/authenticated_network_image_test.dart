import 'package:cached_network_image/cached_network_image.dart';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/services/svg_fetcher.dart';
import 'package:player_android/widgets/authenticated_network_image.dart';
import 'package:player_android/widgets/network_svg_image.dart';

import '../support/fake_http.dart';
import '../support/real_image_loading.dart';
import '../support/svg_test_support.dart';

class _TokenStorage implements TokenStorage {
  _TokenStorage(this.token);
  String? token;

  @override
  Future<String?> readToken() async => token;
  @override
  Future<void> writeToken(String value) async => token = value;
  @override
  Future<void> deleteToken() async => token = null;
}

const baseUrl = 'https://player.example';
const imageUrl = '$baseUrl/api/v1/media/1/thumbnail';

Widget image(String url, {String? sourceName}) => MaterialApp(
      home: Scaffold(
        body: AuthenticatedNetworkImage(
          imageUrl: url,
          sourceName: sourceName,
          placeholder: (_, __) => const Text('loading'),
          errorWidget: (_, __, ___) => const Text('image unavailable'),
        ),
      ),
    );

/// Scope with a signed-in account; [fetcher] serves SVG downloads.
Widget signedIn(Widget child, {RecordingSvgFetcher? fetcher}) => ProviderScope(
      overrides: [
        playerBaseUrlProvider.overrideWithValue(Uri.parse(baseUrl)),
        tokenStorageProvider.overrideWithValue(_TokenStorage('pt-restored')),
        cookieJarProvider.overrideWithValue(CookieJar()),
        credentialMutationQueueProvider.overrideWithValue(
            CredentialMutationQueue(credentialsEnabled: true)),
        if (fetcher != null)
          svgFetcherProvider.overrideWithValue(fetcher.fetcher),
      ],
      child: child,
    );

final _error = find.text('image unavailable');
final _picture = find.byType(SvgPictureBox);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  // Must stay the first group that creates a CachedNetworkImage: the image
  // cache keeps the HTTP client it was first used with.
  group('real bitmap pipeline', _realBitmapTests);
  group('SVG by file name', _svgTests);
  group('request credentials', _credentialTests);
  group('origin check', _originTests);
  group('cache key', _cacheKeyTests);
  group('credential gate', _credentialGateTests);
}

void _credentialTests() {
  testWidgets('protected image receives restored bearer and session cookie',
      (tester) async {
    final jar = CookieJar();
    await jar.saveFromResponse(
        Uri.parse(imageUrl), [Cookie('session', 'session-value')]);
    await tester.pumpWidget(ProviderScope(
      overrides: [
        playerBaseUrlProvider.overrideWithValue(Uri.parse(baseUrl)),
        tokenStorageProvider.overrideWithValue(_TokenStorage('pt-restored')),
        cookieJarProvider.overrideWithValue(jar),
        credentialMutationQueueProvider.overrideWithValue(
            CredentialMutationQueue(credentialsEnabled: true)),
      ],
      child: image(imageUrl),
    ));
    expect(find.text('loading'), findsOneWidget);
    await tester.pump();
    final cached =
        tester.widget<CachedNetworkImage>(find.byType(CachedNetworkImage));
    expect(cached.httpHeaders?['Authorization'], 'Bearer pt-restored');
    expect(cached.httpHeaders?['Cookie'], 'session=session-value');
  });

  testWidgets('no bearer is attached when storage has no token',
      (tester) async {
    await tester.pumpWidget(ProviderScope(
      overrides: [
        playerBaseUrlProvider.overrideWithValue(Uri.parse(baseUrl)),
        tokenStorageProvider.overrideWithValue(_TokenStorage(null)),
        cookieJarProvider.overrideWithValue(CookieJar()),
      ],
      child: image(imageUrl),
    ));
    await tester.pump();
    final cached =
        tester.widget<CachedNetworkImage>(find.byType(CachedNetworkImage));
    expect(cached.httpHeaders?.containsKey('Authorization'), isFalse);
  });
}

void _originTests() {
  testWidgets('different origin does not create an image request',
      (tester) async {
    await tester.pumpWidget(ProviderScope(
      overrides: [
        playerBaseUrlProvider.overrideWithValue(Uri.parse(baseUrl)),
        tokenStorageProvider.overrideWithValue(_TokenStorage('pt-restored')),
        cookieJarProvider.overrideWithValue(CookieJar()),
      ],
      child: image('https://other.example/image'),
    ));
    await tester.pump();
    expect(find.byType(CachedNetworkImage), findsNothing);
    expect(find.text('image unavailable'), findsOneWidget);
  });
}

void _cacheKeyTests() {
  testWidgets('image cache key changes when the account token changes',
      (tester) async {
    final storage = _TokenStorage('pt-account-a');
    final container = ProviderContainer(overrides: [
      playerBaseUrlProvider.overrideWithValue(Uri.parse(baseUrl)),
      tokenStorageProvider.overrideWithValue(storage),
      cookieJarProvider.overrideWithValue(CookieJar()),
      credentialMutationQueueProvider
          .overrideWithValue(CredentialMutationQueue(credentialsEnabled: true)),
    ]);
    addTearDown(container.dispose);
    Future<String?> pumpImage() async {
      await tester.pumpWidget(UncontrolledProviderScope(
          container: container, child: image(imageUrl)));
      await tester.pump();
      return tester
          .widget<CachedNetworkImage>(find.byType(CachedNetworkImage))
          .cacheKey;
    }

    final firstKey = await pumpImage();
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container, child: const SizedBox.shrink()));
    await tester.pump(); // Dispose the old image's autoDispose header provider.
    storage.token = 'pt-account-b';
    final secondKey = await pumpImage();

    expect(firstKey, isNotNull);
    expect(secondKey, isNot(equals(firstKey)));
    expect(firstKey, isNot(contains('pt-account-a')));
    expect(secondKey, isNot(contains('pt-account-b')));
  });
}

void _credentialGateTests() {
  testWidgets('disabled credential gate omits stored bearer and cookie',
      (tester) async {
    final jar = CookieJar();
    await jar.saveFromResponse(
        Uri.parse(imageUrl), [Cookie('session', 'old-session')]);
    final gate = CredentialMutationQueue(credentialsEnabled: true)
      ..beginAuthChange();
    await tester.pumpWidget(ProviderScope(
      overrides: [
        playerBaseUrlProvider.overrideWithValue(Uri.parse(baseUrl)),
        tokenStorageProvider.overrideWithValue(_TokenStorage('pt-stale')),
        cookieJarProvider.overrideWithValue(jar),
        credentialMutationQueueProvider.overrideWithValue(gate),
      ],
      child: image(imageUrl),
    ));
    await tester.pump();
    final cached =
        tester.widget<CachedNetworkImage>(find.byType(CachedNetworkImage));
    expect(cached.httpHeaders, isEmpty);
  });
}

void _svgTests() {
  testWidgets('renders as a vector with the bearer token', (tester) async {
    final fetcher = RecordingSvgFetcher();
    await tester.pumpWidget(signedIn(
        image(imageUrl, sourceName: 'sample-svg.svg'),
        fetcher: fetcher));
    await pumpUntilFound(tester, _picture);

    expect(find.byType(CachedNetworkImage), findsNothing);
    expect(fetcher.requests.single.uri.toString(), imageUrl);
    expect(
        fetcher.requests.single.headers['Authorization'], 'Bearer pt-restored');
  });

  testWidgets('malformed SVG shows the error widget', (tester) async {
    await tester.pumpWidget(signedIn(
        image(imageUrl, sourceName: 'sample-svg.svg'),
        fetcher: RecordingSvgFetcher(body: kMalformedSvg)));
    await pumpUntilFound(tester, _error);

    expect(_picture, findsNothing);
  });

  testWidgets('SVG on another origin is not requested', (tester) async {
    final fetcher = RecordingSvgFetcher();
    await tester.pumpWidget(
        signedIn(image('https://other.example/logo.svg'), fetcher: fetcher));
    await tester.pump();

    expect(fetcher.requests, isEmpty);
    expect(_error, findsOneWidget);
  });

  testWidgets('an invalid URL shows the error widget instead of throwing',
      (tester) async {
    await tester.pumpWidget(signedIn(image('http://[bad')));
    await tester.pump();

    expect(tester.takeException(), isNull);
    expect(_error, findsOneWidget);
  });
}

/// Shows the image at [path] and waits for [result] to appear. The SVG
/// fetcher serves the same kind of content as the fake server does there.
Future<RecordingSvgFetcher> _loadReal(
    WidgetTester tester, String path, Finder result) async {
  final fetcher =
      RecordingSvgFetcher(body: path == '/cover' ? kValidSvg : 'not an image');
  await tester.pumpWidget(const SizedBox.shrink());
  await tester.pumpWidget(signedIn(image('$baseUrl$path'), fetcher: fetcher));
  await pumpUntilFound(tester, result);
  return fetcher;
}

/// Downloads through the real `CachedNetworkImage` pipeline and its disk
/// cache, so failures come from the actual bitmap decoder. The scenarios
/// share one test because only one test per file may use that cache (see
/// real_image_loading.dart).
void _realBitmapTests() {
  useRealImageLoading();
  final replies = <String, FakeHttpReply>{
    '/garbage': FakeHttpReply(List.filled(64, 0x42)),
    '/missing': const FakeHttpReply([], status: 404),
    '/cover': FakeHttpReply(svgBytes(kValidSvg)),
  };

  testWidgets('decode failures, missing files, SVG content and bitmaps',
      (tester) async {
    replies['/photo'] =
        FakeHttpReply((await tester.runAsync(() => makePng(4, 4)))!);
    final server =
        startRealImageLoading(tester, (uri, _) => replies[uri.path]!);

    // Not an image: downloaded with credentials, rejected by the decoder,
    // checked once for SVG content, then the error widget.
    var fetcher = await _loadReal(tester, '/garbage', _error);
    expect(server.requests.single.$2['authorization'], 'Bearer pt-restored');
    expect(fetcher.requests, hasLength(1));
    expect(_picture, findsNothing);

    // Missing: a transport error, so no SVG attempt.
    fetcher = await _loadReal(tester, '/missing', _error);
    expect(fetcher.requests, isEmpty);

    // A folder cover that is an SVG: neither URL nor caller name the
    // format, the content decides.
    fetcher = await _loadReal(tester, '/cover', _picture);
    expect(_error, findsNothing);
    expect(
        fetcher.requests.single.headers['Authorization'], 'Bearer pt-restored');

    // A real bitmap never touches the SVG path.
    fetcher = await _loadReal(tester, '/photo', find.byType(RawImage));
    expect(tester.widget<RawImage>(find.byType(RawImage)).image, isNotNull);
    expect(fetcher.requests, isEmpty);
    await settleImageCache(tester);
  });
}
