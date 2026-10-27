import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/thumbnail_size_provider.dart';
import 'package:player_android/widgets/overflow_menu.dart';
import 'package:player_android/widgets/thumbnail_zoom.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../helpers/thumbnail_gestures.dart';

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('malformed saved size falls back and rapid writes persist in order',
      () async {
    for (final value in [-5, 99, 'bad']) {
      SharedPreferences.setMockInitialValues({'library_thumbnail_size': value});
      final container = ProviderContainer();
      expect(await container.read(thumbnailSizeProvider.future),
          defaultThumbnailSize);
      container.dispose();
    }
    SharedPreferences.setMockInitialValues({});
    final container = ProviderContainer();
    await container.read(thumbnailSizeProvider.future);
    final notifier = container.read(thumbnailSizeProvider.notifier);
    await Future.wait(
        [notifier.setSize(-10), notifier.setSize(99), notifier.setSize(1)]);
    expect(container.read(thumbnailSizeProvider).requireValue, 1);
    container.dispose();
    final reopened = ProviderContainer();
    expect(await reopened.read(thumbnailSizeProvider.future), 1);
    reopened.dispose();
  });

  testWidgets(
      'real two-finger pinch clamps and saves; one-finger scroll, tap and long press still work',
      (tester) async {
    tester.view.physicalSize = const Size(360, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final container = ProviderContainer();
    addTearDown(container.dispose);
    await container.read(thumbnailSizeProvider.future);
    final scroll = ScrollController();
    addTearDown(scroll.dispose);
    var taps = 0;
    var longPresses = 0;
    await tester.pumpWidget(UncontrolledProviderScope(
      container: container,
      child: MaterialApp(
          home: Scaffold(
              body: ThumbnailZoom(
        builder: (context, scale) => GridView.builder(
          controller: scroll,
          gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: thumbnailColumns(360, scale),
          ),
          itemCount: 80,
          itemBuilder: (_, index) => InkWell(
            onTap: () => taps++,
            onLongPress: () => longPresses++,
            child: Center(child: Text('tile $index')),
          ),
        ),
      ))),
    ));
    await tester.pumpAndSettle();
    await tester.drag(find.byType(GridView), const Offset(0, -220));
    await tester.pumpAndSettle();
    expect(scroll.offset, greaterThan(100));
    expect(container.read(thumbnailSizeProvider).requireValue,
        defaultThumbnailSize);
    scroll.jumpTo(0);
    await tester.pumpAndSettle();
    await tester.tap(find.text('tile 0'));
    await tester.longPress(find.text('tile 0'));
    expect(taps, 1);
    expect(longPresses, 1);

    final area =
        tester.getRect(find.byKey(const Key('library_preview_gestures')));
    final first = await tester.createGesture(pointer: 51);
    final second = await tester.createGesture(pointer: 52);
    await first.down(area.center - const Offset(60, 0));
    await second.down(area.center + const Offset(60, 0));
    await tester.pump();
    await first.moveTo(area.center - const Offset(180, 0));
    await second.moveTo(area.center + const Offset(180, 0));
    await tester.pump();
    expect(find.byKey(const Key('preview_size_badge')), findsOneWidget);
    expect(find.text('165%'), findsOneWidget);
    await first.up();
    await second.up();
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('preview_size_badge')), findsNothing,
        reason: 'the size badge shows only while pinching');
    expect(container.read(thumbnailSizeProvider).requireValue, 4);
    await pinchPreviews(tester, 0.1);
    expect(container.read(thumbnailSizeProvider).requireValue, 0);
    expect(taps, 1, reason: 'A pinch must not activate either touched card');
    expect(longPresses, 1);
    await pinchPreviews(tester, 3);
    expect(taps, 1);
    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getInt('library_thumbnail_size'), 4);
    expect(tester.takeException(), isNull);
  });

  testWidgets('cancelling a pinch leaves later taps and scrolling available',
      (tester) async {
    var taps = 0;
    await tester.pumpWidget(ProviderScope(
        child: MaterialApp(
            home: Scaffold(
      body: ThumbnailZoom(
          builder: (_, scale) => ListView.builder(
                itemCount: 50,
                itemBuilder: (_, index) =>
                    ListTile(title: Text('entry $index'), onTap: () => taps++),
              )),
    ))));
    await tester.pumpAndSettle();
    final area =
        tester.getRect(find.byKey(const Key('library_preview_gestures')));
    final one = await tester.createGesture(pointer: 31);
    final two = await tester.createGesture(pointer: 32);
    await one.down(area.center - const Offset(40, 0));
    await two.down(area.center + const Offset(40, 0));
    await one.cancel();
    await two.cancel();
    await tester.pumpAndSettle();
    expect(taps, 0);
    await tester.tap(find.text('entry 0'));
    await tester.drag(find.byType(ListView), const Offset(0, -200));
    await tester.pumpAndSettle();
    expect(taps, 1);
    expect(find.byKey(const Key('preview_size_badge')), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
      'menu preview sizes step, stop at the ends and reset, with large text on a narrow phone',
      (tester) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final container = ProviderContainer();
    addTearDown(container.dispose);
    await container.read(thumbnailSizeProvider.future);
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context)
                .copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: Scaffold(
              appBar: AppBar(actions: [
                OverflowMenuButton(
                    items: (_) => const [PreviewSizeMenuItems()]),
              ]),
              body: ThumbnailZoom(
                  builder: (_, scale) =>
                      ListView(children: const [Text('Library')]))),
        )));
    await tester.pumpAndSettle();
    expect(find.text('Previews'), findsNothing,
        reason: 'preview size lives in the menu, not above the grid');
    Future<void> choose(Key item) async {
      await tester.tap(find.byTooltip('More options'));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(item));
      await tester.pumpAndSettle();
    }

    await choose(const Key('preview_size_larger'));
    expect(container.read(thumbnailSizeProvider).requireValue, 3);
    await choose(const Key('preview_size_larger'));
    expect(container.read(thumbnailSizeProvider).requireValue, 4);
    await tester.tap(find.byTooltip('More options'));
    await tester.pumpAndSettle();
    expect(find.text('Now 165%'), findsOneWidget);
    expect(
        tester
            .widget<ListTile>(find.descendant(
                of: find.byKey(const Key('preview_size_larger')),
                matching: find.byType(ListTile)))
            .enabled,
        isFalse);
    await tester.tap(find.byKey(const Key('preview_size_reset')));
    await tester.pumpAndSettle();
    expect(container.read(thumbnailSizeProvider).requireValue,
        defaultThumbnailSize);
    await choose(const Key('preview_size_smaller'));
    await choose(const Key('preview_size_smaller'));
    expect(container.read(thumbnailSizeProvider).requireValue, 0);
    await tester.tap(find.byTooltip('More options'));
    await tester.pumpAndSettle();
    expect(
        tester
            .widget<ListTile>(find.descendant(
                of: find.byKey(const Key('preview_size_smaller')),
                matching: find.byType(ListTile)))
            .enabled,
        isFalse);
    expect(tester.takeException(), isNull);
  });
}
