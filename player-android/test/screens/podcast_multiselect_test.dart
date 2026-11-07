import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/screens/standalone_podcasts_screen.dart';
import 'podcast_browsing_fakes.dart';

class PartialStore extends BrowsingStore {
  PartialStore(super.fake);
  bool failSecond = true;
  final episodes = <String>[];
  @override
  Future<void> saveEpisode(String scope, String feed, String episode,
      {double? seconds,
      bool? completed,
      bool? deleted,
      String source = 'manual',
      String enclosureUrl = '',
      int? observedMs}) async {
    if (episode == 'guid:1' && failSecond) throw StateError('disk');
    episodes.add(episode);
    await super.saveEpisode(scope, feed, episode,
        completed: completed, deleted: deleted);
  }
}

Future<void> action(WidgetTester tester, String label) async {
  await tester.tap(find.byTooltip('Selected episode actions'));
  await tester.pumpAndSettle();
  await tester.tap(find.text(label));
  if (label == 'Delete episodes') {
    await tester.pump(const Duration(milliseconds: 400));
  } else {
    await tester.pumpAndSettle();
  }
}

void main() {
  podcastMultiselectTests();
  podcastSelectionLayoutTests();
}

void podcastMultiselectTests() {
  for (final feed in [null, 'feed:example/feed']) {
    for (final grid in [false, true]) {
      testWidgets(
          'bulk completion keeps failures for retry ${feed ?? 'global'} $grid',
          (tester) async {
        final repository = BrowsingRepository()..count = 3;
        final store = PartialStore(repository);
        await tester.pumpWidget(browsingApp(
            LocalPodcastEpisodesScreen(feedKey: feed), repository,
            store: store));
        await tester.pumpAndSettle();
        if (grid) {
          await tester.tap(find.text('Grid'));
          await tester.pumpAndSettle();
        }
        await tester.longPress(find.text('Episode 0'));
        await tester.pumpAndSettle();
        expect(find.text('1 selected'), findsOneWidget);
        expect(find.byTooltip('More episode actions'), findsNothing);
        await tester.tap(find.byTooltip('Select all visible episodes'));
        await tester.pumpAndSettle();
        expect(find.text('3 selected'), findsOneWidget);
        await action(tester, 'Mark completed');
        expect(store.episodes, ['guid:0', 'guid:2']);
        expect(find.text('1 selected'), findsOneWidget);
        expect(find.textContaining('Still selected; retry'), findsOneWidget);
        expect(
            tester
                .widgetList<Checkbox>(find.byType(Checkbox))
                .where((check) => check.value == true),
            hasLength(1));
        expect(find.text('Episode 1'), findsOneWidget);
        store.failSecond = false;
        await action(tester, 'Mark completed');
        expect(store.episodes, ['guid:0', 'guid:2', 'guid:1']);
        expect(find.byTooltip('Cancel selection'), findsNothing);
        expect(tester.takeException(), isNull);
      });
    }
  }
  testWidgets('explicit selection, delete cancellation and confirmation',
      (tester) async {
    final repository = BrowsingRepository()..count = 2;
    final store = PartialStore(repository)..failSecond = false;
    await tester.pumpWidget(browsingApp(
        const LocalPodcastEpisodesScreen(), repository,
        store: store));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Podcast views'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Select episodes'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Select all visible episodes'));
    await tester.pumpAndSettle();
    await action(tester, 'Delete episodes');
    expect(find.text('Delete 2 episodes?'), findsOneWidget);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(store.saves, isEmpty);
    expect(find.text('2 selected'), findsOneWidget);
    await action(tester, 'Delete episodes');
    await tester.tap(find.text('Delete'));
    await tester.pumpAndSettle();
    expect(store.episodes, ['guid:0', 'guid:1']);
    expect(find.text('Episode 0'), findsNothing);
  });
  testWidgets('switching library during confirmation stops destructive batch',
      (tester) async {
    final scope = StateProvider<String>((_) => 'local');
    final repository = BrowsingRepository()..count = 2;
    final store = PartialStore(repository)..failSecond = false;
    await tester.pumpWidget(browsingApp(
        const LocalPodcastEpisodesScreen(), repository,
        store: store, scope: (ref) async => ref.watch(scope)));
    await tester.pumpAndSettle();
    await tester.longPress(find.text('Episode 0'));
    await tester.pumpAndSettle();
    await action(tester, 'Delete episodes');
    final container = ProviderScope.containerOf(
        tester.element(find.byType(LocalPodcastEpisodesScreen)));
    container.read(scope.notifier).state = 'account:other:2';
    await tester.pumpAndSettle();
    await tester.tap(find.text('Delete'));
    await tester.pumpAndSettle();
    expect(store.saves, isEmpty);
    expect(find.byTooltip('Cancel selection'), findsNothing);
  });
  testWidgets('Next and Previous page remain available and clear selection',
      (tester) async {
    final repository = BrowsingRepository()..count = 42;
    await tester.pumpWidget(
        browsingApp(const LocalPodcastEpisodesScreen(), repository));
    await tester.pumpAndSettle();
    await tester.longPress(find.text('Episode 0'));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('Next page'), 600,
        maxScrolls: 20);
    await tester.tap(find.text('Next page'));
    await tester.pumpAndSettle();
    expect(repository.queries.last.$3, 40);
    expect(find.byTooltip('Cancel selection'), findsNothing);
    await tester.longPress(find.text('Episode 40'));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('Previous page'), 300,
        maxScrolls: 20);
    await tester.tap(find.text('Previous page'));
    await tester.pumpAndSettle();
    expect(find.text('Episode 0'), findsOneWidget);
    expect(find.byTooltip('Cancel selection'), findsNothing);
  });
  testWidgets('completion filter changes clear selection', (tester) async {
    final repository = BrowsingRepository()..count = 4;
    await tester.pumpWidget(
        browsingApp(const LocalPodcastEpisodesScreen(), repository));
    await tester.pumpAndSettle();
    await tester.longPress(find.text('Episode 0'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('All'));
    await tester.pumpAndSettle();
    expect(find.byTooltip('Cancel selection'), findsNothing);
    expect(find.byType(Checkbox), findsNothing);
  });
}

void podcastSelectionLayoutTests() {
  for (final size in [const Size(390, 844), const Size(844, 390)]) {
    for (final scale in [1.6, 2.0]) {
      for (final grid in [false, true]) {
        testWidgets(
            'selected layout ${size.width}x${size.height} text $scale grid $grid',
            (tester) async {
          await tester.binding.setSurfaceSize(size);
          addTearDown(() => tester.binding.setSurfaceSize(null));
          final repository = BrowsingRepository()..count = 2;
          repository.metadata = {
            'title':
                'A long podcast episode title ${'with readable words ' * 12}'
          };
          await tester.pumpWidget(browsingApp(
              MediaQuery(
                  data: MediaQueryData(
                      size: size, textScaler: TextScaler.linear(scale)),
                  child: const LocalPodcastEpisodesScreen(
                      feedKey: 'feed:example/feed')),
              repository));
          await tester.pumpAndSettle();
          if (grid) {
            await tester.tap(find.text('Grid'));
            await tester.pumpAndSettle();
          }
          await tester.tap(find.byTooltip('Podcast views'));
          await tester.pumpAndSettle();
          await tester.tap(find.text('Select episodes'));
          await tester.pumpAndSettle();
          final check = find.byType(Checkbox).first;
          await tester.ensureVisible(check);
          await tester.pumpAndSettle();
          expect(tester.getSize(check).width, greaterThanOrEqualTo(48));
          expect(tester.getSize(check).height, greaterThanOrEqualTo(48));
          await tester.tap(check);
          await tester.pumpAndSettle();
          expect(find.text('1 selected'), findsOneWidget);
          expect(find.byTooltip('More episode actions'), findsNothing);
          await tester.tap(find.byTooltip('Selected episode actions'));
          await tester.pumpAndSettle();
          expect(find.text('Mark completed'), findsOneWidget);
          expect(find.text('Delete episodes'), findsOneWidget);
          Navigator.of(tester.element(find.text('Mark completed')),
                  rootNavigator: true)
              .pop();
          await tester.pumpAndSettle();
          expect(tester.takeException(), isNull);
          if (grid) {
            await tester.binding.handlePopRoute();
            await tester.pumpAndSettle();
          } else {
            await tester.tap(find.byTooltip('Cancel selection'));
            await tester.pumpAndSettle();
          }
          expect(find.byType(Checkbox), findsNothing);
          expect(find.byTooltip('Cancel selection'), findsNothing);
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox());
        });
      }
    }
  }
}
