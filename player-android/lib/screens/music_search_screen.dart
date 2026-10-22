import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../app_routes.dart';
import '../models/media.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';
import '../widgets/music_mini_player.dart';
import '../widgets/music_queue_sheet.dart';
import '../widgets/server_song_actions.dart';
import '../services/music_queue.dart';

/// Search is server-backed; downloaded-library search belongs to Downloads.
class MusicSearchScreen extends ConsumerStatefulWidget {
  const MusicSearchScreen({super.key});
  @override
  ConsumerState<MusicSearchScreen> createState() => _MusicSearchScreenState();
}

/// Matches the web search's two metadata constraints without splitting names.
Map<String, String> musicSearchQuery(String raw) {
  final parts = {'q': <String>[], 'artist': <String>[], 'album': <String>[]};
  for (final match in RegExp(r'(?:[^\s"]+|"[^"]*")+').allMatches(raw)) {
    final token = match.group(0)!;
    final scoped = RegExp(r'^(artist|album):(.+)$', caseSensitive: false)
        .firstMatch(token);
    parts[scoped?.group(1)?.toLowerCase() ?? 'q']!
        .add((scoped?.group(2) ?? token).replaceAll('"', ''));
  }
  return parts.map((key, value) => MapEntry(key, value.join(' ').trim()));
}

class _MusicSearchScreenState extends ConsumerState<MusicSearchScreen> {
  final _text = TextEditingController();
  final _scroll = ScrollController();
  Timer? _timer;
  int _generation = 0;
  bool _loading = false;
  String? _error;
  String _group = 'All';
  Map<String, dynamic>? _results;

  @override
  void dispose() {
    _generation++;
    _timer?.cancel();
    _text.dispose();
    _scroll.dispose();
    super.dispose();
  }

  void _changed(String value) {
    _timer?.cancel();
    // Invalidate older responses immediately, not only after the debounce.
    _generation++;
    if (_scroll.hasClients) _scroll.jumpTo(0);
    setState(() {
      _results = null;
      _error = null;
      _loading = false;
    });
    if (_text.value.composing.isValid && !_text.value.composing.isCollapsed) {
      return;
    }
    _timer = Timer(const Duration(milliseconds: 300), _load);
  }

  Future<void> _load() async {
    _timer?.cancel();
    final query = musicSearchQuery(_text.text);
    final generation = ++_generation;
    if (query.values.every((value) => value.isEmpty)) {
      setState(() {
        _results = null;
        _error = null;
        _loading = false;
      });
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
      _results = null;
    });
    try {
      final data = await ref.read(apiClientProvider).musicSearch(
          query: query['q']!, artist: query['artist']!, album: query['album']!);
      if (!mounted || generation != _generation) return;
      setState(() {
        _results = data;
        _loading = false;
      });
    } catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _error = setsErrorMessage(error);
        _loading = false;
      });
    }
  }

  void _clear() {
    _text.clear();
    _changed('');
  }

  void _play(List<Media> songs, int index) {
    FocusScope.of(context).unfocus();
    final song = songs[index], client = ref.read(apiClientProvider);
    context.push(AppRoutes.audioPlayerPath('${song.id}'), extra: {
      'mediaUrl': client.playbackUrl(song),
      'title': song.displayTitle,
      'sequence': songs,
      'albumCollection': true,
      if (song.duration <= 1200) 'position': 0.0,
    });
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        bottomNavigationBar: const MusicMiniPlayer(),
        appBar: AppBar(
            title: TextField(
              controller: _text,
              autofocus: true,
              decoration: const InputDecoration(
                  hintText: 'Search music', border: InputBorder.none),
              textInputAction: TextInputAction.search,
              onChanged: _changed,
              onSubmitted: (_) {
                FocusScope.of(context).unfocus();
                _load();
              },
            ),
            actions: [
              IconButton(
                  tooltip: 'Clear search',
                  onPressed: _clear,
                  icon: const Icon(Icons.clear)),
              const MusicQueueButton()
            ]),
        body: Column(children: [
          SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Row(children: [
                for (final group in ['All', 'Songs', 'Albums', 'Artists'])
                  Padding(
                      padding: const EdgeInsets.only(right: 8),
                      child: FilterChip(
                        label: Text(group),
                        selected: _group == group,
                        onSelected: (_) => setState(() {
                          _group = group;
                          if (_scroll.hasClients) _scroll.jumpTo(0);
                        }),
                      )),
              ])),
          Expanded(child: _body()),
        ]),
      );

  Widget _body() {
    if (_loading) {
      return const Center(
          child: CircularProgressIndicator(semanticsLabel: 'Searching music'));
    }
    if (_error != null) {
      return Center(
          child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(mainAxisSize: MainAxisSize.min, children: [
                Text('Could not search music. $_error',
                    textAlign: TextAlign.center),
                FilledButton(onPressed: _load, child: const Text('Retry')),
              ])));
    }
    if (_results == null) {
      return const Center(child: Text('Search songs, albums and artists.'));
    }
    final songs = (_results!['songs'] as List? ?? [])
        .cast<Map<String, dynamic>>()
        .map(Media.fromJson)
        .toList();
    final albums =
        (_results!['albums'] as List? ?? []).cast<Map<String, dynamic>>();
    final artists =
        (_results!['artists'] as List? ?? []).cast<Map<String, dynamic>>();
    final children = <Widget>[];
    void heading(String group, int count) {
      children.add(Semantics(
          header: true,
          child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 12),
              child: Text('$group ($count)',
                  style: Theme.of(context).textTheme.titleMedium))));
    }

    if (_group == 'All' || _group == 'Songs') {
      heading('Songs', songs.length);
      for (var i = 0; i < songs.length; i++) {
        final song = songs[i];
        children.add(ListTile(
          contentPadding: EdgeInsets.zero,
          title: Text(song.displayTitle),
          subtitle: Text([song.music?.artist, song.music?.album]
              .whereType<String>()
              .where((value) => value.isNotEmpty)
              .join(' · ')),
          onTap: () => _play(songs, i),
          trailing: PopupMenuButton<QueueInsertion>(
              tooltip: 'Actions for ${song.displayTitle}',
              onSelected: (action) =>
                  insertServerSongs(context, ref, [song], action),
              itemBuilder: (_) => [
                    for (final action in QueueInsertion.values)
                      PopupMenuItem(
                          value: action, child: Text(queueActionLabel(action)))
                  ]),
        ));
      }
    }
    if (_group == 'All' || _group == 'Albums') {
      heading('Albums', albums.length);
      for (final album in albums) {
        children.add(ListTile(
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.album_outlined),
          title: Text(album['title'] as String),
          subtitle: Text(album['artist'] as String),
          trailing: const Icon(Icons.chevron_right),
          onTap: () {
            FocusScope.of(context).unfocus();
            context.push(
                '${AppRoutes.musicAlbums}?album=${Uri.encodeComponent(album['album_key'] as String)}');
          },
        ));
      }
    }
    if (_group == 'All' || _group == 'Artists') {
      heading('Artists', artists.length);
      for (final artist in artists) {
        children.add(ListTile(
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.person_outline),
          title: Text(artist['name'] as String),
          subtitle: Text(
              '${artist['album_count']} albums · ${artist['song_count']} songs'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () {
            FocusScope.of(context).unfocus();
            context.push(
                '${AppRoutes.musicArtists}?artist=${Uri.encodeComponent(artist['artist_key'] as String)}');
          },
        ));
      }
    }
    final count = switch (_group) {
      'Songs' => songs.length,
      'Albums' => albums.length,
      'Artists' => artists.length,
      _ => songs.length + albums.length + artists.length
    };
    if (count == 0) {
      children.insert(
          0,
          Column(children: [
            const Text('Nothing matches these filters'),
            TextButton(onPressed: _clear, child: const Text('Clear filters'))
          ]));
    }
    return ListView(
        controller: _scroll,
        padding: const EdgeInsets.all(16),
        children: children);
  }
}
