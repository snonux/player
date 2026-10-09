// Tests for IsolateSvgCompiler (svg_compiler.dart) with real isolates.

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/svg_compiler.dart';
import 'package:player_android/services/svg_document.dart';

import '../support/svg_test_support.dart';

Matcher _rejects(String message) => throwsA(
    isA<SvgException>().having((e) => e.message, 'message', contains(message)));

/// Work that never finishes. [bytes] is the path of a file it keeps
/// appending to, so the test can see from outside whether it still runs.
Uint8List _spinForever(Uint8List bytes) {
  final heartbeat = File(utf8.decode(bytes));
  while (true) {
    heartbeat.writeAsStringSync('.', mode: FileMode.append, flush: true);
  }
}

void main() {
  group('compilation', _compileTests);
  group('deadline', _deadlineTests);
  group('queue', _queueTests);
}

void _compileTests() {
  test('compiles a valid SVG in a background isolate', () async {
    expect(await IsolateSvgCompiler().call(svgBytes(kValidSvg)), isNotEmpty);
  });

  test('reports the reason a document was rejected', () async {
    final compiler = IsolateSvgCompiler();
    await expectLater(
        compiler(svgBytes(kMalformedSvg)), _rejects('Invalid SVG'));
    await expectLater(
        compiler(svgBytes(svgDocument(body: ''))), _rejects('nothing to draw'));
  });
}

void _deadlineTests() {
  test('work that exceeds the deadline is rejected and its isolate killed',
      () async {
    final directory = Directory.systemTemp.createTempSync('svg_compiler');
    addTearDown(() => directory.deleteSync(recursive: true));
    final heartbeat = File('${directory.path}/heartbeat')..createSync();
    final compiler = IsolateSvgCompiler(
      deadline: const Duration(milliseconds: 500),
      work: _spinForever,
    );

    await expectLater(compiler(Uint8List.fromList(utf8.encode(heartbeat.path))),
        _rejects('too complex'));

    // The work was really running, and it no longer is: without the kill
    // the file would keep growing.
    Future<int> sizeAfterPause() async {
      await Future<void>.delayed(const Duration(milliseconds: 300));
      return heartbeat.lengthSync();
    }

    final first = await sizeAfterPause();
    expect(first, greaterThan(0));
    expect(await sizeAfterPause(), first);
  });
}

void _queueTests() {
  test('runs no more compilations at once than allowed', () async {
    final compiler = IsolateSvgCompiler(maxConcurrent: 1);
    var firstDone = false;
    var secondStartedAfterFirst = false;
    final first = compiler(svgBytes(kValidSvg)).then((_) => firstDone = true);
    final second = compiler(svgBytes(kValidSvg), isCancelled: () {
      // Asked when the second call gets its turn.
      secondStartedAfterFirst = firstDone;
      return false;
    });
    await Future.wait<Object>([first, second]);
    expect(secondStartedAfterFirst, isTrue);
  });

  test('skips a request cancelled while it waited for its turn', () async {
    final compiler = IsolateSvgCompiler(maxConcurrent: 1);
    final first = compiler(svgBytes(kValidSvg));
    final second = compiler(svgBytes(kValidSvg), isCancelled: () => true);
    await expectLater(second, _rejects('cancelled'));
    expect(await first, isNotEmpty);
  });

  test('a timed-out call frees its slot for the next one', () async {
    final directory = Directory.systemTemp.createTempSync('svg_compiler');
    addTearDown(() => directory.deleteSync(recursive: true));
    final path = utf8.encode('${directory.path}/heartbeat');
    final compiler = IsolateSvgCompiler(
      maxConcurrent: 1,
      deadline: const Duration(milliseconds: 200),
      work: _spinForever,
    );
    await expectLater(
        compiler(Uint8List.fromList(path)), _rejects('too complex'));
    // Would wait forever if the first call still held the only slot.
    await expectLater(
        compiler(Uint8List.fromList(path)), _rejects('too complex'));
  });
}
