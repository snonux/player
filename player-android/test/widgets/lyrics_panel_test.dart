import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/lyrics.dart';
import 'package:player_android/widgets/lyrics_panel.dart';

final _timed = Lyrics.fromJson({
  'synced': true,
  'lines': [
    {'text': 'First line', 'start': 1},
    {'text': 'Second line', 'start': 4},
  ],
});

void main() {
  testWidgets(
      'provider actions are explicit and saved lyrics hide online actions',
      (tester) async {
    var fetched = 0, removed = 0;
    final available = Lyrics.fromJson({'provider_enabled': true, 'lines': []});
    final provider = Lyrics.fromJson({
      'provider_enabled': true,
      'source': 'provider',
      'synced': true,
      'lines': [
        {
          'text': 'Provider line',
          'start': 0,
          'translations': {'fr': 'Texte'}
        }
      ]
    });
    Widget panel({bool saved = false}) => MaterialApp(
            home: Scaffold(
                body: LyricsPanel(
          load: () async => available,
          lookup: () async {
            fetched++;
            return provider;
          },
          removeProvider: () async {
            removed++;
            return available;
          },
          saved: saved,
          positionStream: const Stream.empty(),
          position: () => Duration.zero,
          onSeek: (_) async {},
        )));
    await tester.pumpWidget(panel());
    expect(fetched, 0);
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    expect(fetched, 0, reason: 'opening lyrics reads cache only');
    await tester.tap(find.text('Find lyrics online (send track tags)'));
    await tester.pumpAndSettle();
    expect(fetched, 1);
    expect(find.text('Lyrics: provider'), findsOneWidget);
    await tester.tap(find.text('Remove provider lyrics'));
    await tester.pumpAndSettle();
    expect(removed, 1);
    expect(find.text('No lyrics available.'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    await tester.pumpWidget(panel(saved: true));
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    expect(find.text('No saved lyrics'), findsOneWidget);
    expect(find.text('Find lyrics online (send track tags)'), findsNothing);
    expect(fetched, 1);
  });
  testWidgets('loads on tap, highlights positions and seeks a timed line',
      (tester) async {
    final positions = StreamController<Duration>.broadcast();
    addTearDown(positions.close);
    var calls = 0;
    Duration? sought;
    await tester.pumpWidget(MaterialApp(
        home: Scaffold(
            body: LyricsPanel(
      load: () async {
        calls++;
        return _timed;
      },
      positionStream: positions.stream,
      position: () => Duration.zero,
      onSeek: (position) async => sought = position,
    ))));
    expect(calls, 0);
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    expect(calls, 1);
    expect(
        tester
            .widget<ListTile>(find.byKey(const Key('lyrics_line_0')))
            .selected,
        isFalse);
    positions.add(const Duration(seconds: 5));
    await tester.pumpAndSettle();
    expect(
        tester
            .widget<ListTile>(find.byKey(const Key('lyrics_line_1')))
            .selected,
        isTrue);
    positions.add(const Duration(seconds: 2));
    await tester.pumpAndSettle();
    expect(
        tester
            .widget<ListTile>(find.byKey(const Key('lyrics_line_0')))
            .selected,
        isTrue);
    await tester.tap(find.text('Second line'));
    expect(sought, const Duration(seconds: 4));
    await tester.tap(find.text('Hide lyrics'));
    await tester.pump();
    expect(find.text('Second line'), findsNothing);
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    expect(calls, 1);
  });

  testWidgets(
      'current line remains visible beyond the list viewport and after backseek',
      (tester) async {
    final positions = StreamController<Duration>.broadcast();
    addTearDown(positions.close);
    final lyrics = Lyrics.fromJson({
      'synced': true,
      'lines': List.generate(30, (i) => {'text': 'Line $i', 'start': i * 10}),
    });
    await tester.pumpWidget(MaterialApp(
        home: Scaffold(
            body: LyricsPanel(
      load: () async => lyrics,
      positionStream: positions.stream,
      position: () => Duration.zero,
      onSeek: (_) async {},
    ))));
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    expect(find.text('Line 25'), findsNothing);
    positions.add(const Duration(seconds: 251));
    await tester.pumpAndSettle();
    expect(find.text('Current: Line 25').hitTestable(), findsOneWidget);
    positions.add(const Duration(seconds: 11));
    await tester.pumpAndSettle();
    expect(find.text('Current: Line 1').hitTestable(), findsOneWidget);
    expect(find.text('Current: Line 25'), findsNothing);
  });

  testWidgets(
      'long current text wraps and remains scrollable at large text sizes',
      (tester) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    });
    final text = List.filled(30, 'Long lyric').join(' ');
    await tester.pumpWidget(MaterialApp(
        home: MediaQuery(
      data: const MediaQueryData(textScaler: TextScaler.linear(2)),
      child: Scaffold(
          body: LyricsPanel(
        load: () async => Lyrics.fromJson({
          'synced': true,
          'lines': [
            {'text': text, 'start': 0}
          ],
        }),
        positionStream: const Stream.empty(),
        position: () => Duration.zero,
        onSeek: (_) async {},
      )),
    )));
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    final current =
        tester.widget<Text>(find.byKey(const Key('lyrics_current_line')));
    expect(current.data, 'Current: $text');
    expect(current.maxLines, isNull);
    expect(
        find.ancestor(
            of: find.byKey(const Key('lyrics_current_line')),
            matching: find.byType(SingleChildScrollView)),
        findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
      'reopening while paused reads the current seek position without a stream event',
      (tester) async {
    var position = Duration.zero;
    await tester.pumpWidget(MaterialApp(
        home: Scaffold(
            body: LyricsPanel(
      load: () async => _timed,
      positionStream: const Stream.empty(),
      position: () => position,
      onSeek: (_) async {},
    ))));
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('lyrics_current_line')), findsNothing);
    await tester.tap(find.text('Hide lyrics'));
    await tester.pump();
    position = const Duration(seconds: 5);
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    expect(find.text('Current: Second line'), findsOneWidget);
  });

  testWidgets('failure retries independently, then unavailable or plain lyrics',
      (tester) async {
    var calls = 0;
    Future<Lyrics> load() async {
      calls++;
      if (calls == 1) throw StateError('offline');
      return const Lyrics();
    }

    await tester.pumpWidget(MaterialApp(
        home: LyricsPanel(
      load: load,
      positionStream: const Stream.empty(),
      position: () => Duration.zero,
      onSeek: (_) async => fail('Unavailable lyrics cannot seek'),
    )));
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    expect(find.text('Could not load lyrics.'), findsOneWidget);
    await tester.tap(find.text('Retry lyrics'));
    await tester.pumpAndSettle();
    expect(find.text('No lyrics available.'), findsOneWidget);
    expect(calls, 2);
    await tester.pumpWidget(MaterialApp(
        home: LyricsPanel(
      key: const ValueKey('plain'),
      load: () async => const Lyrics(lines: [LyricsLine(text: 'Plain text')]),
      positionStream: const Stream.empty(),
      position: () => Duration.zero,
      onSeek: (_) async => fail('Plain lyrics cannot seek'),
    )));
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    expect(find.text('Plain text'), findsOneWidget);
    expect(tester.widget<ListTile>(find.byType(ListTile)).onTap, isNull);
  });

  testWidgets('changing source discards both loaded and pending old lyrics',
      (tester) async {
    final old = Completer<Lyrics>();
    Future<void> show(String source, Future<Lyrics> Function() load) =>
        tester.pumpWidget(
          MaterialApp(
              home: LyricsPanel(
            key: ValueKey(source),
            load: load,
            positionStream: const Stream.empty(),
            position: () => Duration.zero,
            onSeek: (_) async {},
          )),
        );
    await show('old', () => old.future);
    await tester.tap(find.text('Lyrics'));
    await tester.pump();
    await show('new', () async => const Lyrics());
    old.complete(_timed);
    await tester.pumpAndSettle();
    expect(find.text('First line'), findsNothing);
    expect(find.byKey(const Key('lyrics_panel')), findsNothing);
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    expect(find.text('No lyrics available.'), findsOneWidget);
    await show('third', () async => _timed);
    expect(find.text('No lyrics available.'), findsNothing);
  });
  testWidgets(
      'native language choice retains seek state, overlapping voices and word emphasis',
      (tester) async {
    final positions = StreamController<Duration>.broadcast();
    addTearDown(positions.close);
    var seeks = 0;
    final lyrics = Lyrics.fromJson({
      'language': 'en',
      'synced': true,
      'lines': [
        {
          'text': 'Hello world',
          'start': 1,
          'end': 5,
          'voice': 'Lead',
          'translations': {'fr': 'Bonjour monde'},
          'words': [
            {'text': 'Hello ', 'start': 1, 'end': 2},
            {'text': 'world', 'start': 2, 'end': 5}
          ]
        },
        {
          'text': 'Harmony',
          'start': 1,
          'end': 3,
          'voice': 'Harmony',
          'words': [
            {'text': 'Harmony', 'start': 1, 'end': 3}
          ]
        },
      ]
    });
    await tester.pumpWidget(MaterialApp(
        home: Scaffold(
            body: LyricsPanel(
      load: () async => lyrics,
      positionStream: positions.stream,
      position: () => const Duration(seconds: 4),
      onSeek: (_) async {
        seeks++;
      },
    ))));
    await tester.tap(find.text('Lyrics'));
    await tester.pumpAndSettle();
    Text banner() =>
        tester.widget<Text>(find.byKey(const Key('lyrics_current_line')));
    var spans = banner().textSpan! as TextSpan;
    expect(spans.toPlainText(), 'Current: Voice: Lead · Hello world');
    expect((spans.children![2] as TextSpan).style!.decoration,
        TextDecoration.underline);
    positions.add(const Duration(milliseconds: 1500));
    await tester.pumpAndSettle();
    expect(
        tester
            .widget<ListTile>(find.byKey(const Key('lyrics_line_0')))
            .selected,
        isTrue);
    // The second voice can be outside the small list viewport; the current banner remains visible.
    expect(
        banner().textSpan!.toPlainText(), 'Current: Voice: Harmony · Harmony');
    await tester.tap(find.byKey(const Key('lyrics_language')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Language: fr').last);
    await tester.pumpAndSettle();
    expect(seeks, 0);
    expect(find.text('Original: en'), findsOneWidget);
    expect(banner().textSpan!.toPlainText(),
        'Current: Voice: Harmony · Harmony (Original: en)');
    expect(
        ((banner().textSpan! as TextSpan).children![1] as TextSpan)
            .style!
            .decoration,
        TextDecoration.underline);
    positions.add(const Duration(seconds: 4));
    await tester.pumpAndSettle();
    expect(banner().data, 'Current: Voice: Lead · Bonjour monde');
    await tester.tap(find.text('Bonjour monde'));
    expect(seeks, 1);
    await tester.tap(find.byKey(const Key('lyrics_language')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Language: Original (en)').last);
    await tester.pumpAndSettle();
    positions.add(const Duration(milliseconds: 1500));
    await tester.pumpAndSettle();
    positions.add(const Duration(seconds: 4));
    await tester.pumpAndSettle();
    spans = banner().textSpan! as TextSpan;
    expect((spans.children![2] as TextSpan).style!.decoration,
        TextDecoration.underline);
    expect(tester.takeException(), isNull);
  });
}
