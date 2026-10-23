import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';

const musicColumns = {
  'albums': {
    'rating': 'Rating',
    'artist': 'Artist',
    'year': 'Year',
    'song_count': 'Songs',
    'duration': 'Duration'
  },
  'songs': {
    'rating': 'Rating',
    'artist': 'Artist',
    'album': 'Album',
    'year': 'Year',
    'duration': 'Duration'
  },
};

class MusicLayout {
  MusicLayout(this.kind, [Map<String, dynamic>? saved])
      : table = saved?['view'] == 'table',
        descending = saved?['descending'] == true,
        sort = ['title', ...musicColumns[kind]!.keys].contains(saved?['sort'])
            ? saved!['sort'] as String
            : 'title',
        columns = musicColumns[kind]!
            .keys
            .where((key) => (saved?['columns'] is List
                    ? saved!['columns'] as List
                    : kind == 'albums'
                        ? ['artist', 'year']
                        : ['artist', 'album', 'duration'])
                .contains(key))
            .toList();
  final String kind;
  bool table, descending;
  String sort;
  List<String> columns;
  Map<String, dynamic> toJson() => {
        'view': table ? 'table' : 'grid',
        'descending': descending,
        'sort': sort,
        'columns': columns
      };
  dynamic value(Map<String, dynamic> item, String column) {
    if (kind == 'albums' || column == 'rating') return item[column];
    final music = item['music'] as Map? ?? {};
    return column == 'duration'
        ? item[column]
        : column == 'title'
            ? music[column] ?? item['file_name']
            : music[column];
  }

  String text(Map<String, dynamic> item, String column) {
    final v = value(item, column);
    if (column == 'duration') {
      final seconds = (v as num? ?? 0).round();
      return '${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')}';
    }
    return v == null || v == '' || (column == 'year' && v == 0) ? '—' : '$v';
  }

  List<Map<String, dynamic>> sorted(List<Map<String, dynamic>> items) {
    bool missing(dynamic value) =>
        value == null || value == '' || (sort == 'year' && value == 0);
    return [...items]..sort((a, b) {
        final av = value(a, sort), bv = value(b, sort);
        if (missing(av) != missing(bv)) return missing(av) ? 1 : -1;
        final order = av is num && bv is num
            ? av.compareTo(bv)
            : '${av ?? ''}'
                .toLowerCase()
                .compareTo('${bv ?? ''}'.toLowerCase());
        return order != 0
            ? (descending ? -order : order)
            : '${a['album_key'] ?? a['id']}'
                .compareTo('${b['album_key'] ?? b['id']}');
      });
  }

  String subtitle(Map<String, dynamic> item) => columns
      .map((column) => '${musicColumns[kind]![column]}: ${text(item, column)}')
      .join(' · ');
}

/// Device-local preferences remain separate for each primary server and account.
class MusicLayoutPanel extends ConsumerStatefulWidget {
  const MusicLayoutPanel(
      {super.key, required this.kind, required this.builder});
  final String kind;
  final Widget Function(MusicLayout layout) builder;
  @override
  ConsumerState<MusicLayoutPanel> createState() => _MusicLayoutPanelState();
}

class _MusicLayoutPanelState extends ConsumerState<MusicLayoutPanel> {
  String? _scope;
  MusicLayout? _layout;
  Future<void> _writes = Future.value();
  Future<void> _load(String scope) async {
    MusicLayout layout;
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString(scope);
      layout = MusicLayout(
          widget.kind,
          raw == null
              ? null
              : Map<String, dynamic>.from(jsonDecode(raw) as Map));
    } catch (_) {
      layout = MusicLayout(widget.kind);
    }
    if (mounted && _scope == scope) setState(() => _layout = layout);
  }

  void _update(VoidCallback change) {
    setState(change);
    final scope = _scope!, raw = jsonEncode(_layout!.toJson());
    _writes = _writes.then((_) async {
      try {
        final prefs = await SharedPreferences.getInstance();
        if (!await prefs.setString(scope, raw)) {
          throw StateError('Storage unavailable');
        }
      } catch (_) {
        if (mounted && _scope == scope) {
          ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
              content: Text('Could not remember music layout.')));
        }
      }
    });
  }

  Future<void> _columns() async {
    final layout = _layout!, scope = _scope;
    final selected = [...layout.columns];
    final result = await showDialog<List<String>>(
        context: context,
        builder: (context) => StatefulBuilder(
            builder: (context, update) => AlertDialog(
                  title: const Text('Music columns'),
                  content: SingleChildScrollView(
                      child: Column(mainAxisSize: MainAxisSize.min, children: [
                    const Text('Title is always shown'),
                    for (final entry in musicColumns[widget.kind]!.entries)
                      CheckboxListTile(
                          title: Text(entry.value),
                          value: selected.contains(entry.key),
                          onChanged: (checked) => update(() {
                                if (checked == true) {
                                  selected.add(entry.key);
                                } else {
                                  selected.remove(entry.key);
                                }
                              })),
                  ])),
                  actions: [
                    TextButton(
                        onPressed: () => Navigator.pop(context),
                        child: const Text('Cancel')),
                    FilledButton(
                        onPressed: () => Navigator.pop(context, selected),
                        child: const Text('Apply'))
                  ],
                )));
    if (mounted && scope == _scope && result != null) {
      _update(() => layout.columns = result);
    }
  }

  @override
  Widget build(BuildContext context) {
    final user = ref.watch(authStateProvider).valueOrNull?.user;
    final origin = ref.watch(playerBaseUrlProvider).origin;
    final scope = 'player.music-layout.v1:$origin:${user?.id}:${widget.kind}';
    if (_scope != scope) {
      _scope = scope;
      _layout = null;
      Future.microtask(() => _load(scope));
    }
    final layout = _layout;
    if (layout == null) {
      return const Center(
          child: CircularProgressIndicator(
              semanticsLabel: 'Loading music layout'));
    }
    return Column(children: [
      Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
          child: Wrap(
              spacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                DropdownButton<String>(
                    value: layout.sort,
                    hint: const Text('Sort by'),
                    items: {'title': 'Title', ...musicColumns[widget.kind]!}
                        .entries
                        .map((e) => DropdownMenuItem(
                            value: e.key, child: Text('Sort: ${e.value}')))
                        .toList(),
                    onChanged: (value) {
                      if (value != null) _update(() => layout.sort = value);
                    }),
                TextButton.icon(
                    style:
                        TextButton.styleFrom(minimumSize: const Size(48, 48)),
                    onPressed: () =>
                        _update(() => layout.descending = !layout.descending),
                    icon: Icon(layout.descending
                        ? Icons.arrow_downward
                        : Icons.arrow_upward),
                    label:
                        Text(layout.descending ? 'Descending' : 'Ascending')),
                if (widget.kind == 'albums')
                  TextButton.icon(
                      style:
                          TextButton.styleFrom(minimumSize: const Size(48, 48)),
                      onPressed: () =>
                          _update(() => layout.table = !layout.table),
                      icon: Icon(
                          layout.table ? Icons.view_module : Icons.table_rows),
                      label: Text(layout.table ? 'View: Table' : 'View: Grid')),
                TextButton(
                    style:
                        TextButton.styleFrom(minimumSize: const Size(48, 48)),
                    onPressed: _columns,
                    child: const Text('Columns')),
              ])),
      Expanded(child: widget.builder(layout)),
    ]);
  }
}
