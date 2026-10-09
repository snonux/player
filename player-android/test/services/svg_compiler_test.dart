// Tests for IsolateSvgCompiler (svg_compiler.dart) with real isolates.

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/svg_compiler.dart';
import 'package:player_android/services/svg_document.dart';

import '../support/svg_test_support.dart';

Matcher _rejects(String message) => throwsA(
    isA<SvgException>().having((e) => e.message, 'message', contains(message)));

void main() {
  test('compiles a valid SVG in a background isolate', () async {
    final compiled = await IsolateSvgCompiler().call(svgBytes(kValidSvg));
    expect(compiled.data, isNotEmpty);
  });

  test('reports the reason a document was rejected', () async {
    final compiler = IsolateSvgCompiler();
    await expectLater(
        compiler(svgBytes(kMalformedSvg)), _rejects('Invalid SVG'));
    await expectLater(
        compiler(svgBytes(svgDocument(body: ''))), _rejects('nothing to draw'));
  });

  test('kills a compilation that exceeds the deadline', () async {
    // Starting an isolate alone takes longer than a microsecond.
    final compiler =
        IsolateSvgCompiler(deadline: const Duration(microseconds: 1));
    await expectLater(compiler(svgBytes(kValidSvg)), _rejects('too complex'));
    // The slot was released: a later call is not stuck in the queue.
    await expectLater(compiler(svgBytes(kValidSvg)), _rejects('too complex'));
  });

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
    expect((await first).data, isNotEmpty);
  });
}
