// Shared helpers for tests that render SVG images.

import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/widgets/network_svg_image.dart';

/// A minimal valid SVG document.
const kValidSvg = '<svg xmlns="http://www.w3.org/2000/svg" width="10" '
    'height="10"><rect width="10" height="10" fill="#f00"/></svg>';

/// An SVG document cut off in the middle of a tag.
const kMalformedSvg = '<svg xmlns="http://www.w3.org/2000/svg" width="10" '
    'height="10"><rect width="10"';

/// Records every request and answers each with [body], or throws [error].
class RecordingSvgFetcher {
  RecordingSvgFetcher({this.body = kValidSvg, this.error});

  final String body;
  final Object? error;
  final List<(Uri, Map<String, String>)> requests = [];

  Future<Uint8List> call(Uri uri, Map<String, String> headers) async {
    requests.add((uri, headers));
    if (error != null) throw error!;
    return Uint8List.fromList(utf8.encode(body));
  }

  SvgFetcher get fetcher => call;
}

/// Pumps until [finder] matches or about two seconds of real time passed.
///
/// SVG parsing and bitmap decoding finish in real time (a background isolate
/// and the engine codec), which the fake clock of `pump` never advances, so
/// each round waits in `runAsync` before pumping the next frame.
Future<void> pumpUntilFound(WidgetTester tester, Finder finder) async {
  for (var i = 0; i < 80 && finder.evaluate().isEmpty; i++) {
    await tester
        .runAsync(() => Future<void>.delayed(const Duration(milliseconds: 25)));
    await tester.pump();
  }
}
