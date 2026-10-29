import 'dart:async';

import 'package:flutter/material.dart';

import '../models/media_filter.dart';
import 'media_filter_sheet.dart';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/// Delay between the last keystroke and the [onFiltersChanged] callback for
/// the text-search field.  400 ms balances responsiveness against unnecessary
/// in-flight API calls on every keypress.
const _kDebounceDelay = Duration(milliseconds: 400);

// ---------------------------------------------------------------------------
// SearchFilterBar
// ---------------------------------------------------------------------------

/// A single debounced search field. Its filter button opens the caller's
/// filter sheet (type, sort, sets, tags, bounds); a one-line summary with a
/// Clear action appears only while something narrows or orders the list.
/// [MediaFilter.favoritesOnly] is toggled from [MediaGridScreen]'s menu.
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
class SearchFilterBar extends StatefulWidget {
  /// The filter state to display when the bar is first built.
  ///
  /// Defaults to `const MediaFilter.empty()` (no filters applied).
  final MediaFilter initialFilter;

  /// Called whenever any filter changes.
  ///
  /// The callee typically stores the new filter and re-fetches media.
  final void Function(MediaFilter filter) onFiltersChanged;

  final Future<MediaFilter?> Function(MediaFilter)? onOpenFilters;
  final String? scopeLabel;
  final int resetRevision;

  const SearchFilterBar({
    super.key,
    this.initialFilter = const MediaFilter.empty(),
    required this.onFiltersChanged,
    this.onOpenFilters,
    this.scopeLabel,
    this.resetRevision = 0,
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

  /// Syncs internal filter state when the parent supplies a new [initialFilter].
  ///
  /// This handles the case where an external control (e.g. the app-bar favourites
  /// shortcut in [MediaGridScreen]) mutates the filter outside [SearchFilterBar]
  /// and rebuilds the widget with a different [initialFilter].  Without this
  /// override the bar would ignore the new value and show stale chips/icons.
  ///
  /// Only fields that actually changed are updated to avoid clearing the search
  /// text while the user is still typing (the debounce timer owns the pending
  /// query, so we leave [_searchController] alone unless the query changed).
  @override
  void didUpdateWidget(SearchFilterBar oldWidget) {
    super.didUpdateWidget(oldWidget);
    final newFilter = widget.initialFilter;
    if (oldWidget.resetRevision != widget.resetRevision) {
      _debounceTimer?.cancel();
      _searchController.text = newFilter.query ?? '';
    }
    if (newFilter == _filter) return; // nothing changed

    // Sync the internal filter state without calling onFiltersChanged —
    // the parent already holds the updated filter; we only need to reflect it.
    setState(() {
      _filter = newFilter;
    });

    // Sync the text field only when the query actually changed (avoid
    // overwriting text the user is currently editing via the debounce path).
    if (oldWidget.initialFilter.query != newFilter.query) {
      _searchController.text = newFilter.query ?? '';
    }
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

  /// Stores the new filter in local state and notifies the parent widget.
  void _applyFilter(MediaFilter updated) {
    setState(() => _filter = updated);
    widget.onFiltersChanged(updated);
  }

  Future<void> _openFilters() async {
    _debounceTimer?.cancel();
    final text = _searchController.text.trim();
    final latest = _filter.copyWith(query: text.isEmpty ? null : text);
    if (latest != _filter) _applyFilter(latest);
    final updated = await widget.onOpenFilters!(latest);
    if (mounted && updated != null) {
      _debounceTimer?.cancel();
      _searchController.text = updated.query ?? '';
      _applyFilter(updated);
    }
  }

  void _clearFilters() {
    _debounceTimer?.cancel();
    _searchController.clear();
    _applyFilter(MediaFilter(sortBy: _filter.sortBy));
  }

  /// What narrows or orders the list beyond the search text; empty when the
  /// list shows the whole current set in default order.
  String get _summary {
    String label(List<({String label, String? value})> options, String? v) =>
        options.where((o) => o.value == v).map((o) => o.label).firstOrNull ??
        v ??
        '';
    final labels = <String>[
      if (_filter.setIds != null && widget.scopeLabel != null)
        widget.scopeLabel!,
      if (_filter.type != null) label(mediaTypeOptions, _filter.type),
      if (_filter.favoritesOnly) 'Favourites',
      if (_filter.tags.isNotEmpty) 'Tags: ${_filter.tags.join(', ')}',
      if (_filter.minDuration != null)
        'Duration ≥ ${_filter.minDuration! / 60} min',
      if (_filter.maxDuration != null)
        'Duration ≤ ${_filter.maxDuration! / 60} min',
      if (_filter.fileSizeMin != null)
        'Size ≥ ${_filter.fileSizeMin! ~/ 1048576} MiB',
      if (_filter.fileSizeMax != null)
        'Size ≤ ${_filter.fileSizeMax! ~/ 1048576} MiB',
    ];
    return labels.join(' · ');
  }

  @override
  Widget build(BuildContext context) {
    final summary = _summary;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 8, 12, 4),
          child: TextField(
            key: const Key('search_input'),
            controller: _searchController,
            onChanged: _onSearchChanged,
            decoration: InputDecoration(
              hintText: 'Search media…',
              prefixIcon: const Icon(Icons.search),
              suffixIcon: widget.onOpenFilters == null
                  ? null
                  : IconButton(
                      key: const Key('media_filters_button'),
                      tooltip: 'Filters and sort',
                      onPressed: _openFilters,
                      icon: Badge(
                        isLabelVisible: summary.isNotEmpty,
                        child: const Icon(Icons.tune),
                      ),
                    ),
              border: const OutlineInputBorder(),
              isDense: true,
              contentPadding:
                  const EdgeInsets.symmetric(vertical: 8, horizontal: 12),
            ),
          ),
        ),
        if (summary.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(left: 16, right: 4),
            child: Row(children: [
              Expanded(
                child: Text(summary,
                    key: const Key('media_filter_summary'),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall),
              ),
              if (_filter.hasConstraints)
                TextButton(
                    key: const Key('media_filter_clear'),
                    onPressed: _clearFilters,
                    child: const Text('Clear')),
            ]),
          ),
      ],
    );
  }
}
