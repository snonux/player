import 'music_discovery.dart';

/// Only collection keys and opaque playlist IDs are persisted, never private names.
class MusicHome {
  MusicHome([Map<String, dynamic>? saved]) {
    final keys = [...albumCollections.keys, 'pinned'];
    order = [
      ...{
        for (final key
            in saved?['order'] is List ? saved!['order'] as List : [])
          if (keys.contains(key)) key as String
      },
    ];
    order.addAll(keys.where((key) => !order.contains(key)));
    hidden = {
      for (final key
          in saved?['hidden'] is List ? saved!['hidden'] as List : [])
        if (keys.contains(key)) key as String
    };
    pins = {
      for (final id in saved?['pins'] is List ? saved!['pins'] as List : [])
        if (id is String && id.isNotEmpty && id.length < 200) id
    }.toList();
  }
  late List<String> order, pins;
  late Set<String> hidden;
  Map<String, dynamic> toJson() =>
      {'order': order, 'hidden': hidden.toList(), 'pins': pins};
}
