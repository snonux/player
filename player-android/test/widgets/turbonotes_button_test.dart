import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/turbonotes_link.dart';
import 'package:player_android/widgets/overflow_menu.dart';
import 'package:player_android/widgets/turbonotes_button.dart';

class _Launcher extends TurboNotesLauncher {
  final opened = <NoteSubject>[];
  bool installed = true;
  @override
  Future<void> open(NoteSubject subject) async {
    if (!installed) throw const TurboNotesMissingException();
    opened.add(subject);
  }
}

final _request = LocalPlaybackRequest(
    localMediaId: 1, sourceUri: Uri.parse('content://x'), title: 'Dune.m4b');

void main() {
  late _Launcher launcher;
  setUp(() => launcher = _Launcher());

  Future<void> pump(WidgetTester tester, NoteSubject? subject) async {
    await tester.pumpWidget(ProviderScope(
      overrides: [
        turboNotesLauncherProvider.overrideWithValue(launcher),
        noteSubjectResolverProvider
            .overrideWithValue((request, client) async => subject),
      ],
      child: MaterialApp(
          home: Scaffold(
              appBar: AppBar(actions: [TurboNotesButton(request: _request)]))),
    ));
    await tester.pumpAndSettle();
  }

  testWidgets('opens the note for an audiobook', (tester) async {
    await pump(tester, const NoteSubject(title: 'Dune', podcast: false));
    expect(find.byTooltip('Book note in TurboNotes'), findsOneWidget);
    await tester.tap(find.byKey(const Key('turbonotes_button')));
    await tester.pumpAndSettle();
    expect(launcher.opened.single.notePath, 'Book-And-Podcast-Notes/Dune.md');
  });

  testWidgets('podcast episodes get an episode note', (tester) async {
    await pump(tester, const NoteSubject(title: 'Ep 1', podcast: true));
    expect(find.byTooltip('Episode note in TurboNotes'), findsOneWidget);
  });

  testWidgets('no button for music', (tester) async {
    await pump(tester, null);
    expect(find.byKey(const Key('turbonotes_button')), findsNothing);
  });

  testWidgets('says so when TurboNotes is not installed', (tester) async {
    launcher.installed = false;
    await pump(tester, const NoteSubject(title: 'Dune', podcast: false));
    await tester.tap(find.byKey(const Key('turbonotes_button')));
    await tester.pumpAndSettle();
    expect(find.textContaining('Install TurboNotes'), findsOneWidget);
  });

  testWidgets('as a menu row it closes the menu, then opens the note',
      (tester) async {
    for (final installed in [true, false]) {
      launcher.installed = installed;
      await tester.pumpWidget(ProviderScope(
        overrides: [
          turboNotesLauncherProvider.overrideWithValue(launcher),
          noteSubjectResolverProvider.overrideWithValue(
              (request, client) async =>
                  const NoteSubject(title: 'Dune', podcast: false)),
        ],
        child: MaterialApp(
            home: Scaffold(
                appBar: AppBar(actions: [
          OverflowMenuButton(
              items: (_) =>
                  [TurboNotesButton(request: _request, asMenuItem: true)]),
        ]))),
      ));
      await tester.tap(find.byTooltip('More options'));
      await tester.pumpAndSettle();
      expect(find.text('Book note in TurboNotes'), findsOneWidget);
      await tester.tap(find.byKey(const Key('turbonotes_button')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('overflow_menu_sheet')), findsNothing);
      if (installed) {
        expect(
            launcher.opened.single.notePath, 'Book-And-Podcast-Notes/Dune.md');
      } else {
        expect(find.textContaining('Install TurboNotes'), findsOneWidget);
      }
    }
  });
}
