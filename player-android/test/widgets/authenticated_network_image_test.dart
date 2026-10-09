import 'package:cached_network_image/cached_network_image.dart';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/widgets/authenticated_network_image.dart';
import 'package:player_android/widgets/network_svg_image.dart';
import 'package:vector_graphics/vector_graphics.dart';

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

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
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

  group('SVG', () {
    Future<void> pumpSvg(WidgetTester tester, RecordingSvgFetcher fetcher) =>
        tester.pumpWidget(ProviderScope(
          overrides: [
            playerBaseUrlProvider.overrideWithValue(Uri.parse(baseUrl)),
            tokenStorageProvider
                .overrideWithValue(_TokenStorage('pt-restored')),
            cookieJarProvider.overrideWithValue(CookieJar()),
            credentialMutationQueueProvider.overrideWithValue(
                CredentialMutationQueue(credentialsEnabled: true)),
            svgFetcherProvider.overrideWithValue(fetcher.fetcher),
          ],
          child: image(imageUrl, sourceName: 'sample-svg.svg'),
        ));

    testWidgets('SVG source renders as a vector with the bearer token',
        (tester) async {
      final fetcher = RecordingSvgFetcher();
      await pumpSvg(tester, fetcher);
      await pumpUntilFound(tester, find.byType(VectorGraphic));

      expect(find.byType(VectorGraphic), findsOneWidget);
      expect(find.byType(CachedNetworkImage), findsNothing);
      final (uri, headers) = fetcher.requests.single;
      expect(uri.toString(), imageUrl);
      expect(headers['Authorization'], 'Bearer pt-restored');
      final svg = tester.widget<NetworkSvgImage>(find.byType(NetworkSvgImage));
      expect(svg.cacheKey, isNot(contains('pt-restored')));
      expect(svg.cacheKey, isNot(imageUrl));
    });

    testWidgets('malformed SVG shows the error widget', (tester) async {
      await pumpSvg(tester, RecordingSvgFetcher(body: kMalformedSvg));
      await pumpUntilFound(tester, find.text('image unavailable'));

      expect(find.text('image unavailable'), findsOneWidget);
      expect(find.byType(VectorGraphic), findsNothing);
    });

    testWidgets('SVG on another origin is not requested', (tester) async {
      final fetcher = RecordingSvgFetcher();
      await tester.pumpWidget(ProviderScope(
        overrides: [
          playerBaseUrlProvider.overrideWithValue(Uri.parse(baseUrl)),
          tokenStorageProvider.overrideWithValue(_TokenStorage('pt-restored')),
          cookieJarProvider.overrideWithValue(CookieJar()),
          svgFetcherProvider.overrideWithValue(fetcher.fetcher),
        ],
        child: image('https://other.example/logo.svg'),
      ));
      await tester.pump();

      expect(fetcher.requests, isEmpty);
      expect(find.text('image unavailable'), findsOneWidget);
    });
  });
}
