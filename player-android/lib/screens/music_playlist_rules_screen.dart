import 'dart:convert';
import 'package:flutter/material.dart';
import '../l10n/music_localizations.dart';

const playlistRuleFields = <String, String>{
  'title': 'Title',
  'artist': 'Artist',
  'album': 'Album',
  'genre': 'Genre',
  'year': 'Year',
  'plays': 'Your plays',
  'rating': 'Your rating',
  'starred': 'Your favorite',
  'added': 'File added',
  'last_played': 'Your last play',
};
const _operators = <String, String>{
  'contains': 'contains',
  'is': 'is',
  'eq': 'equals',
  'gte': 'at least',
  'lte': 'at most',
  'before': 'before date (UTC)',
  'after': 'after date (UTC)',
  'within_days': 'within the last N days',
};
List<String> _ops(String field) {
  if (['title', 'artist', 'album', 'genre'].contains(field)) {
    return ['contains', 'is'];
  }
  if (field == 'starred') return ['is'];
  if (['added', 'last_played'].contains(field)) {
    return ['before', 'after', 'within_days'];
  }
  return ['eq', 'gte', 'lte'];
}

String playlistRulesSummary(BuildContext context, Map<String, dynamic>? rules) {
  if (rules == null) return context.tr('Manual playlist');
  final conditions = (rules['conditions'] as List)
      .map((c) => c['operator'] == 'within_days'
          ? '${context.tr(playlistRuleFields[c['field']]!)} ${context.tr('within the last {days} days', {
                  'days': c['value']
                })}'
          : '${context.tr(playlistRuleFields[c['field']] ?? c['field'])} ${context.tr(_operators[c['operator']] ?? c['operator'])} ${c['field'] == 'starred' ? context.tr(c['value'] == 'true' ? 'Yes' : 'No') : c['value']}')
      .join('; ');
  return context.tr(
      'Match {mode}: {conditions}. Order: {sort}, {direction}; up to {limit} songs.',
      {
        'mode': context.tr(rules['match'] == 'all' ? 'all' : 'any'),
        'conditions': conditions,
        'sort': context.tr(playlistRuleFields[rules['sort']]!),
        'direction': context
            .tr(rules['descending'] == true ? 'Descending' : 'Ascending'),
        'limit': rules['limit'],
      });
}

/// Touch-first rule editing; Back cancels and failed saves preserve the draft.
class MusicPlaylistRulesScreen extends StatefulWidget {
  const MusicPlaylistRulesScreen(
      {super.key,
      required this.rules,
      required this.save,
      this.enable = false});
  final Map<String, dynamic>? rules;
  final bool enable;
  final Future<void> Function(Map<String, dynamic>?) save;
  @override
  State<MusicPlaylistRulesScreen> createState() => _RulesState();
}

class _RulesState extends State<MusicPlaylistRulesScreen> {
  late Map<String, dynamic> _rules;
  late bool _enabled;
  bool _busy = false;
  String? _error;
  final _form = GlobalKey<FormState>();
  @override
  void initState() {
    super.initState();
    _enabled = widget.rules != null || widget.enable;
    _rules = jsonDecode(jsonEncode(widget.rules ??
        {
          'match': 'all',
          'conditions': [
            {'field': 'genre', 'operator': 'contains', 'value': ''}
          ],
          'sort': 'title',
          'descending': false,
          'limit': 100,
        })) as Map<String, dynamic>;
  }

  List<dynamic> get _conditions => _rules['conditions'] as List;
  Widget _select(String label, String value, Map<String, String> values,
          ValueChanged<String> change) =>
      DropdownButtonFormField<String>(
        key: ValueKey('$label-$value-${values.keys.join(",")}'),
        initialValue: value,
        isExpanded: true,
        decoration: InputDecoration(labelText: context.tr(label)),
        items: values.entries
            .map((e) => DropdownMenuItem(
                value: e.key,
                child:
                    Text(context.tr(e.value), overflow: TextOverflow.ellipsis)))
            .toList(),
        onChanged: _busy
            ? null
            : (value) {
                if (value != null) setState(() => change(value));
              },
      );
  Future<void> _save() async {
    if (_busy || (_enabled && !_form.currentState!.validate())) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.save(_enabled ? _rules : null);
      if (mounted) Navigator.of(context).pop(true);
    } catch (_) {
      if (mounted) {
        setState(() => _error = context
            .tr('Could not save rules. Check your connection and retry.'));
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar: AppBar(title: Text(context.tr('Smart playlist rules'))),
        body: Form(
            key: _form,
            child: ListView(padding: const EdgeInsets.all(16), children: [
              SwitchListTile(
                  title: Text(context.tr('Enable smart rules')),
                  value: _enabled,
                  onChanged: _busy
                      ? null
                      : (value) => setState(() => _enabled = value)),
              Text(context.tr(
                  'Your plays, favorites and ratings use your account. Shared smart playlists can show different songs to each viewer.')),
              const SizedBox(height: 12),
              Text(context.tr(
                  'Dates use UTC. File added means when the file entered this library.')),
              if (_enabled) ...[
                _select(
                    'Match',
                    _rules['match'] as String,
                    {'all': 'All rules', 'any': 'Any rule'},
                    (value) => _rules['match'] = value),
                for (var i = 0; i < _conditions.length; i++) _condition(i),
                TextButton.icon(
                    onPressed: _busy || _conditions.length >= 20
                        ? null
                        : () => setState(() => _conditions.add({
                              'field': 'genre',
                              'operator': 'contains',
                              'value': ''
                            })),
                    icon: const Icon(Icons.add),
                    label: Text(context.tr('Add rule'))),
                _select(
                    'Order by',
                    _rules['sort'] as String,
                    Map.fromEntries(playlistRuleFields.entries
                        .where((e) => !['genre', 'starred'].contains(e.key))),
                    (value) => _rules['sort'] = value),
                _select(
                    'Direction',
                    _rules['descending'] == true ? 'descending' : 'ascending',
                    {'ascending': 'Ascending', 'descending': 'Descending'},
                    (value) => _rules['descending'] = value == 'descending'),
                TextFormField(
                    initialValue: '${_rules['limit']}',
                    enabled: !_busy,
                    decoration:
                        InputDecoration(labelText: context.tr('Maximum songs')),
                    keyboardType: TextInputType.number,
                    onChanged: (value) {
                      _rules['limit'] = int.tryParse(value) ?? 0;
                      setState(() {});
                    },
                    validator: (value) {
                      final n = int.tryParse(value ?? '');
                      return n == null || n < 1 || n > 1000
                          ? context.tr('Choose 1 to 1000 songs.')
                          : null;
                    }),
              ],
              const SizedBox(height: 16),
              Text(playlistRulesSummary(context, _enabled ? _rules : null)),
              if (_error != null)
                Semantics(
                    liveRegion: true,
                    child: Text(_error!,
                        style: TextStyle(
                            color: Theme.of(context).colorScheme.error))),
              const SizedBox(height: 16),
              FilledButton(
                  onPressed: _busy ? null : _save,
                  child: Text(context.tr(_busy ? 'Saving…' : 'Save rules'))),
              TextButton(
                  onPressed: () => Navigator.of(context).pop(),
                  child: Text(context.tr('Cancel'))),
            ])),
      );
  Widget _dateValue(Map<String, dynamic> condition) => FormField<String>(
        key:
            ValueKey('${identityHashCode(condition)}-${condition['operator']}'),
        initialValue: condition['value'] as String,
        validator: (value) =>
            (value ?? '').isEmpty ? context.tr('Choose a date.') : null,
        builder: (field) =>
            Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          OutlinedButton.icon(
              icon: const Icon(Icons.calendar_today),
              label: Text((condition['value'] as String).isEmpty
                  ? context.tr('Choose date (UTC)')
                  : condition['value'] as String),
              onPressed: _busy
                  ? null
                  : () async {
                      final chosen = await showDatePicker(
                          context: context,
                          initialDate:
                              DateTime.tryParse(condition['value'] as String) ??
                                  DateTime.now(),
                          firstDate: DateTime(1),
                          lastDate: DateTime(9999, 12, 31));
                      if (chosen == null || !mounted) return;
                      final value =
                          '${chosen.year.toString().padLeft(4, '0')}-${chosen.month.toString().padLeft(2, '0')}-${chosen.day.toString().padLeft(2, '0')}';
                      field.didChange(value);
                      setState(() => condition['value'] = value);
                    }),
          if (field.hasError)
            Text(field.errorText!,
                style: TextStyle(color: Theme.of(context).colorScheme.error)),
        ]),
      );

  Widget _condition(int index) {
    final c = _conditions[index] as Map<String, dynamic>;
    final field = c['field'] as String;
    final date = ['added', 'last_played'].contains(field) &&
        c['operator'] != 'within_days';
    return Card(
        key: ObjectKey(c),
        child: Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(context.tr('Rule {number}', {'number': index + 1})),
                  _select('Field', field, playlistRuleFields, (value) {
                    c['field'] = value;
                    c['operator'] = _ops(value).first;
                    c['value'] = value == 'starred' ? 'true' : '';
                  }),
                  _select('Comparison', c['operator'] as String, {
                    for (final op in _ops(field)) op: _operators[op]!
                  }, (value) {
                    c['operator'] = value;
                    c['value'] = field == 'starred' ? 'true' : '';
                  }),
                  if (field == 'starred')
                    _select(
                        'Value',
                        c['value'] as String,
                        {'true': 'Yes', 'false': 'No'},
                        (value) => c['value'] = value)
                  else if (date)
                    _dateValue(c)
                  else
                    TextFormField(
                        key: ValueKey(
                            '${identityHashCode(c)}-$field-${c['operator']}'),
                        initialValue: c['value'] as String,
                        enabled: !_busy,
                        maxLength: 300,
                        decoration: InputDecoration(
                            labelText: context.tr('Value'),
                            hintText: date ? 'YYYY-MM-DD' : null),
                        keyboardType:
                            ['year', 'plays', 'rating'].contains(field) ||
                                    c['operator'] == 'within_days'
                                ? TextInputType.number
                                : TextInputType.text,
                        onChanged: (value) =>
                            setState(() => c['value'] = value),
                        validator: (value) {
                          if ((value ?? '').trim().isEmpty) {
                            return context.tr('Enter a rule value.');
                          }
                          if (date &&
                              !RegExp(r'^\d{4}-\d{2}-\d{2}$')
                                  .hasMatch(value!)) {
                            return context.tr('Use YYYY-MM-DD.');
                          }
                          return null;
                        }),
                  TextButton.icon(
                      onPressed: _busy || _conditions.length == 1
                          ? null
                          : () => setState(() => _conditions.removeAt(index)),
                      icon: const Icon(Icons.remove_circle_outline),
                      label: Text(context.tr('Remove rule'))),
                ])));
  }
}
