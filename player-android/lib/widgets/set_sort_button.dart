import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';

final setSortValueProvider =
    StateProvider.family<String?, String>((ref, key) => null);

const setSortFields = <String, String>{
  'name': 'Name',
  'date': 'Date added',
  'play_count': 'Playbacks',
  'size': 'Size',
  'duration': 'Duration',
  'date_taken': 'Date taken',
  'random': 'Random',
};
String normalizeSetSort(String? value) => value == 'random' ||
        RegExp(r'^(name|date|play_count|size|duration|date_taken)_(asc|desc)$')
            .hasMatch(value ?? '')
    ? value!
    : 'name_asc';
String? setSortKey(WidgetRef ref, int setId) {
  final user = ref.read(authStateProvider).valueOrNull?.user?.id;
  if (user == null) return null;
  return 'set-sort:${ref.read(playerBaseUrlProvider)}:$user:$setId';
}

Future<String> readSetSort(WidgetRef ref, int setId) async {
  final key = setSortKey(ref, setId);
  if (key == null) return 'name_asc';
  final current = ref.read(setSortValueProvider(key));
  if (current != null) return current;
  try {
    return normalizeSetSort(
        (await SharedPreferences.getInstance()).getString(key));
  } catch (_) {
    return 'name_asc';
  }
}

Future<void> saveSetSort(WidgetRef ref, int setId, String value) async {
  final key = setSortKey(ref, setId);
  if (key != null) ref.read(setSortValueProvider(key).notifier).state = value;
  if (key != null &&
      !await (await SharedPreferences.getInstance()).setString(key, value)) {
    throw StateError('Could not save sort preference');
  }
}

class SetSortButton extends StatelessWidget {
  const SetSortButton(
      {super.key, required this.value, required this.onChanged});
  final String value;
  final ValueChanged<String> onChanged;
  @override
  Widget build(BuildContext context) => IconButton(
        tooltip: 'Sort',
        icon: const Icon(Icons.sort),
        style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
        onPressed: () => showModalBottomSheet<void>(
            context: context,
            showDragHandle: true,
            useSafeArea: true,
            isScrollControlled: true,
            builder: (sheet) {
              var selected = normalizeSetSort(value);
              return StatefulBuilder(builder: (context, update) {
                final index = selected.lastIndexOf('_');
                final field =
                    index < 0 ? selected : selected.substring(0, index);
                final descending = selected.endsWith('_desc');
                void change(String next) {
                  update(() => selected = next);
                  onChanged(next);
                }

                return SingleChildScrollView(
                    child: Padding(
                        padding: const EdgeInsets.all(16),
                        child: Column(
                            mainAxisSize: MainAxisSize.min,
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              Text('Sort this set',
                                  style:
                                      Theme.of(context).textTheme.titleLarge),
                              const Text(
                                  'Applies to this set and its folders. Folders first, alphabetical. Unknown metadata appears last.'),
                              DropdownButtonFormField<String>(
                                  initialValue: field,
                                  decoration: const InputDecoration(
                                      labelText: 'Order by'),
                                  items: [
                                    for (final entry in setSortFields.entries)
                                      DropdownMenuItem(
                                          value: entry.key,
                                          child: Text(entry.value))
                                  ],
                                  onChanged: (next) {
                                    if (next != null) {
                                      change(next == 'random'
                                          ? next
                                          : '${next}_${descending ? 'desc' : 'asc'}');
                                    }
                                  }),
                              SwitchListTile(
                                  title: const Text('Descending'),
                                  value: descending,
                                  onChanged: field == 'random'
                                      ? null
                                      : (next) => change(
                                          '${field}_${next ? 'desc' : 'asc'}')),
                              TextButton(
                                  onPressed: () => Navigator.pop(sheet),
                                  child: const Text('Done')),
                            ])));
              });
            }),
      );
}
