import 'dart:async';
import 'dart:collection';
import 'dart:isolate';
import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'svg_document.dart';

/// Compiles downloaded SVG bytes to the vector_graphics binary format.
/// [isCancelled] is asked right before the work starts, so a request that
/// was dropped while waiting costs nothing.
typedef SvgCompiler = Future<Uint8List> Function(
  Uint8List bytes, {
  bool Function()? isCancelled,
});

/// The work done in the isolate; must be a top-level or static function.
typedef SvgCompileWork = Uint8List Function(Uint8List bytes);

/// Runs [compileSvg] in short-lived background isolates.
///
/// Parsing is too slow for the UI isolate, and the document is untrusted:
///  * At most [maxConcurrent] isolates run at once, so a grid full of SVG
///    thumbnails queues up instead of starting one isolate per card.
///  * An isolate that is not done after [deadline] is killed. No input is
///    known that takes this long within the download size cap (nested
///    `<use>` references that multiply, for one, are rejected within
///    milliseconds); the deadline is a backstop so that a parser weakness
///    nobody has found yet costs a bounded amount of CPU instead of running
///    forever.
class IsolateSvgCompiler {
  IsolateSvgCompiler({
    this.maxConcurrent = 2,
    this.deadline = const Duration(seconds: 10),
    this.work = compileSvg,
  });

  final int maxConcurrent;
  final Duration deadline;

  /// Replaceable so tests can run work that never finishes.
  final SvgCompileWork work;

  int _running = 0;
  final Queue<Completer<void>> _waiting = Queue();

  Future<Uint8List> call(
    Uint8List bytes, {
    bool Function()? isCancelled,
  }) async {
    await _acquire();
    try {
      if (isCancelled?.call() ?? false) {
        throw const SvgException('SVG request cancelled');
      }
      return await _compileInIsolate(bytes);
    } finally {
      _release();
    }
  }

  Future<void> _acquire() async {
    if (_running < maxConcurrent) {
      _running++;
      return;
    }
    final turn = Completer<void>();
    _waiting.add(turn);
    // The slot is handed over by [_release], which keeps [_running] as is.
    await turn.future;
  }

  void _release() {
    if (_waiting.isEmpty) {
      _running--;
    } else {
      _waiting.removeFirst().complete();
    }
  }

  /// The isolate answers with the compiled bytes, or with the error text.
  /// `null` arrives when it exits without an answer (killed or crashed).
  Future<Uint8List> _compileInIsolate(Uint8List bytes) async {
    final port = ReceivePort();
    Isolate? isolate;
    try {
      isolate = await Isolate.spawn(
        _isolateMain,
        (port.sendPort, work, bytes),
        onExit: port.sendPort,
        debugName: 'Compile SVG',
      );
      final answer = await port.first.timeout(deadline);
      if (answer is Uint8List) return answer;
      throw SvgException(answer is String ? answer : 'SVG compiler stopped');
    } on TimeoutException {
      throw const SvgException('SVG is too complex');
    } finally {
      // Also reached when spawning fails, so the port never leaks.
      isolate?.kill(priority: Isolate.immediate);
      port.close();
    }
  }
}

/// Isolate entry point. Errors travel back as text because arbitrary
/// exception objects may not be sendable between isolates.
void _isolateMain((SendPort, SvgCompileWork, Uint8List) message) {
  final (port, work, bytes) = message;
  Object answer;
  try {
    answer = work(bytes);
  } on SvgException catch (error) {
    answer = error.message;
  } catch (error) {
    answer = 'Invalid SVG: $error';
  }
  Isolate.exit(port, answer);
}

/// App-wide compiler, shared so the concurrency limit covers every image.
/// Tests may override it to count or stub compilations.
final svgCompilerProvider =
    Provider<SvgCompiler>((ref) => IsolateSvgCompiler().call);
