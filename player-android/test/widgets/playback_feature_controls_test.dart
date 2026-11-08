// Widget-level journeys for player controls that had no direct test: the
// sleep timer status and chooser, the queue's autoplay opt-in and the music
// shuffle options dialog.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/music_shuffle.dart';
import 'package:player_android/services/sleep_timer.dart';
import 'package:player_android/widgets/music_autoplay_control.dart';
import 'package:player_android/widgets/music_shuffle_options_dialog.dart';
import 'package:player_android/widgets/sleep_timer_control.dart';

Widget _host(Widget child) =>
    MaterialApp(home: Scaffold(body: SingleChildScrollView(child: child)));

void main() {
  group('sleep timer', () {
    late Duration now;
    late int expiries;
    late SleepTimer timer;

    setUp(() {
      now = Duration.zero;
      expiries = 0;
      timer = SleepTimer(onExpire: () async => expiries++, now: () => now);
    });
    tearDown(() => timer.dispose());

    testWidgets('stays hidden until a timer runs', (tester) async {
      await tester.pumpWidget(_host(SleepTimerControl(timer: timer)));
      expect(find.byType(TextButton), findsNothing);
    });

    testWidgets(
        'chooser starts a timer, status counts down and cancel hides it',
        (tester) async {
      await tester.pumpWidget(_host(Builder(
          builder: (context) => Column(children: [
                // The player menu row calls the same chooser after its menu closes.
                TextButton(
                    onPressed: () => chooseSleepTimer(context, timer),
                    child: const Text('Choose sleep timer')),
                SleepTimerControl(timer: timer),
              ]))));
      await tester.tap(find.text('Choose sleep timer'));
      await tester.pumpAndSettle();
      expect(find.text('Pause playback after a duration.'), findsOneWidget);
      for (final minutes in [1, 5, 15, 30, 45, 60, 90]) {
        expect(find.text('$minutes minutes'), findsOneWidget);
      }
      await tester.tap(find.text('15 minutes'));
      await tester.pumpAndSettle();
      expect(timer.remaining, const Duration(minutes: 15));
      expect(find.text('Sleep 15:00 remaining'), findsOneWidget);

      now = const Duration(minutes: 2, seconds: 30);
      timer.check();
      await tester.pump();
      expect(find.text('Sleep 12:30 remaining'), findsOneWidget);

      await tester.tap(find.text('Cancel timer'));
      await tester.pump();
      expect(find.textContaining('Sleep '), findsNothing);
      expect(timer.remaining, isNull);
      expect(expiries, 0);
    });

    testWidgets('expiry pauses once and shows the ended state', (tester) async {
      await tester.pumpWidget(_host(SleepTimerControl(timer: timer)));
      timer.start(const Duration(minutes: 1));
      await tester.pump();
      expect(find.text('Sleep 1:00 remaining'), findsOneWidget);
      now = const Duration(minutes: 1, seconds: 1);
      timer.check();
      await tester.pump();
      expect(expiries, 1);
      expect(find.text('Sleep timer ended · paused'), findsOneWidget);
      expect(find.text('Cancel timer'), findsNothing);
      timer.check();
      expect(expiries, 1);
      // A new explicit playback intent clears the ended banner.
      timer.beginPlaybackIntent();
      await tester.pump();
      expect(find.text('Sleep timer ended · paused'), findsNothing);
    });

    testWidgets('a failed pause is reported instead of the ended state',
        (tester) async {
      final failing = SleepTimer(
          onExpire: () async => throw StateError('cast lost'), now: () => now);
      addTearDown(failing.dispose);
      await tester.pumpWidget(_host(SleepTimerControl(timer: failing)));
      failing.start(const Duration(minutes: 1));
      now = const Duration(minutes: 2);
      failing.check();
      await tester.pump();
      await tester.pump();
      expect(
          find.text(
              'Could not pause playback. Check the player or cast device.'),
          findsOneWidget);
    });
  });

  group('autoplay related music', () {
    testWidgets('toggles while online and reports its status', (tester) async {
      final changes = <bool>[];
      await tester.pumpWidget(_host(MusicAutoplayControl(
          enabled: false,
          onChanged: changes.add,
          status: 'Finding related songs…')));
      expect(find.text('Autoplay related music'), findsOneWidget);
      expect(
          find.text('After this queue ends · related songs from your library'),
          findsOneWidget);
      expect(find.text('Finding related songs…'), findsOneWidget);
      await tester.tap(find.byType(Switch));
      expect(changes, [true]);
    });

    testWidgets('offline cannot turn it on but can still turn it off',
        (tester) async {
      final changes = <bool>[];
      await tester.pumpWidget(_host(MusicAutoplayControl(
          enabled: false, available: false, onChanged: changes.add)));
      expect(find.text('Autoplay needs a connection.'), findsOneWidget);
      await tester.tap(find.byType(Switch));
      expect(changes, isEmpty);

      await tester.pumpWidget(_host(MusicAutoplayControl(
          enabled: true, available: false, onChanged: changes.add)));
      await tester.tap(find.byType(Switch));
      expect(changes, [false]);
    });
  });

  group('shuffle options', () {
    Future<void> open(
        WidgetTester tester,
        Future<bool> Function(
                MusicShuffleOptions, bool Function(), void Function(String))
            run) async {
      await tester.pumpWidget(MaterialApp(
          home: Scaffold(
              body: Builder(
                  builder: (context) => TextButton(
                      onPressed: () => showDialog<void>(
                          context: context,
                          builder: (_) => MusicShuffleOptionsDialog(run: run)),
                      child: const Text('open'))))));
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();
    }

    Finder field(String label) =>
        find.widgetWithText(TextField, '$label (optional)');

    testWidgets('invalid years stay in the dialog with a clear message',
        (tester) async {
      var runs = 0;
      await open(tester, (_, __, ___) async {
        runs++;
        return true;
      });
      await tester.enterText(field('From year'), '2020');
      await tester.enterText(field('To year'), '1999');
      await tester.tap(find.text('Shuffle'));
      await tester.pump();
      expect(find.text('From year must not be after To year.'), findsOneWidget);
      await tester.enterText(field('To year'), 'abc');
      await tester.tap(find.text('Shuffle'));
      await tester.pump();
      expect(find.text('To year must be a whole number from 1 to 9999.'),
          findsOneWidget);
      expect(runs, 0);
      expect(find.byType(MusicShuffleOptionsDialog), findsOneWidget);
    });

    testWidgets('valid options run once and close the dialog on success',
        (tester) async {
      final received = <MusicShuffleOptions>[];
      await open(tester, (options, current, report) async {
        received.add(options);
        expect(current(), isTrue);
        return true;
      });
      expect(
          find.text(
              'Blank includes every song. Applies to this collection only.'),
          findsOneWidget);
      await tester.enterText(field('Genre'), '  Jazz ');
      await tester.enterText(field('From year'), '1990');
      await tester.enterText(field('To year'), '1999');
      await tester.enterText(field('Maximum songs'), '25');
      await tester.tap(find.text('Shuffle'));
      await tester.pumpAndSettle();
      expect(received, hasLength(1));
      expect(received.single.genre, 'jazz');
      expect(received.single.from, 1990);
      expect(received.single.to, 1999);
      expect(received.single.maximum, 25);
      expect(find.byType(MusicShuffleOptionsDialog), findsNothing);
    });

    testWidgets('a failed run keeps the dialog open with its message',
        (tester) async {
      await open(tester, (options, current, report) async {
        report('No songs match these options.');
        return false;
      });
      await tester.tap(find.text('Shuffle'));
      await tester.pumpAndSettle();
      expect(find.text('No songs match these options.'), findsOneWidget);
      expect(find.byType(MusicShuffleOptionsDialog), findsOneWidget);
      // The button is usable again for a retry; Cancel closes the dialog.
      expect(
          tester
              .widget<FilledButton>(
                  find.widgetWithText(FilledButton, 'Shuffle'))
              .onPressed,
          isNotNull);
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      expect(find.byType(MusicShuffleOptionsDialog), findsNothing);
    });
  });
}
