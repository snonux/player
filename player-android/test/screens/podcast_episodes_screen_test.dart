import 'package:player_android/services/podcast_server_metadata.dart';
import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/screens/standalone_podcasts_screen.dart';
import 'podcast_browsing_fakes.dart';
import 'package:player_android/widgets/podcast_download_controls.dart';

Future<void> openEpisodeActions(WidgetTester tester) async {
  await tester.tap(find.byTooltip('More episode actions').first);
  await tester.pumpAndSettle();
}

class FakeServerMetadata extends PodcastServerMetadata {
  FakeServerMetadata(BrowsingRepository repository, String scope)
      : super(
            repository: repository,
            scope: scope,
            current: () => true,
            fetchPage: (_, __, ___) async => []);
  final feeds = <String>[];
  @override
  Future<void> refresh(String feed, {void Function()? onPage}) async {
    feeds.add(feed);
    onPage?.call();
  }
}

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
    await openEpisodeActions(tester);
    await tester.tap(find.text('Mark completed'));
    await tester.pumpAndSettle();
    expect(store.saves.single['scope'], 'account:B');
  });

  testWidgets(
      'return-from-playback refresh keeps current filter page and scroll',
      (tester) async {
    final repository = BrowsingRepository()..count = 80;
    await tester.pumpWidget(
        browsingApp(const LocalPodcastEpisodesScreen(), repository));
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
    expect(find.byType(ChoiceChip), findsNothing);
  });
  testWidgets(
      'per-feed filters remain while all episodes always include listened',
      (tester) async {
    final repository = BrowsingRepository();
    final store = BrowsingStore(repository);
    await tester.pumpWidget(browsingApp(
        const StandalonePodcastsScreen(), repository,
        store: store));
    await tester.pumpAndSettle();
    await tester.tap(find.text('My podcast'));
    await tester.pumpAndSettle();
    await openEpisodeActions(tester);
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
    expect(find.text('Episode 0'), findsOneWidget);
    expect(find.text('Listened'), findsOneWidget);
    expect(find.byType(ChoiceChip), findsNothing);
    expect(
        find.text('Newest first · Includes listened episodes'), findsOneWidget);
    final artwork = tester.widget<Opacity>(find.byType(Opacity).first);
    expect(artwork.opacity, 0.5);
    await openEpisodeActions(tester);
    await tester.tap(find.text('Mark uncompleted'));
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
    await openEpisodeActions(tester);
    await tester.tap(find.text('Mark completed'));
    await tester.pumpAndSettle();
    expect(find.text('Could not save completion. Retry.'), findsOneWidget);
    expect(find.text('Episode 0'), findsOneWidget);
    repository.failSave = false;
    await openEpisodeActions(tester);
    await tester.tap(find.text('Mark completed'));
    await tester.pumpAndSettle();
    expect(find.text('Episode 0'), findsOneWidget);
  });

  testWidgets('pages and filter reset use bounded requests', (tester) async {
    final repository = BrowsingRepository()..count = 43;
    await tester.pumpWidget(browsingApp(
        const LocalPodcastEpisodesScreen(feedKey: 'feed:example/feed'),
        repository));
    await tester.pumpAndSettle();
    await tester.dragUntilVisible(
        find.text('Next page'), find.byType(ListView), const Offset(0, -500));
    await tester.tap(find.text('Next page'));
    await tester.pumpAndSettle();
    expect(repository.queries.last.$3, 40);
    expect(find.text('Episode 40'), findsOneWidget);
    await tester.tap(find.widgetWithText(ChoiceChip, 'All'));
    await tester.pumpAndSettle();
    expect(repository.queries.last, ('feed:example/feed', 'all', 0));
    expect(find.text('Episode 0'), findsOneWidget);
  });
  testWidgets(
      'all episodes refreshes missing subscriptions without hiding cached rows and offers retry',
      (tester) async {
    final repository = BrowsingRepository()
      ..missingFeeds = ['one', 'two', 'three']
      ..failedFeeds.add('two');
    await tester.pumpWidget(
        browsingApp(const LocalPodcastEpisodesScreen(), repository));
    await tester.pumpAndSettle();
    expect(repository.refreshed, ['one', 'two', 'three']);
    expect(find.text('Episode 0'), findsOneWidget);
    expect(find.textContaining('1 feeds unavailable'), findsOneWidget);
    await tester.tap(find.byTooltip('Reload episodes'));
    await tester.pumpAndSettle();
    expect(repository.refreshed.last, 'feed:example/feed');
  });

  testWidgets('episode deletion requires confirmation and removes the row',
      (tester) async {
    final repository = BrowsingRepository();
    await tester.pumpWidget(
        browsingApp(const LocalPodcastEpisodesScreen(), repository));
    await tester.pumpAndSettle();
    await openEpisodeActions(tester);
    await tester.tap(find.text('Delete episode'));
    await tester.pumpAndSettle();
    expect(find.text('Delete episode?'), findsOneWidget);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(find.text('Episode 0'), findsOneWidget);
    await openEpisodeActions(tester);
    await tester.tap(find.text('Delete episode'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(TextButton, 'Delete episode'));
    await tester.pumpAndSettle();
    expect(find.text('Episode 0'), findsNothing);
  });
  testWidgets(
      'account switch while delete confirmation is open does not delete the old episode',
      (tester) async {
    final selected = StateProvider<String>((ref) => 'local');
    final repository = BrowsingRepository();
    final store = BrowsingStore(repository);
    await tester.pumpWidget(browsingApp(
        const LocalPodcastEpisodesScreen(), repository,
        store: store, scope: (ref) async => ref.watch(selected)));
    await tester.pumpAndSettle();
    final container = ProviderScope.containerOf(
        tester.element(find.byType(LocalPodcastEpisodesScreen)));
    await openEpisodeActions(tester);
    await tester.tap(find.text('Delete episode'));
    await tester.pumpAndSettle();
    container.read(selected.notifier).state = 'account:B';
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(TextButton, 'Delete episode'));
    await tester.pumpAndSettle();
    expect(store.saves, isEmpty);
    expect(repository.deleted, false);
    expect(find.text('Episode 0'), findsOneWidget);
  });
  testWidgets(
      'authenticated publisher failure falls back to server metadata while keeping cached rows',
      (tester) async {
    final repository = BrowsingRepository()
      ..missingFeeds = ['unavailable']
      ..failedFeeds.add('unavailable');
    final metadata = FakeServerMetadata(repository, 'account:A');
    await tester.pumpWidget(browsingApp(
        const LocalPodcastEpisodesScreen(), repository,
        scope: (_) async => 'account:A', serverMetadata: metadata));
    await tester.pumpAndSettle();
    expect(metadata.feeds, ['unavailable']);
    expect(find.text('Episode 0'), findsOneWidget);
    expect(find.textContaining('feeds unavailable'), findsNothing);
  });

  testWidgets('standalone publisher failure never consults account metadata',
      (tester) async {
    final repository = BrowsingRepository()
      ..missingFeeds = ['unavailable']
      ..failedFeeds.add('unavailable');
    final metadata = FakeServerMetadata(repository, 'local');
    await tester.pumpWidget(browsingApp(
        const LocalPodcastEpisodesScreen(), repository,
        serverMetadata: metadata));
    await tester.pumpAndSettle();
    expect(metadata.feeds, isEmpty);
    expect(find.text('Episode 0'), findsOneWidget);
    expect(find.textContaining('feeds unavailable'), findsOneWidget);
  });
}
