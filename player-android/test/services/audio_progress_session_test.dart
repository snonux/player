import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/audio_progress_session.dart';

void main() {
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
