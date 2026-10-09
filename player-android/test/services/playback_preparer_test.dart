// Unit tests for PlaybackPreparer: the retry policy that waits for a server
// compatibility stream (503 + Retry-After) before a native player opens it.
//
// The probe is a plain function, so no HTTP is involved. Pauses use real
// timers with millisecond intervals; the total-wait cap is driven by an
// injected clock so the tests never sleep for long.

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/playback_preparer.dart';

final _uri = Uri.parse('https://player.test/api/v1/media/7/compat');

const _short = Duration(milliseconds: 2);
const _ready = PlaybackProbe(statusCode: 200);
const _busy = PlaybackProbe(statusCode: 503, retryAfter: _short);

/// Answers probes from [answers] in order and repeats the last one. An
/// [Object] that is not a [PlaybackProbe] is thrown.
class _Probe {
  _Probe(this.answers);

  final List<Object> answers;
  int calls = 0;
  CancelToken? lastToken;

  Future<PlaybackProbe> call(Uri uri, {CancelToken? cancelToken}) async {
    lastToken = cancelToken;
    final answer = answers[calls < answers.length ? calls : answers.length - 1];
    calls++;
    if (answer is PlaybackProbe) return answer;
    throw answer;
  }
}

Matcher _fails(PlaybackPreparationFailure failure, {int? statusCode}) =>
    throwsA(isA<PlaybackPreparationException>()
        .having((e) => e.failure, 'failure', failure)
        .having((e) => e.statusCode, 'statusCode', statusCode));

void main() {
  test('a ready stream is played after a single probe', () async {
    final probe = _Probe([_ready]);
    final preparation =
        const PlaybackPreparer().prepare(_uri, probe: probe.call);
    expect(await preparation.ready, isTrue);
    expect(probe.calls, 1);
  });

  test('only a success status is ready and only 503 is preparing', () {
    expect(const PlaybackProbe(statusCode: 200).isReady, isTrue);
    expect(const PlaybackProbe(statusCode: 200).isPreparing, isFalse);
    expect(const PlaybackProbe(statusCode: 503).isReady, isFalse);
    expect(const PlaybackProbe(statusCode: 503).isPreparing, isTrue);
    expect(const PlaybackProbe(statusCode: 500).isReady, isFalse);
    expect(const PlaybackProbe(statusCode: 500).isPreparing, isFalse);
  });

  test('503 answers are retried until the stream is ready', () async {
    final probe = _Probe([_busy, _busy, _ready]);
    final preparation =
        const PlaybackPreparer().prepare(_uri, probe: probe.call);
    expect(await preparation.ready, isTrue);
    expect(probe.calls, 3);
  });

  test('503 without Retry-After waits for the default interval', () async {
    final probe = _Probe([const PlaybackProbe(statusCode: 503), _ready]);
    final watch = Stopwatch()..start();
    final preparation = const PlaybackPreparer(
      defaultRetryInterval: Duration(milliseconds: 40),
    ).prepare(_uri, probe: probe.call);
    expect(await preparation.ready, isTrue);
    expect(watch.elapsedMilliseconds, greaterThanOrEqualTo(35));
  });

  test('an excessive Retry-After is capped', () async {
    final probe = _Probe([
      const PlaybackProbe(statusCode: 503, retryAfter: Duration(hours: 1)),
      _ready,
    ]);
    final preparation = const PlaybackPreparer(maxRetryInterval: _short)
        .prepare(_uri, probe: probe.call);
    expect(await preparation.ready.timeout(const Duration(seconds: 5)), isTrue);
    expect(probe.calls, 2);
  });

  test('503 until the cap fails with timedOut instead of retrying forever',
      () async {
    final probe = _Probe([_busy]);
    var now = DateTime(2026);
    final preparer = PlaybackPreparer(
      maxWait: const Duration(seconds: 30),
      // Every look at the clock costs ten seconds: start, then each check.
      now: () {
        final current = now;
        now = now.add(const Duration(seconds: 10));
        return current;
      },
    );
    final preparation = preparer.prepare(_uri, probe: probe.call);
    await expectLater(
      preparation.ready,
      _fails(PlaybackPreparationFailure.timedOut),
    );
    // Checked at +10 s, +20 s (retried) and +30 s (pause would pass the cap).
    expect(probe.calls, 3);
  });

  for (final (status, failure) in [
    (500, PlaybackPreparationFailure.conversionFailed),
    (507, PlaybackPreparationFailure.outOfStorage),
    (400, PlaybackPreparationFailure.rejected),
    (403, PlaybackPreparationFailure.rejected),
    (415, PlaybackPreparationFailure.rejected),
    (404, PlaybackPreparationFailure.rejected),
    (410, PlaybackPreparationFailure.rejected),
  ]) {
    test('$status is terminal and is not retried', () async {
      final probe = _Probe([PlaybackProbe(statusCode: status), _ready]);
      final preparation =
          const PlaybackPreparer().prepare(_uri, probe: probe.call);
      await expectLater(
        preparation.ready,
        _fails(failure, statusCode: status),
      );
      expect(probe.calls, 1);
    });
  }

  test('a failed request (timeout, no connection) is terminal', () async {
    final probe = _Probe([
      DioException(
        requestOptions: RequestOptions(),
        type: DioExceptionType.receiveTimeout,
      ),
    ]);
    final preparation =
        const PlaybackPreparer().prepare(_uri, probe: probe.call);
    await expectLater(
      preparation.ready,
      _fails(PlaybackPreparationFailure.unreachable),
    );
    expect(probe.calls, 1);
  });

  test('cancel during a pause ends the wait without another probe', () async {
    final probe = _Probe([
      const PlaybackProbe(statusCode: 503, retryAfter: Duration(minutes: 5)),
    ]);
    final preparation = const PlaybackPreparer(
      maxRetryInterval: Duration(minutes: 5),
    ).prepare(_uri, probe: probe.call);
    await Future<void>.delayed(const Duration(milliseconds: 10));
    preparation.cancel();
    expect(await preparation.ready, isFalse);
    expect(preparation.isCancelled, isTrue);
    expect(probe.calls, 1);
    expect(probe.lastToken!.isCancelled, isTrue);
  });

  test('cancel while a probe is pending reports cancelled, not an error',
      () async {
    final pending = Completer<PlaybackProbe>();
    CancelToken? token;
    final preparation = const PlaybackPreparer().prepare(
      _uri,
      probe: (uri, {cancelToken}) {
        token = cancelToken;
        return pending.future;
      },
    );
    await Future<void>.delayed(Duration.zero);
    preparation.cancel();
    // Dio fails a cancelled request; the preparer must not surface that.
    pending.completeError(DioException(
      requestOptions: RequestOptions(),
      type: DioExceptionType.cancel,
    ));
    expect(await preparation.ready, isFalse);
    expect(token!.isCancelled, isTrue);
  });

  test('a replaced owner stops the wait at the next probe', () async {
    final probe = _Probe([_busy]);
    var wanted = true;
    final preparation = const PlaybackPreparer().prepare(
      _uri,
      probe: probe.call,
      stillWanted: () => wanted,
    );
    await Future<void>.delayed(const Duration(milliseconds: 10));
    wanted = false;
    expect(await preparation.ready, isFalse);
    final callsWhenStopped = probe.calls;
    await Future<void>.delayed(const Duration(milliseconds: 20));
    expect(probe.calls, callsWhenStopped);
  });
}
