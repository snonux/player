import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/screens/podcast_list_screen.dart';
import 'package:player_android/screens/podcast_episodes_screen.dart';
import 'podcast_browsing_fakes.dart';

void main() {
  testWidgets(
      'native Podcasts opens device folders and all episodes without a server API',
      (tester) async {
    final repository = BrowsingRepository();
    await tester.pumpWidget(browsingApp(const PodcastListScreen(), repository));
    await tester.pumpAndSettle();
    expect(find.text('Podcasts on this device'), findsOneWidget);
    await tester.tap(find.text('My podcast'));
    await tester.pumpAndSettle();
    expect(repository.queries.last.$1, 'feed:example/feed');
    expect(repository.queries.last.$2, 'uncompleted');
    expect(find.textContaining('In progress'), findsOneWidget);
    await tester.tap(find.byTooltip('More episode actions'));
    await tester.pumpAndSettle();
    expect(find.text('Mark completed'), findsOneWidget);
    Navigator.of(tester.element(find.text('Mark completed')),
            rootNavigator: true)
        .pop();
    await tester.pumpAndSettle();
    await tester.pageBack();
    await tester.pumpAndSettle();
    await tester.tap(find.text('All podcast episodes'));
    await tester.pumpAndSettle();
    expect(repository.queries.last.$1, isNull);
    expect(find.text('My podcast'), findsOneWidget);
    expect(find.text('Download'), findsOneWidget);
  });

  testWidgets(
      'legacy numeric podcast route opens local picker instead of server downloads',
      (tester) async {
    await tester.pumpWidget(browsingApp(
        const PodcastEpisodesScreen(setId: 42), BrowsingRepository()));
    await tester.pumpAndSettle();
    expect(find.text('Podcasts on this device'), findsOneWidget);
    expect(find.text('All podcast episodes'), findsOneWidget);
    await tester.tap(find.byTooltip('Subscribe to podcast'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'not a feed');
    await tester.tap(find.text('Subscribe'));
    await tester.pumpAndSettle();
    expect(
        find.textContaining('Enter a valid podcast feed URL'), findsOneWidget);
    await tester.pump(const Duration(seconds: 1));
  });
}
