import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/widgets/music_discovery_panel.dart';

void main() {
  testWidgets(
      'narrow discovery keeps empty sources and View all snapshot accessible',
      (tester) async {
    tester.view.physicalSize = const Size(390, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final snapshot = <Map<String, dynamic>>[
      {
        'album_key': 'b',
        'title': 'A very long album title ${List.filled(80, '長').join()}',
        'artist': 'Artist'
      },
      {'album_key': 'a', 'title': 'Second album', 'artist': 'Another artist'},
    ];
    String? chosen;
    List<Map<String, dynamic>>? chosenItems;
    await tester.pumpWidget(ProviderScope(
        child: MaterialApp(
            theme: ThemeData.dark(),
            home: Scaffold(
                body: MusicDiscoveryPanel(
              collections: {'random': snapshot},
              onViewAll: (key, items) {
                chosen = key;
                chosenItems = items;
              },
              onOpenAlbum: (_) {},
              onBrowse: () {},
              onRefresh: () async {},
            )))));
    expect(find.text('Discover albums'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('View all random albums'), 300,
        scrollable: find.byType(Scrollable).first);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('View all random albums'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('View all random albums'));
    expect(chosen, 'random');
    expect(identical(chosenItems, snapshot), isTrue,
        reason: 'View all retains displayed ordered snapshot');
    expect(tester.takeException(), isNull);
    await tester.scrollUntilVisible(
        find.text('Rate an album to add it here.'), 300,
        scrollable: find.byType(Scrollable).first);
    expect(find.text('Rate an album to add it here.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
