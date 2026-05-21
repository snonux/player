import 'package:flutter/material.dart';

import '../api/player_api_client.dart';
import '../utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// TagPicker
// ---------------------------------------------------------------------------

/// Interactive tag-management widget for a single media item.
///
/// Displays existing tags as chips with a delete button and provides an
/// autocomplete text field for adding new tags from the global tag list.
///
/// Design notes:
///   - Pure callback-based interface: [tags], [mediaId], and [client] are
///     injected so this widget is provider-free and independently testable
///     (Dependency Inversion, Single Responsibility).
///   - Optimistic UI: the tag list is updated immediately on user action;
///     the API call is awaited in the background, and the change is reverted
///     (with a SnackBar) if the call fails.
///   - A per-tag loading guard ([_loadingTags]) prevents concurrent operations
///     on the same tag while still allowing independent tags to be acted on.
///   - No `dio` import: errors are mapped by [tagErrorMessage] in
///     `error_mappers.dart` (Dependency Inversion Principle).
///   - [mounted] is checked after every `await` to prevent setState calls on
///     a disposed widget.
class TagPicker extends StatefulWidget {
  /// Creates a [TagPicker] for the given [mediaId].
  const TagPicker({
    super.key,
    required this.mediaId,
    required this.tags,
    required this.client,
  });

  /// The ID of the media item whose tags are being managed.
  final int mediaId;

  /// The initial list of tag strings attached to this media item.
  ///
  /// The widget maintains its own internal copy; the caller's list is not
  /// mutated and does not need to be updated after add/remove operations.
  final List<String> tags;

  /// The API client used to call [listTags], [addTag], and [removeTag].
  ///
  /// Injected rather than read from a provider so the widget has no Riverpod
  /// dependency and can be tested with a plain fake/stub (DIP, testability).
  final PlayerApiClient client;

  @override
  State<TagPicker> createState() => _TagPickerState();
}

class _TagPickerState extends State<TagPicker> {
  // Current list of tags for this media item; updated optimistically.
  late List<String> _tags;

  // Set of tag names currently being added or removed (prevents concurrent
  // duplicate operations on the same tag while allowing others to proceed).
  final Set<String> _loadingTags = {};

  // All known tag names across the library; populated once during [initState]
  // via [_fetchAllTags] to seed the autocomplete dropdown.
  List<String> _allTagNames = [];

  // True while the initial [listTags] call is in flight.
  bool _tagListLoading = false;

  @override
  void initState() {
    super.initState();
    // Take a mutable copy of the injected tag list so optimistic updates
    // do not mutate the caller's data.
    _tags = List<String>.from(widget.tags);
    _fetchAllTags();
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches the global tag list for autocomplete suggestions.
  ///
  /// Called once during [initState].  Failures are silently ignored so a
  /// network hiccup does not prevent the existing chips from rendering — the
  /// user can still delete tags even without autocomplete suggestions.
  Future<void> _fetchAllTags() async {
    if (!mounted) return;
    setState(() => _tagListLoading = true);
    try {
      final tagObjects = await widget.client.listTags();
      if (!mounted) return;
      setState(() {
        _allTagNames = tagObjects.map((t) => t.name).toList();
        _tagListLoading = false;
      });
    } catch (_) {
      // Non-fatal: autocomplete simply shows no suggestions on error.
      if (mounted) setState(() => _tagListLoading = false);
    }
  }

  // ---------------------------------------------------------------------------
  // Tag removal
  // ---------------------------------------------------------------------------

  /// Optimistically removes [tag] from the UI and calls [removeTag] on the
  /// server.  Reverts and shows a SnackBar if the API call fails.
  Future<void> _removeTag(String tag) async {
    // Guard: skip if already being acted on (prevents double-tap race).
    if (_loadingTags.contains(tag)) return;

    // Optimistic remove — update the chip list immediately so the UI feels
    // responsive without waiting for the network round-trip.
    setState(() {
      _tags.remove(tag);
      _loadingTags.add(tag);
    });

    try {
      await widget.client.removeTag(widget.mediaId, tag);
    } catch (e) {
      if (!mounted) return;
      // Revert the optimistic removal and surface the error to the user.
      setState(() => _tags.add(tag));
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(tagErrorMessage(e))),
      );
    } finally {
      if (mounted) setState(() => _loadingTags.remove(tag));
    }
  }

  // ---------------------------------------------------------------------------
  // Tag addition
  // ---------------------------------------------------------------------------

  /// Optimistically adds [tag] to the UI and calls [addTag] on the server.
  /// Reverts and shows a SnackBar if the API call fails.
  ///
  /// Duplicate and empty tags are silently ignored to keep the UI consistent
  /// with the server's deduplication behaviour.
  Future<void> _addTag(String tag) async {
    final trimmed = tag.trim();
    // Silently ignore empty input or tags that are already attached.
    if (trimmed.isEmpty || _tags.contains(trimmed)) return;
    // Guard: skip if this tag name is already being acted on.
    if (_loadingTags.contains(trimmed)) return;

    // Optimistic add — append the chip immediately.
    setState(() {
      _tags.add(trimmed);
      _loadingTags.add(trimmed);
    });

    try {
      await widget.client.addTag(widget.mediaId, trimmed);
    } catch (e) {
      if (!mounted) return;
      // Revert the optimistic addition and surface the error.
      setState(() => _tags.remove(trimmed));
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(tagErrorMessage(e))),
      );
    } finally {
      if (mounted) setState(() => _loadingTags.remove(trimmed));
    }
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // Existing tags as deletable chips.
        _TagChipRow(
          tags: _tags,
          loadingTags: _loadingTags,
          onDelete: _removeTag,
        ),

        const SizedBox(height: 8),

        // Autocomplete input for adding new tags.
        _TagAutocomplete(
          allTagNames: _allTagNames,
          isLoading: _tagListLoading,
          currentTags: _tags,
          onTagSelected: _addTag,
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// _TagChipRow
// ---------------------------------------------------------------------------

/// Wrapping row of deletable tag chips.
///
/// Each chip shows a ✕ delete button that calls [onDelete].  Tags listed in
/// [loadingTags] render as slightly translucent to indicate an in-flight
/// operation (without blocking any other chip's delete button).
class _TagChipRow extends StatelessWidget {
  const _TagChipRow({
    required this.tags,
    required this.loadingTags,
    required this.onDelete,
  });

  final List<String> tags;

  /// Names of tags currently being added or removed.
  final Set<String> loadingTags;

  /// Called with the tag name when the user taps the delete (✕) button.
  final void Function(String tag) onDelete;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      key: const Key('tag_picker_chips'),
      spacing: 8,
      runSpacing: 4,
      children: [
        for (final tag in tags)
          _DeletableTagChip(
            tag: tag,
            isLoading: loadingTags.contains(tag),
            onDelete: () => onDelete(tag),
          ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// _DeletableTagChip
// ---------------------------------------------------------------------------

/// Single deletable tag chip.
///
/// Shows [tag] with a trailing ✕ button.  The button is disabled while
/// [isLoading] is true to prevent duplicate concurrent operations.
class _DeletableTagChip extends StatelessWidget {
  const _DeletableTagChip({
    required this.tag,
    required this.isLoading,
    required this.onDelete,
  });

  final String tag;

  /// Whether a remove-tag API call is currently in flight for this chip.
  final bool isLoading;

  /// Called when the user taps the ✕ button.
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    return Opacity(
      // Dim the chip while a remove operation is in flight (visual feedback).
      opacity: isLoading ? 0.5 : 1.0,
      child: Chip(
        key: Key('tag_chip_$tag'),
        label: Text(tag),
        labelStyle: Theme.of(context).textTheme.labelSmall,
        padding: EdgeInsets.zero,
        visualDensity: VisualDensity.compact,
        // The delete icon triggers optimistic removal.
        deleteIcon: Icon(
          Icons.close,
          key: Key('tag_chip_delete_$tag'),
          size: 14,
        ),
        onDeleted: isLoading ? null : onDelete,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _TagAutocomplete
// ---------------------------------------------------------------------------

/// Text field with autocomplete suggestions for adding new tags.
///
/// Suggestions come from [allTagNames] minus tags already in [currentTags] so
/// the user only sees tags that can actually be added.  Selecting a suggestion
/// or submitting the field calls [onTagSelected] and clears the input.
///
/// This is a [StatelessWidget] because [Autocomplete] manages its own
/// internal text controller and focus node — no local mutable state is needed.
class _TagAutocomplete extends StatelessWidget {
  const _TagAutocomplete({
    required this.allTagNames,
    required this.isLoading,
    required this.currentTags,
    required this.onTagSelected,
  });

  /// The full list of tag names available globally (from listTags).
  final List<String> allTagNames;

  /// True while [allTagNames] is still being fetched from the server.
  final bool isLoading;

  /// Tags already attached to this item; excluded from autocomplete suggestions.
  final List<String> currentTags;

  /// Called with the confirmed tag name when the user selects or submits.
  final Future<void> Function(String tag) onTagSelected;

  @override
  Widget build(BuildContext context) {
    return Autocomplete<String>(
      optionsBuilder: (editing) {
        // Return an empty iterable when the field is blank to avoid showing
        // the full list unprompted (avoids overwhelming the user).
        final query = editing.text.trim().toLowerCase();
        if (query.isEmpty) return const Iterable<String>.empty();

        // Filter suggestions: match by query substring and exclude
        // already-applied tags so only addable tags are suggested.
        return allTagNames
            .where((name) => !currentTags.contains(name))
            .where((name) => name.toLowerCase().contains(query));
      },
      onSelected: (selected) {
        // Autocomplete automatically clears the field text after onSelected,
        // so we only need to trigger our own add-tag callback.
        onTagSelected(selected);
      },
      fieldViewBuilder: (context, textController, focusNode, onFieldSubmitted) {
        // The Autocomplete widget manages textController and focusNode; we
        // read from textController in the suffix button's onPressed so the
        // manually typed text is submitted with the same controller.
        return TextField(
          key: const Key('tag_add_input'),
          controller: textController,
          focusNode: focusNode,
          decoration: InputDecoration(
            hintText: isLoading ? 'Loading tags…' : 'Add a tag…',
            isDense: true,
            suffixIcon: IconButton(
              key: const Key('tag_add_button'),
              icon: const Icon(Icons.add, size: 18),
              tooltip: 'Add tag',
              // Submit using the Autocomplete-managed controller text.
              onPressed: () {
                final text = textController.text.trim();
                if (text.isEmpty) return;
                textController.clear();
                onTagSelected(text);
              },
            ),
            border: const OutlineInputBorder(),
            contentPadding:
                const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          ),
          onSubmitted: (value) {
            // onFieldSubmitted closes the Autocomplete overlay (keyboard Enter).
            // When the text does NOT match any highlighted suggestion the
            // Autocomplete's onSelected fires only for the highlighted item, so
            // we additionally submit the raw typed text here so the user can
            // enter a brand-new tag name without selecting from the list.
            final trimmed = value.trim();
            if (trimmed.isNotEmpty) {
              textController.clear();
              onTagSelected(trimmed);
            }
            onFieldSubmitted();
          },
        );
      },
    );
  }
}
