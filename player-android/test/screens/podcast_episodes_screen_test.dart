import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/screens/standalone_podcasts_screen.dart';
import 'podcast_browsing_fakes.dart';
import 'package:player_android/widgets/podcast_download_controls.dart';

void main() {
  testWidgets(
      'account switch hides retained page actions until new scoped rows load',
      (tester) async {
    final selected = StateProvider<String>((ref) => 'local');
    final repository = BrowsingRepository()..gate = Completer<void>();
    final store = BrowsingStore(repository);
    await tester.pumpWidget(browsingApp(
        const LocalPodcastEpisodesScreen(), repository,
        store: store, scope: (ref) async => ref.watch(selected)));
    await tester.pumpAndSettle();
    final container = ProviderScope.containerOf(
        tester.element(find.byType(LocalPodcastEpisodesScreen)));
    container.read(selected.notifier).state = 'account:B';
    await tester.pump();
    await tester.pump();
    expect(find.text('Mark completed'), findsNothing);
    expect(find.byType(PodcastDownloadControls), findsNothing);
    repository.gate!.complete();
    await tester.pumpAndSettle();
    final controls = tester
        .widget<PodcastDownloadControls>(find.byType(PodcastDownloadControls));
    expect(controls.scope, 'account:B');
    await tester.tap(find.text('Mark completed'));
    await tester.pumpAndSettle();
    expect(store.saves.single['scope'], 'account:B');
  });

  testWidgets(
      'return-from-playback refresh keeps current filter page and scroll',
      (tester) async {
    final repository = BrowsingRepository()..count = 43;
    await tester.pumpWidget(
        browsingApp(const LocalPodcastEpisodesScreen(), repository));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(ChoiceChip, 'All'));
    await tester.pumpAndSettle();
    await tester.dragUntilVisible(
        find.text('Next page'), find.byType(ListView), const Offset(0, -500));
    await tester.tap(find.text('Next page'));
    await tester.pumpAndSettle();
    final list = tester.widget<ListView>(find.byType(ListView));
    list.controller!.jumpTo(80);
    await tester.pump();
    final before = list.controller!.offset;
    final control = tester.widget<PodcastDownloadControls>(
        find.byType(PodcastDownloadControls).first);
    control.onPlaybackReturn!();
    await tester.pumpAndSettle();
    expect(repository.queries.last, (null, 'all', 40));
    expect(list.controller!.offset, before);
    expect(
        tester
            .widget<ChoiceChip>(find.widgetWithText(ChoiceChip, 'All'))
            .selected,
        isTrue);
  });
  testWidgets(
      'manual completion filters both views and never writes a position',
      (tester) async {
    final repository = BrowsingRepository();
    final store = BrowsingStore(repository);
    await tester.pumpWidget(browsingApp(
        const StandalonePodcastsScreen(), repository,
        store: store));
    await tester.pumpAndSettle();
    await tester.tap(find.text('My podcast'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Mark completed'));
    await tester.pumpAndSettle();
    expect(find.text('Episode 0'), findsNothing);
    expect(store.saves.single, {
      'scope': 'local',
      'completed': true,
      'seconds': null,
      'source': 'manual'
    });
    await tester.pageBack();
    await tester.pumpAndSettle();
    await tester.tap(find.text('All podcast episodes'));
    await tester.pumpAndSettle();
    expect(find.text('Episode 0'), findsNothing);
    await tester.tap(find.widgetWithText(ChoiceChip, 'Completed'));
    await tester.pumpAndSettle();
    expect(find.text('Saved position: 0:00:30.000000'), findsOneWidget);
    await tester.tap(find.text('Mark uncompleted'));
    await tester.pumpAndSettle();
    expect(find.text('Episode 0'), findsNothing);
    await tester.tap(find.widgetWithText(ChoiceChip, 'Uncompleted'));
    await tester.pumpAndSettle();
    expect(find.text('Episode 0'), findsOneWidget);
    expect(store.saves.last['seconds'], isNull);
  });

  testWidgets('save error retains episode with retryable manual control',
      (tester) async {
    final repository = BrowsingRepository()..failSave = true;
    await tester.pumpWidget(
        browsingApp(const LocalPodcastEpisodesScreen(), repository));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Mark completed'));
    await tester.pumpAndSettle();
    expect(find.text('Could not save completion. Retry.'), findsOneWidget);
    expect(find.text('Episode 0'), findsOneWidget);
    repository.failSave = false;
    await tester.tap(find.text('Mark completed'));
    await tester.pumpAndSettle();
    expect(find.text('Episode 0'), findsNothing);
  });

  testWidgets('pages and filter reset use bounded requests', (tester) async {
    final repository = BrowsingRepository()..count = 43;
    await tester.pumpWidget(
        browsingApp(const LocalPodcastEpisodesScreen(), repository));
    await tester.pumpAndSettle();
    await tester.dragUntilVisible(
        find.text('Next page'), find.byType(ListView), const Offset(0, -500));
    await tester.tap(find.text('Next page'));
    await tester.pumpAndSettle();
    expect(repository.queries.last.$3, 40);
    expect(find.text('Episode 40'), findsOneWidget);
    await tester.tap(find.widgetWithText(ChoiceChip, 'All'));
    await tester.pumpAndSettle();
    expect(repository.queries.last, (null, 'all', 0));
    expect(find.text('Episode 0'), findsOneWidget);
  });
}
