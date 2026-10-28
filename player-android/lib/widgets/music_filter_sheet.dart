import '../l10n/music_localizations.dart';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';

const musicFacetLabels = {
  'genre': 'Genre',
  'year': 'Year / decade',
  'role': 'Role',
  'release_type': 'Release type',
  'compilation': 'Compilation',
  'mood': 'Mood',
  'grouping': 'Grouping',
  'label': 'Label',
};
String musicFacetValue(String key, String value) => key == 'compilation'
    ? value == 'true'
        ? 'Marked'
        : 'Not marked'
    : const {
          'track_artist': 'Track artist',
          'album_artist': 'Album artist',
          'composer': 'Composer',
          'performer': 'Performer',
          'artists': 'Artists (track and album)'
        }[value] ??
        value;

/// Account-scoped applied facets; the sheet edits a draft until Apply.
class MusicFilterBar extends ConsumerStatefulWidget
    implements PreferredSizeWidget {
  const MusicFilterBar(
      {super.key, required this.onChanged, this.trailing = const []});
  final ValueChanged<Map<String, String>> onChanged;

  /// Extra controls in the same horizontal filter strip (e.g. result groups).
  final List<Widget> trailing;
  @override
  Size get preferredSize => const Size.fromHeight(48);
  @override
  ConsumerState<MusicFilterBar> createState() => _MusicFilterBarState();
}

class _MusicFilterBarState extends ConsumerState<MusicFilterBar> {
  Map<String, String> _filter = {};
  String? _key;
  int _generation = 0;
  @override
  void initState() {
    super.initState();
    Future.microtask(_restore);
  }

  String get _scope =>
      'music_facets:${ref.read(playerBaseUrlProvider).origin}:${ref.read(authStateProvider).valueOrNull?.user?.id}';
  Future<void> _restore() async {
    final key = _scope, generation = ++_generation;
    _key = key;
    Map<String, String> value = {};
    try {
      final raw = (await SharedPreferences.getInstance()).getString(key);
      if (raw != null) value = Map<String, String>.from(jsonDecode(raw) as Map);
    } catch (_) {/* A malformed local preference safely restores no filters. */}
    if (!mounted || generation != _generation || key != _scope) return;
    setState(() => _filter = value);
    widget.onChanged(Map.of(value));
  }

  Future<void> _apply(Map<String, String> value) async {
    final key = _scope;
    setState(() => _filter = value);
    widget.onChanged(Map.of(value));
    try {
      if (!await (await SharedPreferences.getInstance())
          .setString(key, jsonEncode(value))) {
        throw StateError('save failed');
      }
    } catch (_) {
      if (mounted && key == _scope) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content: Text('Could not save music filters on this device.')));
      }
    }
  }

  Future<void> _open() async {
    final key = _scope;
    final value = await showModalBottomSheet<Map<String, String>>(
        context: context,
        isScrollControlled: true,
        useSafeArea: true,
        builder: (context) => _MusicFilterSheet(initial: _filter));
    if (mounted && key == _scope && value != null) await _apply(value);
  }

  @override
  Widget build(BuildContext context) {
    ref.watch(playerBaseUrlProvider);
    ref.watch(authStateProvider);
    if (_key != _scope) Future.microtask(_restore);
    return SizedBox(
      height: 48,
      child: SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: Row(children: [
            TextButton.icon(
                onPressed: _open,
                icon: const Icon(Icons.filter_list),
                label: Text('Filters (${_filter.length})')),
            for (final child in widget.trailing)
              Padding(padding: const EdgeInsets.only(left: 8), child: child),
            for (final entry in _filter.entries)
              Padding(
                  padding: const EdgeInsets.only(left: 8),
                  child: InputChip(
                      label: Text(
                          '${musicFacetLabels[entry.key]}: ${musicFacetValue(entry.key, entry.value)}'),
                      onDeleted: () =>
                          _apply(Map.of(_filter)..remove(entry.key)))),
            if (_filter.isNotEmpty)
              TextButton(
                  onPressed: () => _apply({}),
                  child: Text(context.tr('Clear filters'))),
          ])),
    );
  }
}

class _MusicFilterSheet extends ConsumerStatefulWidget {
  const _MusicFilterSheet({required this.initial});
  final Map<String, String> initial;
  @override
  ConsumerState<_MusicFilterSheet> createState() => _MusicFilterSheetState();
}

class _MusicFilterSheetState extends ConsumerState<_MusicFilterSheet> {
  late Map<String, String> _draft;
  Map<String, dynamic>? _options;
  bool _failed = false;
  @override
  void initState() {
    super.initState();
    _draft = Map.of(widget.initial);
    Future.microtask(_load);
  }

  Future<void> _load() async {
    setState(() => _failed = false);
    try {
      final options = await ref.read(apiClientProvider).musicFacets();
      if (mounted) setState(() => _options = options);
    } catch (_) {
      if (mounted) setState(() => _failed = true);
    }
  }

  @override
  Widget build(BuildContext context) => Padding(
      padding: EdgeInsets.fromLTRB(
          16, 16, 16, 16 + MediaQuery.viewInsetsOf(context).bottom),
      child: SizedBox(
          height: MediaQuery.sizeOf(context).height * .75,
          child: Column(children: [
            Text(context.tr('Music filters'),
                style: Theme.of(context).textTheme.titleLarge),
            Expanded(
                child: _failed
                    ? Column(children: [
                        const Text(
                            'Could not load filters. Check your connection.'),
                        TextButton(
                            onPressed: _load, child: Text(context.tr('Retry')))
                      ])
                    : _options == null
                        ? const Center(child: CircularProgressIndicator())
                        : ListView(children: [
                            for (final entry in musicFacetLabels.entries)
                              if ((_options![entry.key] as List? ?? [])
                                      .isNotEmpty ||
                                  _draft.containsKey(entry.key))
                                Padding(
                                    padding:
                                        const EdgeInsets.symmetric(vertical: 8),
                                    child: _field(entry.key, entry.value)),
                          ])),
            Row(mainAxisAlignment: MainAxisAlignment.end, children: [
              TextButton(
                  onPressed: () => setState(() => _draft = {}),
                  child: Text(context.tr('Clear'))),
              const SizedBox(width: 16),
              FilledButton(
                  onPressed: () => Navigator.pop(context, _draft),
                  child: Text(context.tr('Apply')))
            ]),
          ])));
  Widget _field(String key, String label) {
    final choices = (_options![key] as List? ?? []).whereType<String>().toSet();
    if (key == 'year') {
      choices.addAll(choices.toList().map((v) {
        final start = (int.tryParse(v) ?? 0) ~/ 10 * 10;
        return '$start-${start + 9}';
      }));
    }
    if (key == 'compilation') {
      choices.clear();
      choices.addAll(['true', 'false']);
    }
    if (_draft[key] != null) choices.add(_draft[key]!);
    return DropdownButtonFormField<String>(
        key: ValueKey('$key-${_draft[key]}'),
        initialValue: _draft[key] ?? '',
        isExpanded: true,
        decoration: InputDecoration(labelText: label),
        items: [
          const DropdownMenuItem(value: '', child: Text('Any')),
          for (final value in choices)
            DropdownMenuItem(
                value: value,
                child: Text(context.tr(musicFacetValue(key, value)),
                    overflow: TextOverflow.ellipsis))
        ],
        onChanged: (value) => setState(() {
              if (value == null || value.isEmpty) {
                _draft.remove(key);
              } else {
                _draft[key] = value;
              }
            }));
  }
}
