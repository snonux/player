import 'package:flutter/material.dart';

import '../models/music_metadata.dart';

/// Selectable embedded metadata shared by server details and saved downloads.
class MusicMetadataDetails extends StatelessWidget {
  const MusicMetadataDetails(
      {super.key, required this.music, this.filename = '', this.path = ''});
  final MusicMetadata? music;
  final String filename;
  final String path;

  @override
  Widget build(BuildContext context) {
    final basic = music?.details ?? const <String, String>{};
    final groups =
        music?.detailGroups ?? const <String, List<MapEntry<String, String>>>{};
    Widget row(String label, String value) => Padding(
        padding: const EdgeInsets.only(bottom: 8),
        child: Align(
            alignment: Alignment.centerLeft,
            child: SelectableText('$label: $value')));
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      if (filename.isNotEmpty) row('Filename', filename),
      if (path.isNotEmpty) row('Relative path', path),
      if (basic.isEmpty && groups.isEmpty)
        const Text('No embedded music metadata saved.'),
      for (final entry in basic.entries) row(entry.key, entry.value),
      for (final group in groups.entries) ...[
        Padding(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: Semantics(
                header: true,
                child: Text(group.key,
                    style: Theme.of(context).textTheme.titleSmall))),
        for (final entry in group.value) row(entry.key, entry.value),
      ],
    ]);
  }
}
