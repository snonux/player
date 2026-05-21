import 'dart:async';

import 'package:flutter/material.dart';

import '../models/media_filter.dart';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/// Delay between the last keystroke and the [onFiltersChanged] callback for
/// the text-search field.  400 ms balances responsiveness against unnecessary
/// in-flight API calls on every keypress.
const _kDebounceDelay = Duration(milliseconds: 400);

/// Internal sentinel used as the [PopupMenuButton] item value for the
/// "Default" (no sort) option.
///
/// [PopupMenuButton.onSelected] is only invoked for non-null values, so we
/// use a non-null sentinel string and convert back to `null` inside
/// [_SortDropdown.build].  The sentinel is kept private to this file so it
/// cannot leak into the public [MediaFilter] model.
const _kSortDefault = '_sort_default_';

/// Sort options shown in the dropdown, each mapped to its server-side value.
///
/// Keeping the list here (not inside the widget class) makes it easy to unit-
/// test the labels independently and avoids rebuilding the list on every
/// [build] call (stateless constant).
const List<({String label, String? value})> _kSortOptions = [
  (label: 'Default', value: null),
  (label: 'Name', value: 'name'),
  (label: 'Date', value: 'date'),
  (label: 'Duration', value: 'duration'),
  (label: 'Play count', value: 'play_count'),
  (label: 'Random', value: 'random'),
];

/// Type-filter chips shown below the search field.
///
/// The `null` value represents "All" (no type filter).
const List<({String label, String? value})> _kTypeOptions = [
  (label: 'All', value: null),
  (label: 'Video', value: 'video'),
  (label: 'Audio', value: 'audio'),
  (label: 'Image', value: 'image'),
];

// ---------------------------------------------------------------------------
// SearchFilterBar
// ---------------------------------------------------------------------------

/// A composite filter bar that combines a debounced text search field,
/// media-type filter chips, a favourites toggle, and a sort dropdown.
///
/// The widget is intentionally a pure `StatefulWidget` (not a
/// `ConsumerWidget`) — it owns only local UI state (text controller, timer)
/// and delegates all business logic to the [onFiltersChanged] callback.  This
/// satisfies the Single-Responsibility Principle: the bar manages its own UI
/// state, and the caller (e.g. [MediaGridScreen]) owns the data-fetching logic.
///
/// Usage:
/// ```dart
/// SearchFilterBar(
///   initialFilter: _filter,
///   onFiltersChanged: (filter) => setState(() => _filter = filter),
/// )
/// ```
///
/// Key design choices:
///   - Debounce is implemented with a [Timer] that is cancelled on every
///     keystroke; this is the canonical Flutter pattern and avoids pulling in
///     any third-party stream or debounce library.
///   - [_debounceTimer] is always cancelled in [dispose] to prevent callbacks
///     firing after the widget is unmounted.
///   - Type-filter chips and the sort dropdown fire [onFiltersChanged]
///     synchronously (no debounce) because the user's intent is unambiguous
///     when they tap a chip or pick from a dropdown.
class SearchFilterBar extends StatefulWidget {
  /// The filter state to display when the bar is first built.
  ///
  /// Defaults to `const MediaFilter()` (no filters applied).
  final MediaFilter initialFilter;

  /// Called whenever any filter changes.
  ///
  /// The callee typically stores the new filter and re-fetches media.
  final void Function(MediaFilter filter) onFiltersChanged;

  const SearchFilterBar({
    super.key,
    this.initialFilter = const MediaFilter(),
    required this.onFiltersChanged,
  });

  @override
  State<SearchFilterBar> createState() => _SearchFilterBarState();
}

class _SearchFilterBarState extends State<SearchFilterBar> {
  // Current composite filter state — mutated incrementally by each UI control.
  late MediaFilter _filter;

  // Controller for the search [TextField].
  late final TextEditingController _searchController;

  // Cancels the previous timer when the user types before the delay expires.
  Timer? _debounceTimer;

  @override
  void initState() {
    super.initState();
    _filter = widget.initialFilter;
    _searchController = TextEditingController(text: _filter.query ?? '');
  }

  @override
  void dispose() {
    // Always cancel the timer to prevent a stale callback firing after
    // the widget is removed from the tree.
    _debounceTimer?.cancel();
    _searchController.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Internal mutators — each updates _filter and calls onFiltersChanged.
  // ---------------------------------------------------------------------------

  /// Called on every keystroke in the search field.
  ///
  /// Cancels the previous debounce timer (if any) and schedules a new one.
  /// Only the final keystroke within [_kDebounceDelay] propagates to the
  /// caller, reducing unnecessary API calls while the user is still typing.
  ///
  /// The timer callback includes a [mounted] guard: if the widget is disposed
  /// before the delay expires (e.g. the user navigates away), the callback
  /// is a no-op and [setState] is never called on a dead widget.
  void _onSearchChanged(String value) {
    _debounceTimer?.cancel();
    _debounceTimer = Timer(_kDebounceDelay, () {
      if (!mounted) return;
      // Trim once and reuse to avoid double computation.
      final trimmed = value.trim();
      final updated = _filter.copyWith(query: trimmed.isEmpty ? null : trimmed);
      _applyFilter(updated);
    });
  }

  /// Called when the user taps a type-filter chip.
  ///
  /// Fires synchronously — no debounce needed because the user's intent is
  /// unambiguous when tapping a chip.
  void _onTypeSelected(String? type) {
    _applyFilter(_filter.copyWith(type: type));
  }

  /// Called when the user toggles the favourites button.
  void _onFavoritesToggled() {
    _applyFilter(_filter.copyWith(favoritesOnly: !_filter.favoritesOnly));
  }

  /// Called when the user picks a sort option.
  void _onSortSelected(String? sortBy) {
    _applyFilter(_filter.copyWith(sortBy: sortBy));
  }

  /// Stores the new filter in local state and notifies the parent widget.
  void _applyFilter(MediaFilter updated) {
    setState(() => _filter = updated);
    widget.onFiltersChanged(updated);
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // Row 1: text search + favourites toggle + sort dropdown.
        _SearchRow(
          controller: _searchController,
          favoritesOnly: _filter.favoritesOnly,
          sortBy: _filter.sortBy,
          onSearchChanged: _onSearchChanged,
          onFavoritesToggled: _onFavoritesToggled,
          onSortSelected: _onSortSelected,
        ),
        // Row 2: media-type filter chips (All / Video / Audio / Image).
        _TypeFilterRow(
          selectedType: _filter.type,
          onTypeSelected: _onTypeSelected,
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// _SearchRow
// ---------------------------------------------------------------------------

/// Top row of the filter bar: search field, favourites toggle, sort dropdown.
///
/// Extracted to keep [_SearchFilterBarState.build] concise and to allow
/// independent widget tests for just the top row (Single Responsibility).
class _SearchRow extends StatelessWidget {
  const _SearchRow({
    required this.controller,
    required this.favoritesOnly,
    required this.sortBy,
    required this.onSearchChanged,
    required this.onFavoritesToggled,
    required this.onSortSelected,
  });

  final TextEditingController controller;
  final bool favoritesOnly;
  final String? sortBy;
  final ValueChanged<String> onSearchChanged;
  final VoidCallback onFavoritesToggled;
  final ValueChanged<String?> onSortSelected;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(12, 8, 8, 0),
      child: Row(
        children: [
          // Expands to fill the available width, pushing the icon buttons right.
          Expanded(
            child: TextField(
              key: const Key('search_input'),
              controller: controller,
              onChanged: onSearchChanged,
              decoration: const InputDecoration(
                hintText: 'Search media…',
                prefixIcon: Icon(Icons.search),
                border: OutlineInputBorder(),
                isDense: true,
                contentPadding: EdgeInsets.symmetric(
                  vertical: 8,
                  horizontal: 12,
                ),
              ),
            ),
          ),
          const SizedBox(width: 4),
          // Favourites toggle — filled star when active.
          IconButton(
            key: const Key('favorites_toggle'),
            tooltip: favoritesOnly ? 'All items' : 'Favourites only',
            icon: Icon(
              favoritesOnly ? Icons.star : Icons.star_border,
              color: favoritesOnly
                  ? Theme.of(context).colorScheme.primary
                  : null,
            ),
            onPressed: onFavoritesToggled,
          ),
          // Sort dropdown — a small icon button that opens a pop-up menu.
          _SortDropdown(
            sortBy: sortBy,
            onSortSelected: onSortSelected,
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _SortDropdown
// ---------------------------------------------------------------------------

/// Pop-up sort menu triggered by a leading sort icon.
///
/// Separated from [_SearchRow] so it can be tested in isolation and because
/// the pop-up-menu logic is non-trivial enough to deserve its own widget
/// (Single Responsibility).
///
/// Implementation note: [PopupMenuButton] only calls [onSelected] for
/// non-null return values, so this widget uses a non-nullable [String] type
/// with [_kSortDefault] as the sentinel for "no sort".  The sentinel is
/// converted back to `null` before calling [onSortSelected].
class _SortDropdown extends StatelessWidget {
  const _SortDropdown({
    required this.sortBy,
    required this.onSortSelected,
  });

  final String? sortBy;
  final ValueChanged<String?> onSortSelected;

  /// Returns the label for the currently active sort option, or "Sort" as
  /// a fallback when no sort is selected.
  String get _currentLabel =>
      _kSortOptions
          .where((o) => o.value == sortBy)
          .map((o) => o.label)
          .firstOrNull ??
      'Sort';

  /// Maps a [PopupMenuButton] selection (always non-null) back to the
  /// nullable [sortBy] value expected by [MediaFilter].
  void _handleSelected(String raw) {
    onSortSelected(raw == _kSortDefault ? null : raw);
  }

  @override
  Widget build(BuildContext context) {
    return PopupMenuButton<String>(
      key: const Key('sort_dropdown'),
      tooltip: 'Sort by',
      onSelected: _handleSelected,
      itemBuilder: (_) => _kSortOptions
          .map(
            (option) => PopupMenuItem<String>(
              key: Key('sort_option_${option.value ?? 'default'}'),
              // Use the sentinel for the null/"Default" option so that
              // onSelected is always called (Flutter skips null returns).
              value: option.value ?? _kSortDefault,
              child: Text(option.label),
            ),
          )
          .toList(),
      // Show the current sort label next to the icon so the user always
      // knows which order is active without opening the menu.
      // child must be last per sort_child_properties_last lint rule.
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.sort),
            const SizedBox(width: 4),
            Text(
              _currentLabel,
              key: const Key('sort_label'),
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _TypeFilterRow
// ---------------------------------------------------------------------------

/// Horizontally scrollable row of [ChoiceChip]s for media-type filtering.
///
/// Using [ChoiceChip] (single-select) rather than [FilterChip]
/// (multi-select) mirrors the API contract: `type` accepts exactly one value.
/// A horizontal [SingleChildScrollView] handles narrow screens without
/// truncating the chips.
class _TypeFilterRow extends StatelessWidget {
  const _TypeFilterRow({
    required this.selectedType,
    required this.onTypeSelected,
  });

  final String? selectedType;
  final ValueChanged<String?> onTypeSelected;

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      key: const Key('type_filter_row'),
      scrollDirection: Axis.horizontal,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      child: Row(
        children: _kTypeOptions
            .map(
              (option) => Padding(
                padding: const EdgeInsets.only(right: 8),
                child: ChoiceChip(
                  key: Key('type_chip_${option.value ?? 'all'}'),
                  label: Text(option.label),
                  selected: selectedType == option.value,
                  onSelected: (_) => onTypeSelected(option.value),
                ),
              ),
            )
            .toList(),
      ),
    );
  }
}
