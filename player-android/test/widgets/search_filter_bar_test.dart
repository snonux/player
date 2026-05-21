// Widget tests for SearchFilterBar (widgets/search_filter_bar.dart).
//
// Tests cover:
//   1. Text input triggers debounced callback after 400 ms.
//   2. Text input does NOT fire immediately (debounce delay is respected).
//   3. Typing quickly cancels the previous timer (only last value fires).
//   4. Clearing the search text passes null as the query.
//   5. Selecting a type chip updates the filter type.
//   6. Tapping the "All" chip clears the type filter.
//   7. Toggling the favourites button flips favoritesOnly.
//   8. Picking a sort option updates sortBy in the callback.
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
  MediaFilter initialFilter = const MediaFilter(),
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
  MediaFilter initialFilter = const MediaFilter(),
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
        initialFilter: const MediaFilter(query: 'hello'),
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

  group('type filter chips', () {
    testWidgets('tapping Video chip sets type to "video"', (tester) async {
      MediaFilter? emitted;
      await _pumpBar(tester, onChanged: (f) => emitted = f);

      await tester.tap(find.byKey(const Key('type_chip_video')));
      await tester.pumpAndSettle();

      expect(emitted, isNotNull);
      expect(emitted!.type, equals('video'));
    });

    testWidgets('tapping Audio chip sets type to "audio"', (tester) async {
      MediaFilter? emitted;
      await _pumpBar(tester, onChanged: (f) => emitted = f);

      await tester.tap(find.byKey(const Key('type_chip_audio')));
      await tester.pumpAndSettle();

      expect(emitted!.type, equals('audio'));
    });

    testWidgets('tapping Image chip sets type to "image"', (tester) async {
      MediaFilter? emitted;
      await _pumpBar(tester, onChanged: (f) => emitted = f);

      await tester.tap(find.byKey(const Key('type_chip_image')));
      await tester.pumpAndSettle();

      expect(emitted!.type, equals('image'));
    });

    testWidgets('tapping All chip clears the type filter', (tester) async {
      MediaFilter? emitted;
      await _pumpBar(
        tester,
        initialFilter: const MediaFilter(type: 'video'),
        onChanged: (f) => emitted = f,
      );

      await tester.tap(find.byKey(const Key('type_chip_all')));
      await tester.pumpAndSettle();

      expect(emitted!.type, isNull);
    });

    testWidgets('all four type chips are rendered', (tester) async {
      await _pumpBar(tester, onChanged: (_) {});

      expect(find.byKey(const Key('type_chip_all')), findsOneWidget);
      expect(find.byKey(const Key('type_chip_video')), findsOneWidget);
      expect(find.byKey(const Key('type_chip_audio')), findsOneWidget);
      expect(find.byKey(const Key('type_chip_image')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Favourites toggle
  // --------------------------------------------------------------------------

  group('favourites toggle', () {
    testWidgets('toggling favourites sets favoritesOnly to true',
        (tester) async {
      MediaFilter? emitted;
      await _pumpBar(tester, onChanged: (f) => emitted = f);

      await tester.tap(find.byKey(const Key('favorites_toggle')));
      await tester.pumpAndSettle();

      expect(emitted!.favoritesOnly, isTrue);
    });

    testWidgets('toggling favourites a second time sets favoritesOnly to false',
        (tester) async {
      MediaFilter? emitted;
      await _pumpBar(
        tester,
        initialFilter: const MediaFilter(favoritesOnly: true),
        onChanged: (f) => emitted = f,
      );

      await tester.tap(find.byKey(const Key('favorites_toggle')));
      await tester.pumpAndSettle();

      expect(emitted!.favoritesOnly, isFalse);
    });
  });

  // --------------------------------------------------------------------------
  // Sort dropdown
  // --------------------------------------------------------------------------

  group('sort dropdown', () {
    testWidgets('selecting Name sort emits sortBy = "name"', (tester) async {
      MediaFilter? emitted;
      await _pumpBar(tester, onChanged: (f) => emitted = f);

      // Open the popup menu.
      await tester.tap(find.byKey(const Key('sort_dropdown')));
      await tester.pumpAndSettle();

      // Tap the Name option.
      await tester.tap(find.byKey(const Key('sort_option_name')));
      await tester.pumpAndSettle();

      expect(emitted!.sortBy, equals('name'));
    });

    testWidgets('selecting Date sort emits sortBy = "date"', (tester) async {
      MediaFilter? emitted;
      await _pumpBar(tester, onChanged: (f) => emitted = f);

      await tester.tap(find.byKey(const Key('sort_dropdown')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('sort_option_date')));
      await tester.pumpAndSettle();

      expect(emitted!.sortBy, equals('date'));
    });

    testWidgets('selecting Random sort emits sortBy = "random"',
        (tester) async {
      MediaFilter? emitted;
      await _pumpBar(tester, onChanged: (f) => emitted = f);

      await tester.tap(find.byKey(const Key('sort_dropdown')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('sort_option_random')));
      await tester.pumpAndSettle();

      expect(emitted!.sortBy, equals('random'));
    });

    testWidgets('selecting Default clears sortBy to null', (tester) async {
      MediaFilter? emitted;
      await _pumpBar(
        tester,
        initialFilter: const MediaFilter(sortBy: 'name'),
        onChanged: (f) => emitted = f,
      );

      await tester.tap(find.byKey(const Key('sort_dropdown')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('sort_option_default')));
      await tester.pumpAndSettle();

      expect(emitted!.sortBy, isNull);
    });
  });

  // --------------------------------------------------------------------------
  // Initial filter reflected in UI
  // --------------------------------------------------------------------------

  group('initial filter state', () {
    testWidgets('initial query is shown in the search field', (tester) async {
      await _pumpBar(
        tester,
        initialFilter: const MediaFilter(query: 'test query'),
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
    testWidgets(
        'favourites star updates when initialFilter is changed externally',
        (tester) async {
      final key = await _pumpControlledBar(
        tester,
        initialFilter: const MediaFilter(favoritesOnly: false),
        onChanged: (_) {},
      );
      await tester.pumpAndSettle();

      // Star should initially be the outlined (inactive) variant.
      expect(
        find.descendant(
          of: find.byKey(const Key('favorites_toggle')),
          matching: find.byIcon(Icons.star_border),
        ),
        findsOneWidget,
      );

      // Push a new filter with favoritesOnly = true from outside.
      key.currentState!.pushFilter(const MediaFilter(favoritesOnly: true));
      await tester.pumpAndSettle();

      // Star should now be filled (active).
      expect(
        find.descendant(
          of: find.byKey(const Key('favorites_toggle')),
          matching: find.byIcon(Icons.star),
        ),
        findsOneWidget,
      );
    });

    testWidgets('search text updates when query is changed externally',
        (tester) async {
      final key = await _pumpControlledBar(
        tester,
        initialFilter: const MediaFilter(query: 'original'),
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
      key.currentState!.pushFilter(const MediaFilter());
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
}
