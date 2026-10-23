import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/media.dart';
import '../services/music_queue.dart';
import 'server_song_actions.dart';

/// An explained local ranking, using the same queue actor as artist playback.
class MusicTopSongsSection extends ConsumerWidget {
  const MusicTopSongsSection(
      {super.key, required this.ranking, required this.artist});
  final Map<String, dynamic>? ranking;
  final String artist;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final source = ranking?['source'] as String? ?? 'none';
    final songs = (ranking?['songs'] as List? ?? [])
        .map((song) => Media.fromJson(song as Map<String, dynamic>))
        .toList();
    final sourceLabel = switch (source) {
      'personal' => 'From your library · your most-played credited songs',
      'global' =>
        'From your library · server-wide plays (no personal plays yet)',
      _ => 'Local ranking · no external popularity provider',
    };
    Future<void> play(int index) => insertServerSongs(
        context, ref, songs.sublist(index), QueueInsertion.now,
        replace: true, sourceLabel: '$artist · Top songs');
    return Padding(
        padding: const EdgeInsets.symmetric(vertical: 16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Semantics(
              header: true,
              child: Text('Top songs',
                  style: Theme.of(context).textTheme.titleLarge)),
          Text(sourceLabel, style: Theme.of(context).textTheme.bodySmall),
          if (songs.isEmpty)
            const Text(
                'No top songs yet. Play this artist to build a local ranking. You can still use Play artist.')
          else ...[
            FilledButton.icon(
                onPressed: () => play(0),
                icon: const Icon(Icons.play_arrow),
                label: const Text('Play top songs')),
            for (var i = 0; i < songs.length; i++)
              ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: Text('${i + 1}'),
                  title: Text(songs[i].displayTitle),
                  subtitle: Text(
                      '${songs[i].music?.album ?? ''} · ${source == 'personal' ? songs[i].personalPlayCount : songs[i].playCount} ${source == 'personal' ? 'personal' : 'server-wide'} plays'),
                  trailing: IconButton(
                      tooltip: 'Play ${songs[i].displayTitle}',
                      icon: const Icon(Icons.play_arrow),
                      onPressed: () => play(i)),
                  onTap: () => play(i)),
          ],
        ]));
  }
}
