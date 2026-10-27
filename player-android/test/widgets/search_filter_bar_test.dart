// Widget tests for SearchFilterBar (widgets/search_filter_bar.dart).
//
// Tests cover:
//   1. Text input triggers debounced callback after 400 ms.
//   2. Text input does NOT fire immediately (debounce delay is respected).
//   3. Typing quickly cancels the previous timer (only last value fires).
//   4. Clearing the search text passes null as the query.
//   5. Only the search field shows until something filters the list.
//   6. The filter sheet result (type, sort) is applied; favourites persist.
//   7. The summary names active filters; Clear keeps the sort order.
//   9. Initial filter state is reflected in the UI on first render.
//  10. didUpdateWidget: external filter change updates the bar's UI.
//
// Run with: flutter test test/widgets/search_filter_bar_test.dart

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/media_filter.dart';
import 'package:player_android/widgets/search_filter_bar.dart';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Pumps a [SearchFilterBar] in isolation inside a [MaterialApp].
///
/// [onChanged] receives filter updates and can be used to assert on the
/// emitted value.  [initialFilter] seeds the bar's initial state.
Future<void> _pumpBar(
  WidgetTester tester, {
  MediaFilter initialFilter = const MediaFilter.empty(),
  required void Function(MediaFilter) onChanged,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: SearchFilterBar(
          initialFilter: initialFilter,
          onFiltersChanged: onChanged,
        ),
      ),
    ),
  );
}

// Key used to locate the [_ControlledFilterBar] state in rebuild tests.
final _controlledBarKey = GlobalKey<_ControlledFilterBarState>();

/// Stateful wrapper that lets tests push a new [MediaFilter] into
/// [SearchFilterBar] from outside (simulating an external control like the
/// MediaGridScreen app-bar shortcut).
class _ControlledFilterBar extends StatefulWidget {
  const _ControlledFilterBar({
    super.key,
    required this.initialFilter,
    required this.onChanged,
  });

  final MediaFilter initialFilter;
  final void Function(MediaFilter) onChanged;

  @override
  _ControlledFilterBarState createState() => _ControlledFilterBarState();
}

class _ControlledFilterBarState extends State<_ControlledFilterBar> {
  late MediaFilter _filter;

  @override
  void initState() {
    super.initState();
    _filter = widget.initialFilter;
  }

  /// Simulates an external filter update (e.g. app-bar shortcut).
  void pushFilter(MediaFilter filter) {
    setState(() => _filter = filter);
  }

  @override
  Widget build(BuildContext context) {
    return SearchFilterBar(
      initialFilter: _filter,
      onFiltersChanged: widget.onChanged,
    );
  }
}

/// Pumps [_ControlledFilterBar] and returns the wrapper key.
Future<GlobalKey<_ControlledFilterBarState>> _pumpControlledBar(
  WidgetTester tester, {
  MediaFilter initialFilter = const MediaFilter.empty(),
  required void Function(MediaFilter) onChanged,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: _ControlledFilterBar(
          key: _controlledBarKey,
          initialFilter: initialFilter,
          onChanged: onChanged,
        ),
      ),
    ),
  );
  return _controlledBarKey;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  // --------------------------------------------------------------------------
  // Text search / debounce
  // --------------------------------------------------------------------------

  group('text search — debounce', () {
    testWidgets('fires callback after 400 ms debounce', (tester) async {
      MediaFilter? emitted;
      await _pumpBar(tester, onChanged: (f) => emitted = f);

      await tester.enterText(find.byKey(const Key('search_input')), 'hello');

      // Before the debounce delay the callback should NOT have fired.
      expect(emitted, isNull);

      // Advance by the exact debounce delay.
      await tester.pump(const Duration(milliseconds: 400));

      expect(emitted, isNotNull);
      expect(emitted!.query, equals('hello'));
    });

    testWidgets('does not fire before 400 ms', (tester) async {
      MediaFilter? emitted;
      await _pumpBar(tester, onChanged: (f) => emitted = f);

      await tester.enterText(find.byKey(const Key('search_input')), 'abc');

      // Advance to just before the debounce window closes (399 ms < 400 ms).
      await tester.pump(const Duration(milliseconds: 399));

      // Callback must not have been called yet — the timer has not fired.
      expect(emitted, isNull);

      // Consume the remaining timer so the test does not leave pending async
      // work that leaks into subsequent tests.
      await tester.pumpAndSettle();
    });

    testWidgets('only last typed value fires when typing quickly',
        (tester) async {
      final emittedValues = <String?>[];
      await _pumpBar(tester, onChanged: (f) => emittedValues.add(f.query));

      // Type 'a', then immediately 'ab', then 'abc' — each within 200 ms.
      await tester.enterText(find.byKey(const Key('search_input')), 'a');
      await tester.pump(const Duration(milliseconds: 200));
      await tester.enterText(find.byKey(const Key('search_input')), 'ab');
      await tester.pump(const Duration(milliseconds: 200));
      await tester.enterText(find.byKey(const Key('search_input')), 'abc');

      // Wait for the final debounce to settle.
      await tester.pump(const Duration(milliseconds: 400));

      // Only the last value should have been emitted (the first two timers
      // were cancelled before they fired).
      expect(emittedValues, hasLength(1));
      expect(emittedValues.first, equals('abc'));
    });

    testWidgets('clears query (passes null) when text is emptied',
        (tester) async {
      MediaFilter? emitted;
      await _pumpBar(
        tester,
        initialFilter: MediaFilter(query: 'hello'),
        onChanged: (f) => emitted = f,
      );

      // Clear the field.
      await tester.enterText(find.byKey(const Key('search_input')), '');
      await tester.pump(const Duration(milliseconds: 400));

      expect(emitted, isNotNull);
      // An empty string is normalised to null by the bar.
      expect(emitted!.query, isNull);
    });
  });

  // --------------------------------------------------------------------------
  // Type filter chips
  // --------------------------------------------------------------------------

  group('filters button and summary', () {
    Future<void> pumpWithSheet(WidgetTester tester,
        {required MediaFilter initial,
        required void Function(MediaFilter) onChanged,
        required Future<MediaFilter?> Function(MediaFilter) sheet}) async {
      await tester.pumpWidget(MaterialApp(
          home: Scaffold(
              body: SearchFilterBar(
        initialFilter: initial,
        scopeLabel: 'Selected sets: Music',
        onFiltersChanged: onChanged,
        onOpenFilters: sheet,
      ))));
    }

    testWidgets('only the search field shows while nothing is filtered',
        (tester) async {
      await pumpWithSheet(tester,
          initial: const MediaFilter.empty(),
          onChanged: (_) {},
          sheet: (_) async => null);
      expect(find.byKey(const Key('search_input')), findsOneWidget);
      expect(find.byKey(const Key('media_filters_button')), findsOneWidget);
      expect(find.byKey(const Key('media_filter_summary')), findsNothing);
      expect(find.byKey(const Key('media_filter_clear')), findsNothing);
      expect(find.byType(ChoiceChip), findsNothing);
      expect(find.byKey(const Key('favorites_toggle')), findsNothing);
    });

    testWidgets('the sheet result applies type and sort and keeps favourites',
        (tester) async {
      MediaFilter? emitted;
      await pumpWithSheet(tester,
          initial: MediaFilter(favoritesOnly: true),
          onChanged: (f) => emitted = f,
          sheet: (filter) async =>
              filter.copyWith(type: 'video', sortBy: 'name'));
      await tester.tap(find.byKey(const Key('media_filters_button')));
      await tester.pumpAndSettle();
      expect(emitted!.type, 'video');
      expect(emitted!.sortBy, 'name');
      expect(emitted!.favoritesOnly, isTrue);
      expect(find.text('Video · Favourites · Sort: Name'), findsOneWidget);
    });

    testWidgets('summary names scope and advanced filters; Clear keeps sort',
        (tester) async {
      MediaFilter? emitted;
      await pumpWithSheet(tester,
          initial: MediaFilter(
              setIds: [3], type: 'audio', tags: ['keep'], sortBy: 'date'),
          onChanged: (f) => emitted = f,
          sheet: (_) async => null);
      expect(
          find.text('Selected sets: Music · Audio · Sort: Date · Tags: keep'),
          findsOneWidget);
      await tester.tap(find.byKey(const Key('media_filter_clear')));
      await tester.pumpAndSettle();
      expect(emitted, MediaFilter(sortBy: 'date'));
      expect(find.text('Sort: Date'), findsOneWidget);
      expect(find.byKey(const Key('media_filter_clear')), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Initial filter reflected in UI
  // --------------------------------------------------------------------------

  group('initial filter state', () {
    testWidgets('initial query is shown in the search field', (tester) async {
      await _pumpBar(
        tester,
        initialFilter: MediaFilter(query: 'test query'),
        onChanged: (_) {},
      );

      expect(
        tester
            .widget<TextField>(find.byKey(const Key('search_input')))
            .controller
            ?.text,
        equals('test query'),
      );
    });
  });

  // --------------------------------------------------------------------------
  // External filter update (didUpdateWidget)
  // --------------------------------------------------------------------------

  group('external filter update', () {
    testWidgets('an external favourites change is kept by later searches',
        (tester) async {
      MediaFilter? emitted;
      final key = await _pumpControlledBar(
        tester,
        initialFilter: MediaFilter(favoritesOnly: false),
        onChanged: (f) => emitted = f,
      );
      await tester.pumpAndSettle();

      // The app bar menu changes the filter from outside the bar.
      key.currentState!.pushFilter(MediaFilter(favoritesOnly: true));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const Key('search_input')), 'jazz');
      await tester.pump(const Duration(milliseconds: 500));

      expect(emitted!.query, 'jazz');
      expect(emitted!.favoritesOnly, isTrue);
    });

    testWidgets('search text updates when query is changed externally',
        (tester) async {
      final key = await _pumpControlledBar(
        tester,
        initialFilter: MediaFilter(query: 'original'),
        onChanged: (_) {},
      );
      await tester.pumpAndSettle();

      expect(
        tester
            .widget<TextField>(find.byKey(const Key('search_input')))
            .controller
            ?.text,
        equals('original'),
      );

      // External clear: push a filter with no query.
      key.currentState!.pushFilter(const MediaFilter.empty());
      await tester.pumpAndSettle();

      expect(
        tester
            .widget<TextField>(find.byKey(const Key('search_input')))
            .controller
            ?.text,
        equals(''),
      );
    });
  });
  testWidgets(
      'Clear cancels pending search and keeps sort, sheet receives latest query',
      (tester) async {
    final emitted = <MediaFilter>[];
    MediaFilter? opened;
    await tester.pumpWidget(MaterialApp(
        home: Scaffold(
            body: SearchFilterBar(
      initialFilter: MediaFilter(tags: ['keep'], sortBy: 'date'),
      scopeLabel: 'Current set: Music',
      onFiltersChanged: emitted.add,
      onOpenFilters: (filter) async {
        opened = filter;
        return null;
      },
    ))));
    await tester.enterText(find.byKey(const Key('search_input')), 'late');
    await tester.tap(find.byKey(const Key('media_filter_clear')));
    await tester.pump(const Duration(seconds: 1));
    expect(emitted, [MediaFilter(sortBy: 'date')]);
    await tester.enterText(find.byKey(const Key('search_input')), 'latest');
    await tester.tap(find.byKey(const Key('media_filters_button')));
    await tester.pump();
    expect(opened!.query, 'latest');
    expect(opened!.sortBy, 'date');
    await tester.pump(const Duration(seconds: 1));
    expect(emitted.length, 2);
  });
}
