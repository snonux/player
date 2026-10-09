// Widget tests for NetworkSvgImage and the bitmap-error fallback
// (network_svg_image.dart).
//
// Only the download is scripted; parsing, decoding and painting run for
// real. "Renders" is checked on pixels read back from the render tree, so a
// test cannot pass merely because a widget of the right type exists.

import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/svg_fetcher.dart';
import 'package:player_android/widgets/network_svg_image.dart';

import '../support/svg_test_support.dart';

const _url = 'https://player.example/s/token/stream';
const _boundary = Key('boundary');
final _error = find.text('image unavailable');
final _picture = find.byType(SvgImageBox);

/// The image in a 100x100 box at the top left, inside a repaint boundary
/// whose pixels the tests read back. [show] false removes the image while
/// keeping the provider scope alive.
Widget _app(
  ProviderContainer container, {
  bool show = true,
  BoxFit? fit,
  Map<String, String> headers = const {},
}) =>
    UncontrolledProviderScope(
      container: container,
      child: MaterialApp(
        home: Align(
          alignment: Alignment.topLeft,
          child: RepaintBoundary(
            key: _boundary,
            child: SizedBox(
              width: 100,
              height: 100,
              child: !show
                  ? null
                  : NetworkSvgImage(
                      imageUrl: _url,
                      headers: headers,
                      fit: fit,
                      placeholder: (_, __) => const Text('loading'),
                      errorWidget: (_, __, ___) =>
                          const Text('image unavailable'),
                    ),
            ),
          ),
        ),
      ),
    );

ProviderContainer _container(RecordingSvgFetcher fetcher) {
  final container = ProviderContainer(
      overrides: [svgFetcherProvider.overrideWithValue(fetcher.fetcher)]);
  addTearDown(container.dispose);
  return container;
}

/// Shows [document] and waits until it is painted or rejected.
Future<void> _show(WidgetTester tester, String document, {BoxFit? fit}) async {
  final container = _container(RecordingSvgFetcher(body: document));
  await tester.pumpWidget(_app(container, fit: fit));
  await pumpUntilFound(
      tester,
      find.byWidgetPredicate((w) =>
          w is SvgImageBox || (w is Text && w.data == 'image unavailable')));
}

void main() {
  group('rendering', _renderingTests);
  group('sizing', _sizingTests);
  group('rejected documents', _errorTests);
  group('reloading', _reloadTests);
  group('bitmap error classification', _fallbackTests);
  group('bitmap error fallback', _fallbackWidgetTests);
}

void _renderingTests() {
  testWidgets('shows the placeholder, then paints the SVG', (tester) async {
    final fetcher = RecordingSvgFetcher();
    await tester.pumpWidget(
        _app(_container(fetcher), headers: {'Authorization': 'Bearer t'}));
    expect(find.text('loading'), findsOneWidget);

    await pumpUntilFound(tester, _picture);

    expect(find.text('loading'), findsNothing);
    expect(_error, findsNothing);
    expect(await pixelAt(tester, find.byKey(_boundary)), kSvgRed);
    expect(fetcher.requests.single.uri.toString(), _url);
    expect(fetcher.requests.single.headers, {'Authorization': 'Bearer t'});
  });

  testWidgets('the bitmap is made for the box, not for the declared size',
      (tester) async {
    // Left half red, right half blue, declared as only 2x2 units. A bitmap
    // of that size stretched to 100 px would blur across the middle.
    await _show(
        tester,
        svgDocument(
            size: 'width="2" height="2"',
            body: '<rect width="1" height="2" fill="#ff0000"/>'
                '<rect x="1" width="1" height="2" fill="#0000ff"/>'));
    final boundary = find.byKey(_boundary);

    expect(tester.getSize(_picture), const Size(100, 100));
    expect(await pixelAt(tester, boundary, const Alignment(-0.06, 0)), kSvgRed);
    expect(await pixelAt(tester, boundary, const Alignment(0.06, 0)),
        const Color(0xff0000ff));
    // 100 logical pixels at the test device pixel ratio of 3, rounded up
    // to the next bitmap step.
    final box = tester.widget<SvgImageBox>(_picture);
    expect(box.raster.image.width, 384);
  });

  testWidgets('the drawing is painted as a bitmap, never replayed',
      (tester) async {
    await _show(tester, kValidSvg);

    final image =
        find.descendant(of: _picture, matching: find.byType(RawImage));
    expect(tester.widget<RawImage>(image).image, isNotNull);
    expect(find.descendant(of: _picture, matching: find.byType(CustomPaint)),
        findsNothing);
  });
}

void _sizingTests() {
  testWidgets('a huge declared size paints without rasterising it',
      (tester) async {
    await _show(tester, svgDocument(size: 'width="20000" height="20000"'));

    expect(tester.takeException(), isNull);
    expect(tester.getSize(_picture), const Size(100, 100));
    expect(await pixelAt(tester, find.byKey(_boundary)), kSvgRed);
  });

  testWidgets('BoxFit.contain letterboxes a wide drawing', (tester) async {
    await _show(tester, svgDocument(size: 'width="20" height="10"'),
        fit: BoxFit.contain);
    final boundary = find.byKey(_boundary);

    expect(await pixelAt(tester, boundary), kSvgRed);
    expect(
        await pixelAt(tester, boundary, Alignment.topCenter), isNot(kSvgRed));
  });

  testWidgets('BoxFit.cover fills the box and clips the overflow',
      (tester) async {
    await _show(tester, svgDocument(size: 'width="20" height="10"'),
        fit: BoxFit.cover);
    final boundary = find.byKey(_boundary);

    expect(await pixelAt(tester, boundary, Alignment.topCenter), kSvgRed);
    expect(await pixelAt(tester, boundary, Alignment.bottomCenter), kSvgRed);
    expect(tester.getSize(_picture), const Size(100, 100));
  });
}

/// Documents that must end in the error widget, by description.
final _rejectedDocuments = <String, String>{
  'malformed XML': kMalformedSvg,
  'a response that is not SVG': '{"error":"not found"}',
  'zero dimensions': svgDocument(size: 'width="0" height="0"'),
  'negative dimensions': svgDocument(size: 'width="-5" height="-5"'),
  'no dimensions at all': svgDocument(size: ''),
  'an empty drawing': svgDocument(body: ''),
  'only an external image': svgDocument(
      body: '<image width="10" height="10" href="http://x.example/a.png"/>'),
  'a size too small to lay out':
      svgDocument(size: 'viewBox="0 0 1e-300 1e-300"'),
  'a pattern fill': svgDocument(
      size: 'width="100" height="100" viewBox="0 0 16000 16000"',
      body: '<defs><pattern id="p" width="16000" height="16000" '
          'patternUnits="userSpaceOnUse"><rect width="8000" height="8000" '
          'fill="#f00"/></pattern></defs>'
          '<rect width="16000" height="16000" fill="url(#p)"/>'),
  'an embedded bitmap': svgDocument(
      body: '<rect width="5" height="5"/>'
          '${embeddedImage(Uint8List.fromList([1, 2, 3]))}'),
};

void _errorTests() {
  for (final MapEntry(key: name, value: document)
      in _rejectedDocuments.entries) {
    testWidgets('$name shows the error widget', (tester) async {
      await _show(tester, document);

      expect(tester.takeException(), isNull);
      expect(_error, findsOneWidget);
      expect(_picture, findsNothing);
    });
  }

  testWidgets('a failed download shows the error widget', (tester) async {
    final fetcher = RecordingSvgFetcher(error: StateError('offline'));
    await tester.pumpWidget(_app(_container(fetcher)));
    await pumpUntilFound(tester, _error);

    expect(_error, findsOneWidget);
  });

  testWidgets('an invalid URL shows the error widget without a request',
      (tester) async {
    final fetcher = RecordingSvgFetcher();
    await tester.pumpWidget(ProviderScope(
      overrides: [svgFetcherProvider.overrideWithValue(fetcher.fetcher)],
      child: MaterialApp(
        home: NetworkSvgImage(
          imageUrl: 'http://[bad',
          placeholder: (_, __) => const Text('loading'),
          errorWidget: (_, __, ___) => const Text('image unavailable'),
        ),
      ),
    ));

    expect(_error, findsOneWidget);
    expect(fetcher.requests, isEmpty);
  });
}

void _reloadTests() {
  // Both tests keep one provider container across hiding and showing the
  // image, as happens when a grid card scrolls away and back.
  testWidgets('a failure is not kept: showing the image again retries',
      (tester) async {
    final fetcher = RecordingSvgFetcher(error: StateError('offline'));
    final container = _container(fetcher);
    await tester.pumpWidget(_app(container));
    await pumpUntilFound(tester, _error);

    await tester.pumpWidget(_app(container, show: false));
    await tester.pump();
    fetcher.error = null;
    await tester.pumpWidget(_app(container));
    await pumpUntilFound(tester, _picture);

    expect(fetcher.requests, hasLength(2));
    expect(await pixelAt(tester, find.byKey(_boundary)), kSvgRed);
  });

  testWidgets('a loaded SVG is shown again without a second download',
      (tester) async {
    final fetcher = RecordingSvgFetcher();
    final container = _container(fetcher);
    await tester.pumpWidget(_app(container));
    await pumpUntilFound(tester, _picture);

    await tester.pumpWidget(_app(container, show: false));
    await tester.pump();
    expect(_picture, findsNothing);
    await tester.pumpWidget(_app(container));
    await pumpUntilFound(tester, _picture);

    expect(fetcher.requests, hasLength(1));
    expect(await pixelAt(tester, find.byKey(_boundary)), kSvgRed);
  });
}

/// Builds [bitmapErrorOrSvg] for [error]; SVG downloads return [body].
Future<RecordingSvgFetcher> _pumpFallback(
  WidgetTester tester,
  Object error, {
  String body = '<html>not an image</html>',
}) async {
  final fetcher = RecordingSvgFetcher(body: body);
  await tester.pumpWidget(ProviderScope(
    overrides: [svgFetcherProvider.overrideWithValue(fetcher.fetcher)],
    child: MaterialApp(
      home: Builder(
        builder: (context) => bitmapErrorOrSvg(
          context,
          error: error,
          imageUrl: _url,
          placeholder: (_, __) => const Text('loading'),
          errorWidget: (_, __, e) => Text('image unavailable: $e'),
        ),
      ),
    ),
  ));
  return fetcher;
}

/// Same shape as `ClientException` of package:http, which the app cannot
/// import: an exception class of its own that is not an `IOException`.
class _ClientException implements Exception {
  _ClientException(this.message);

  final String message;

  @override
  String toString() => 'ClientException: $message';
}

void _fallbackTests() {
  test('transport errors are not decode failures', () {
    final transport = <Object>[
      const HttpException('404'),
      const SocketException('offline'),
      NetworkImageLoadException(statusCode: 404, uri: Uri.parse(_url)),
      // What a dropped connection becomes inside the image cache.
      _ClientException('Connection closed while receiving data'),
      TimeoutException('stalled'),
      StateError('Image origin does not match server'),
    ];
    for (final error in transport) {
      expect(isBitmapDecodeFailure(error), isFalse, reason: '$error');
    }
  });

  testWidgets('the error of the real image codec is a decode failure',
      (tester) async {
    // Guards the type check against a change in how the engine reports
    // undecodable data.
    final error = await tester.runAsync(() async {
      try {
        await ui.instantiateImageCodec(Uint8List.fromList(List.filled(64, 66)));
      } catch (error) {
        return error;
      }
      return null;
    });

    expect(error, isNotNull);
    expect(isBitmapDecodeFailure(error!), isTrue);
  });
}

/// A bitmap that failed to decode, as [bitmapErrorOrSvg] shows it; [show]
/// false removes it while keeping the provider container.
Widget _probeApp(ProviderContainer container, {required bool show}) =>
    UncontrolledProviderScope(
      container: container,
      child: MaterialApp(
        home: !show
            ? const SizedBox.shrink()
            : Builder(
                builder: (context) => bitmapErrorOrSvg(
                  context,
                  error: Exception('Invalid image data'),
                  imageUrl: _url,
                  placeholder: (_, __) => const Text('loading'),
                  errorWidget: (_, __, ___) => const Text('image unavailable'),
                ),
              ),
      ),
    );

void _fallbackWidgetTests() {
  testWidgets('a transport error shows the error without another request',
      (tester) async {
    final fetcher = await _pumpFallback(tester, const HttpException('404'));

    expect(find.textContaining('image unavailable'), findsOneWidget);
    expect(fetcher.requests, isEmpty);
  });

  testWidgets('a decode failure of a non-SVG file reports the bitmap error',
      (tester) async {
    await _pumpFallback(tester, Exception('Invalid image data'));
    await pumpUntilFound(tester, find.textContaining('image unavailable'));

    expect(find.textContaining('Invalid image data'), findsOneWidget);
  });

  testWidgets('a decode failure of an SVG file renders the SVG',
      (tester) async {
    await _pumpFallback(tester, Exception('Invalid image data'),
        body: kValidSvg);
    await pumpUntilFound(tester, _picture);

    expect(_picture, findsOneWidget);
    expect(find.textContaining('image unavailable'), findsNothing);
  });

  testWidgets('a file found not to be SVG is probed only once', (tester) async {
    final fetcher = RecordingSvgFetcher(body: 'BBBB corrupt bitmap');
    final container = _container(fetcher);
    // The card scrolls into view, away, and back.
    await tester.pumpWidget(_probeApp(container, show: true));
    await pumpUntilFound(tester, _error);
    await tester.pumpWidget(_probeApp(container, show: false));
    await tester.pump();
    await tester.pumpWidget(_probeApp(container, show: true));
    await pumpUntilFound(tester, _error);

    expect(fetcher.requests, hasLength(1));
  });
}
