// Shared helpers for tests that render SVG images.

import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:dio/dio.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/svg_fetcher.dart';

/// Pure red, the fill of [kValidSvg].
const kSvgRed = ui.Color(0xffff0000);

/// Builds an SVG document; the default is a 10x10 drawing filled with red.
String svgDocument({
  String size = 'width="10" height="10"',
  String body = '<rect width="100%" height="100%" fill="#ff0000"/>',
}) =>
    '<svg xmlns="http://www.w3.org/2000/svg" $size>$body</svg>';

/// A minimal valid SVG document: a red 10x10 square.
final kValidSvg = svgDocument();

/// An SVG document cut off in the middle of a tag.
const kMalformedSvg = '<svg xmlns="http://www.w3.org/2000/svg" width="10" '
    'height="10"><rect width="10"';

Uint8List svgBytes(String document) =>
    Uint8List.fromList(utf8.encode(document));

/// One call made to a [RecordingSvgFetcher].
typedef SvgFetchCall = ({
  Uri uri,
  Map<String, String> headers,
  CancelToken cancel
});

/// Records every request and answers each with [body], or throws [error].
/// Both can be changed between requests. While [gate] is set, requests wait
/// for it, which lets a test act on a download that is still running.
class RecordingSvgFetcher {
  RecordingSvgFetcher({String? body, this.error}) : body = body ?? kValidSvg;

  String body;
  Object? error;
  Completer<void>? gate;
  final List<SvgFetchCall> requests = [];

  Future<Uint8List> call(
    Uri uri,
    Map<String, String> headers,
    CancelToken cancel,
  ) async {
    requests.add((uri: uri, headers: headers, cancel: cancel));
    await gate?.future;
    if (error != null) throw error!;
    return svgBytes(body);
  }

  SvgFetcher get fetcher => call;
}

/// Pumps frames until [finder] matches.
///
/// SVG parsing, bitmap decoding and the image disk cache finish in real time
/// (isolates, engine codecs, file I/O), which the fake clock of `pump` never
/// advances, so each round waits in `runAsync` before pumping a frame. The
/// loop ends as soon as the widget appears; the generous deadline only
/// bounds a genuine failure, so a slow machine cannot make this flaky.
Future<void> pumpUntilFound(WidgetTester tester, Finder finder) async {
  const step = Duration(milliseconds: 20);
  final deadline = DateTime.now().add(const Duration(seconds: 60));
  while (finder.evaluate().isEmpty) {
    if (DateTime.now().isAfter(deadline)) {
      fail('Timed out waiting for $finder');
    }
    await tester.runAsync(() => Future<void>.delayed(step));
    await tester.pump();
  }
}

/// Returns the colour painted at [alignment] inside the `RepaintBoundary`
/// matched by [boundary]. This looks at real pixels, so it only passes when
/// the picture was decoded and painted, not merely when a widget exists.
Future<ui.Color> pixelAt(
  WidgetTester tester,
  Finder boundary, [
  Alignment alignment = Alignment.center,
]) async {
  final render = tester.renderObject<RenderRepaintBoundary>(boundary);
  final color = await tester.runAsync(() async {
    final image = await render.toImage();
    final data = await image.toByteData(format: ui.ImageByteFormat.rawRgba);
    final at = alignment.alongSize(ui.Size(image.width - 1, image.height - 1));
    final offset = (at.dy.round() * image.width + at.dx.round()) * 4;
    image.dispose();
    return ui.Color.fromARGB(
      data!.getUint8(offset + 3),
      data.getUint8(offset),
      data.getUint8(offset + 1),
      data.getUint8(offset + 2),
    );
  });
  return color!;
}

/// Encodes a solid green PNG. Needs real async: call it inside `runAsync`
/// from widget tests.
Future<Uint8List> makePng(int width, int height) async {
  final recorder = ui.PictureRecorder();
  ui.Canvas(recorder).drawRect(
    ui.Rect.fromLTWH(0, 0, width.toDouble(), height.toDouble()),
    ui.Paint()..color = const ui.Color(0xff00ff00),
  );
  final image = await recorder.endRecording().toImage(width, height);
  final data = await image.toByteData(format: ui.ImageByteFormat.png);
  image.dispose();
  return data!.buffer.asUint8List();
}

/// An `<image>` element embedding [encoded] as a data URI.
String embeddedImage(Uint8List encoded, {String mime = 'image/png'}) =>
    '<image width="10" height="10" '
    'href="data:$mime;base64,${base64.encode(encoded)}"/>';
