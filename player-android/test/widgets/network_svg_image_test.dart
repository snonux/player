// Tests for SVG detection and NetworkSvgImage (network_svg_image.dart).
//
// The SVG fetcher provider is overridden, so no network is used. Parsing
// runs for real, which is what makes the malformed-document cases meaningful.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/widgets/network_svg_image.dart';
import 'package:vector_graphics/vector_graphics.dart';

import '../support/svg_test_support.dart';

void main() {
  Future<void> pumpImage(
    WidgetTester tester,
    RecordingSvgFetcher fetcher, {
    String url = 'https://player.example/s/token/stream',
    Map<String, String> headers = const {},
  }) =>
      tester.pumpWidget(ProviderScope(
        overrides: [svgFetcherProvider.overrideWithValue(fetcher.fetcher)],
        child: MaterialApp(
          home: NetworkSvgImage(
            imageUrl: url,
            headers: headers,
            placeholder: (_, __) => const Text('loading'),
            errorWidget: (_, __, ___) => const Text('image unavailable'),
          ),
        ),
      ));

  group('isSvgSource', () {
    test('recognises the file name extension in any case', () {
      const url = 'https://player.example/api/v1/media/1/thumbnail';
      expect(isSvgSource(fileName: 'logo.svg', url: url), isTrue);
      expect(isSvgSource(fileName: 'LOGO.SVG ', url: url), isTrue);
    });

    test('recognises the URL path extension, ignoring the query', () {
      expect(isSvgSource(url: 'https://player.example/logo.svg?v=2'), isTrue);
    });

    test('rejects bitmaps and names that merely contain svg', () {
      const url = 'https://player.example/api/v1/media/1/stream';
      expect(isSvgSource(fileName: 'photo.jpg', url: url), isFalse);
      expect(isSvgSource(fileName: 'svg-export.png', url: url), isFalse);
      expect(isSvgSource(fileName: null, url: url), isFalse);
      expect(isSvgSource(url: 'https://player.example/a.png?n=x.svg'), isFalse);
    });
  });

  testWidgets('shows the placeholder, then renders a valid SVG',
      (tester) async {
    final fetcher = RecordingSvgFetcher();
    await pumpImage(tester, fetcher, headers: {'Authorization': 'Bearer t'});
    expect(find.text('loading'), findsOneWidget);

    await pumpUntilFound(tester, find.byType(VectorGraphic));

    expect(find.byType(VectorGraphic), findsOneWidget);
    expect(find.text('loading'), findsNothing);
    expect(find.text('image unavailable'), findsNothing);
    final (uri, headers) = fetcher.requests.single;
    expect(uri.toString(), 'https://player.example/s/token/stream');
    expect(headers, {'Authorization': 'Bearer t'});
  });

  testWidgets('malformed SVG shows the error widget', (tester) async {
    await pumpImage(tester, RecordingSvgFetcher(body: kMalformedSvg));
    await pumpUntilFound(tester, find.text('image unavailable'));

    expect(find.text('image unavailable'), findsOneWidget);
    expect(find.byType(VectorGraphic), findsNothing);
  });

  testWidgets('a response that is not SVG shows the error widget',
      (tester) async {
    await pumpImage(tester, RecordingSvgFetcher(body: '{"error":"not found"}'));
    await pumpUntilFound(tester, find.text('image unavailable'));

    expect(find.text('image unavailable'), findsOneWidget);
  });

  testWidgets('a failed download shows the error widget', (tester) async {
    await pumpImage(tester, RecordingSvgFetcher(error: StateError('offline')));
    await pumpUntilFound(tester, find.text('image unavailable'));

    expect(find.text('image unavailable'), findsOneWidget);
  });

  testWidgets('a failure is not cached: the next visit downloads again',
      (tester) async {
    await pumpImage(tester, RecordingSvgFetcher(error: StateError('offline')));
    await pumpUntilFound(tester, find.text('image unavailable'));
    await tester.pumpWidget(const SizedBox.shrink());

    await pumpImage(tester, RecordingSvgFetcher());
    await pumpUntilFound(tester, find.byType(VectorGraphic));

    expect(find.byType(VectorGraphic), findsOneWidget);
  });
}
