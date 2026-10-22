import 'package:flutter/material.dart';

import '../api/player_api_client.dart';
import '../models/models.dart';

/// Edits existing file-library filters as a draft. Dismissal never applies it.
class MediaFilterSheet extends StatefulWidget {
  const MediaFilterSheet({
    super.key,
    required this.initialFilter,
    required this.currentSetId,
    required this.currentSetName,
    required this.client,
    required this.onSetsLoaded,
  });

  final MediaFilter initialFilter;
  final int currentSetId;
  final String currentSetName;
  final PlayerApiClient client;
  final ValueChanged<List<MediaSet>> onSetsLoaded;

  @override
  State<MediaFilterSheet> createState() => _MediaFilterSheetState();
}

class _MediaFilterSheetState extends State<MediaFilterSheet> {
  late MediaFilter _draft;
  final _form = GlobalKey<FormState>();
  late final List<TextEditingController> _bounds;
  List<MediaSet>? _sets;
  List<Tag>? _tags;
  bool _loading = false;
  String? _optionError;

  @override
  void initState() {
    super.initState();
    _draft = widget.initialFilter;
    _bounds = List.generate(4, (_) => TextEditingController());
    _syncBounds();
    _loadOptions();
  }

  void _syncBounds() {
    final values = [
      _draft.minDuration == null ? '' : _number(_draft.minDuration! / 60),
      _draft.maxDuration == null ? '' : _number(_draft.maxDuration! / 60),
      _draft.fileSizeMin == null ? '' : _number(_draft.fileSizeMin! / 1048576),
      _draft.fileSizeMax == null ? '' : _number(_draft.fileSizeMax! / 1048576),
    ];
    for (var i = 0; i < values.length; i++) {
      _bounds[i].text = values[i];
    }
  }

  String _number(num value) => value == value.roundToDouble()
      ? value.toInt().toString()
      : value.toString();

  Future<void> _loadOptions() async {
    setState(() {
      _loading = true;
      _optionError = null;
    });
    try {
      final options = await Future.wait<Object>([
        widget.client.listSets(),
        widget.client.listTags(),
      ]);
      if (!mounted) return;
      widget.onSetsLoaded(options[0] as List<MediaSet>);
      setState(() {
        _sets = options[0] as List<MediaSet>;
        _tags = options[1] as List<Tag>;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _optionError =
            'Could not load sets and tags. Check your connection and retry.';
      });
    }
  }

  @override
  void dispose() {
    for (final controller in _bounds) {
      controller.dispose();
    }
    super.dispose();
  }

  Set<int> get _selectedSets =>
      (_draft.setIds ?? [widget.currentSetId]).toSet();

  void _toggleSet(int id, bool selected) {
    final ids = _selectedSets;
    selected ? ids.add(id) : ids.remove(id);
    setState(() => _draft = _draft.copyWith(
          setIds: ids.length == 1 && ids.first == widget.currentSetId
              ? null
              : ids.toList(),
        ));
  }

  void _clear() {
    setState(() {
      _form.currentState?.reset();
      _draft = MediaFilter(sortBy: _draft.sortBy);
      _syncBounds();
    });
  }

  String? _validate(String? raw, int index) {
    final text = (raw ?? '').trim();
    if (text.isEmpty) return null;
    if (index >= 2) {
      final value = int.tryParse(text);
      // Keep multiplication within the signed 64-bit server byte bound.
      if (!RegExp(r'^\d+$').hasMatch(text) ||
          value == null ||
          value > 8796093022207) {
        return 'Enter a whole number from 0 to 8796093022207';
      }
    } else {
      final value = double.tryParse(text);
      if (value == null ||
          !value.isFinite ||
          value < 0 ||
          !(value * 60).isFinite) {
        return 'Enter a finite number of minutes, 0 or more';
      }
    }
    if (index.isOdd) {
      final min = double.tryParse(_bounds[index - 1].text.trim());
      final max = double.tryParse(text);
      if (min != null && max != null && min > max) {
        return 'Maximum must be at least minimum';
      }
    }
    return null;
  }

  void _apply() {
    if (!_form.currentState!.validate()) return;
    double? minutes(int i) {
      final text = _bounds[i].text.trim();
      return text.isEmpty ? null : double.parse(text) * 60;
    }

    int? bytes(int i) {
      final text = _bounds[i].text.trim();
      return text.isEmpty ? null : int.parse(text) * 1048576;
    }

    Navigator.pop(
        context,
        _draft.copyWith(
          minDuration: minutes(0),
          maxDuration: minutes(1),
          fileSizeMin: bytes(2),
          fileSizeMax: bytes(3),
        ));
  }

  Widget _bound(int index, String label) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 6),
        child: TextFormField(
          key: Key('media_filter_bound_$index'),
          controller: _bounds[index],
          keyboardType: TextInputType.numberWithOptions(decimal: index < 2),
          decoration: InputDecoration(
              labelText: label, border: const OutlineInputBorder()),
          validator: (value) => _validate(value, index),
        ),
      );

  @override
  Widget build(BuildContext context) {
    final sets = <int, String>{
      widget.currentSetId: widget.currentSetName,
      for (final set in _sets ?? <MediaSet>[]) set.id: set.name,
      for (final id in _selectedSets)
        id: _sets?.where((s) => s.id == id).map((s) => s.name).firstOrNull ??
            (id == widget.currentSetId
                ? widget.currentSetName
                : 'Set $id (unavailable)'),
    };
    final tags = <String>{...?_tags?.map((t) => t.name), ..._draft.tags}
        .toList()
      ..sort();
    return SafeArea(
      child: Padding(
        padding:
            EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
        child: SizedBox(
          height: MediaQuery.sizeOf(context).height * 0.88,
          child: Column(children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 8, 0),
              child: Row(children: [
                Expanded(
                    child: Text('Filters',
                        style: Theme.of(context).textTheme.titleLarge)),
                IconButton(
                    tooltip: 'Close filters',
                    onPressed: () => Navigator.pop(context),
                    icon: const Icon(Icons.close)),
              ]),
            ),
            Expanded(
                child: Form(
                    key: _form,
                    child: ListView(
                      padding: const EdgeInsets.symmetric(
                          horizontal: 16, vertical: 8),
                      children: [
                        if (_loading)
                          const LinearProgressIndicator(
                              semanticsLabel: 'Loading filter options'),
                        if (_optionError != null) ...[
                          Text(_optionError!),
                          TextButton(
                              onPressed: _loadOptions,
                              child: const Text('Retry filter options')),
                        ],
                        Text('Sets',
                            style: Theme.of(context).textTheme.titleMedium),
                        const Text('No selection includes all visible sets.'),
                        for (final entry in sets.entries)
                          CheckboxListTile(
                            contentPadding: EdgeInsets.zero,
                            title: Text(entry.value),
                            value: _selectedSets.contains(entry.key),
                            onChanged: _loading || _optionError != null
                                ? null
                                : (value) =>
                                    _toggleSet(entry.key, value ?? false),
                          ),
                        const SizedBox(height: 12),
                        Text('Tags',
                            style: Theme.of(context).textTheme.titleMedium),
                        const Text('Matches every selected tag.'),
                        if (!_loading && _optionError == null && tags.isEmpty)
                          const Text('No tags yet'),
                        for (final tag in tags)
                          CheckboxListTile(
                            contentPadding: EdgeInsets.zero,
                            title: Text(tag),
                            value: _draft.tags.contains(tag),
                            onChanged: (selected) {
                              final next = _draft.tags.toSet();
                              selected == true
                                  ? next.add(tag)
                                  : next.remove(tag);
                              setState(() => _draft =
                                  _draft.copyWith(tags: next.toList()));
                            },
                          ),
                        const SizedBox(height: 12),
                        _bound(0, 'Minimum duration (minutes)'),
                        _bound(1, 'Maximum duration (minutes)'),
                        _bound(2, 'Minimum file size (MiB)'),
                        _bound(3, 'Maximum file size (MiB)'),
                        const Text(
                            'Bounds include their endpoints. 1 MiB = 1,048,576 bytes.'),
                      ],
                    ))),
            Padding(
              padding: const EdgeInsets.all(8),
              child: Wrap(alignment: WrapAlignment.end, spacing: 8, children: [
                TextButton(
                    onPressed: _clear, child: const Text('Clear filters')),
                TextButton(
                    onPressed: () => Navigator.pop(context),
                    child: const Text('Cancel')),
                FilledButton(onPressed: _apply, child: const Text('Apply')),
              ]),
            ),
          ]),
        ),
      ),
    );
  }
}
