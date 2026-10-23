import 'package:flutter/material.dart';

String musicListeningText(Map<String, dynamic> item) {
  final count = item['personal_play_count'] as int? ?? 0;
  final date =
      DateTime.tryParse(item['last_played_at'] as String? ?? '')?.toLocal();
  final played = date == null ? 'Not played yet' : 'Last played $date';
  return 'Your plays: $count · $played';
}

/// Secondary listening information, using the surrounding theme and wrapping.
class MusicListeningDetails extends StatelessWidget {
  const MusicListeningDetails(this.item, {super.key});
  final Map<String, dynamic> item;
  @override
  Widget build(BuildContext context) => Text(musicListeningText(item),
      style: Theme.of(context).textTheme.bodySmall);
}
