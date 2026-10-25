import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/audio_progress_session.dart';

void main() {
  testWidgets('podcast near-end ticks and close only save position',
      (tester) async {
    final positions = <double>[];
    final session = AudioProgressSession(
      position: () => const Duration(seconds: 99),
      duration: () => const Duration(seconds: 100),
      playing: () => true,
      naturalEndOnly: true,
      savePosition: (p) async => positions.add(p),
      markFinished: () async => fail('near-end is not completion'),
      finishAtPosition: (_) async => fail('close is not completion'),
    );
    await tester.pump(const Duration(seconds: 5));
    await session.close(finalPosition: const Duration(seconds: 99));
    expect(positions, [99]);
  });

  testWidgets(
      'podcast natural end is one atomic callback using native duration',
      (tester) async {
    final ended = <double>[];
    final session = AudioProgressSession(
      position: () => const Duration(seconds: 98),
      duration: () => const Duration(seconds: 100),
      playing: () => true,
      naturalEndOnly: true,
      savePosition: (_) async => fail('end must not save position separately'),
      markFinished: () async => fail('end must be paired'),
      finishAtPosition: (p) async => ended.add(p),
    );
    await session.record(force: true, completed: true);
    await session.close();
    expect(ended, [100]);
  });
  testWidgets('unknown-duration completion retries a failed finish write',
      (tester) async {
    var attempts = 0;
    final session = AudioProgressSession(
      position: () => const Duration(seconds: 8),
      duration: () => null,
      playing: () => false,
      savePosition: (_) async {},
      markFinished: () async {
        if (++attempts == 1) throw StateError('database busy');
      },
    );
    await session.record(force: true, completed: true);
    expect(attempts, 1);
    await session.close();
    expect(attempts, 2);
  });

  for (final duration in <Duration?>[null, Duration.zero]) {
    testWidgets('unknown duration $duration waits for actual completion',
        (tester) async {
      final writes = <String>[];
      final session = AudioProgressSession(
        position: () => const Duration(seconds: 8),
        duration: () => duration,
        playing: () => true,
        savePosition: (seconds) async => writes.add('position:$seconds'),
        markFinished: () async => writes.add('finished'),
      );
      await session.record();
      expect(writes, ['position:8.0']);
      await session.record(force: true, completed: true);
      await session.record(force: true, completed: true);
      await session.close();
      expect(writes, ['position:8.0', 'finished']);
    });
  }

  testWidgets('stop saves final position after a partial tick', (tester) async {
    var position = const Duration(milliseconds: 1200);
    final saved = <double>[];
    final session = AudioProgressSession(
      position: () => position,
      duration: () => const Duration(seconds: 100),
      playing: () => true,
      savePosition: (seconds) async => saved.add(seconds),
      markFinished: () async {},
    );
    await tester.pump(const Duration(seconds: 5));
    position = const Duration(milliseconds: 2400);
    await session.close();
    expect(saved, [1.2, 2.4]);
  });

  testWidgets('failed position and finish calls retry on the same position',
      (tester) async {
    var saveCalls = 0;
    var finishCalls = 0;
    final session = AudioProgressSession(
      position: () => const Duration(seconds: 96),
      duration: () => const Duration(seconds: 100),
      playing: () => true,
      savePosition: (_) async {
        saveCalls++;
        if (saveCalls == 1) throw StateError('offline');
      },
      markFinished: () async {
        finishCalls++;
        if (finishCalls == 1) throw StateError('offline');
      },
    );
    await session.record();
    expect((saveCalls, finishCalls), (1, 1));
    await session.close();
    expect((saveCalls, finishCalls), (2, 2));
  });

  testWidgets('successful completion is the final queued update',
      (tester) async {
    var position = const Duration(seconds: 96);
    final writes = <String>[];
    final session = AudioProgressSession(
      position: () => position,
      duration: () => const Duration(seconds: 100),
      playing: () => true,
      savePosition: (seconds) async => writes.add('position:$seconds'),
      markFinished: () async => writes.add('finished'),
    );
    await session.record();
    position = const Duration(seconds: 99);
    await tester.pump(const Duration(seconds: 5));
    await session.close();
    expect(writes, ['position:96.0', 'finished']);
  });
}
