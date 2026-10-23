import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:dio/dio.dart';
import '../api/music_artwork_api.dart';
import '../providers/api_client_provider.dart';

/// Fixed-size preview and discoverable native artwork editing controls.
class MusicArtworkSection extends ConsumerStatefulWidget {
  const MusicArtworkSection(
      {super.key, required this.kind, required this.entityKey, this.fallback});
  final String kind, entityKey;
  final Widget? fallback;
  @override
  ConsumerState<MusicArtworkSection> createState() =>
      _MusicArtworkSectionState();
}

class _MusicArtworkSectionState extends ConsumerState<MusicArtworkSection> {
  Map<String, dynamic>? _data;
  Uint8List? _image;
  bool _busy = false, _failed = false;
  String? _message;
  int _generation = 0;
  @override
  void initState() {
    super.initState();
    Future.microtask(_load);
  }

  @override
  void didUpdateWidget(covariant MusicArtworkSection oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.kind != widget.kind ||
        oldWidget.entityKey != widget.entityKey) {
      _generation++;
      _busy = false;
      _data = null;
      _image = null;
      _load();
    }
  }

  Future<void> _load({bool pick = false, bool remove = false}) async {
    if (_busy) return;
    final client = ref.read(apiClientProvider), generation = ++_generation;
    final kind = widget.kind, key = widget.entityKey;
    bool alive() =>
        mounted &&
        generation == _generation &&
        ref.read(apiClientProvider) == client;
    setState(() {
      _busy = true;
      _failed = false;
      _message = pick || remove ? 'Saving artwork…' : 'Loading artwork…';
    });
    try {
      Uint8List? bytes;
      if (pick) {
        bytes =
            await const MethodChannel('zone.foo.player_android/artwork_picker')
                .invokeMethod<Uint8List>('pick');
        if (!alive()) return;
        if (bytes == null) {
          setState(() => _message = null);
          return;
        }
      }
      final data = pick || remove
          ? await client.saveMusicArtwork(kind, key, bytes)
          : await client.musicArtwork(kind, key);
      if (!alive()) return;
      // Clear removed artwork immediately, even if reading a new preview fails.
      setState(() {
        _data = data;
        if (data['has_artwork'] != true) _image = null;
      });
      final image = data['has_artwork'] == true
          ? await client.musicArtworkImage(
              kind, key, data['revision'] as String)
          : null;
      if (!alive()) return;
      setState(() {
        _image = image;
        _message = pick
            ? 'Artwork saved.'
            : remove
                ? 'Custom artwork removed.'
                : null;
      });
    } catch (error) {
      if (!alive()) return;
      setState(() {
        _failed = true;
        if (error is DioException &&
            [401, 403, 404].contains(error.response?.statusCode)) {
          _data = null;
          _image = null;
        }
        final body = error is DioException ? error.response?.data : null;
        _message = body is Map && body['error'] is String
            ? body['error'] as String
            : error is PlatformException
                ? error.message
                : 'Could not update artwork. Check your connection and retry.';
      });
    } finally {
      if (alive()) setState(() => _busy = false);
    }
  }

  Future<void> _remove() async {
    final client = ref.read(apiClientProvider),
        key = widget.entityKey,
        kind = widget.kind;
    final yes = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
                title: const Text('Remove artwork?'),
                content: const Text(
                    'Restore the default cover for this artist or playlist.'),
                actions: [
                  TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: const Text('Cancel')),
                  FilledButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: const Text('Remove'))
                ]));
    if (yes == true &&
        mounted &&
        ref.read(apiClientProvider) == client &&
        key == widget.entityKey &&
        kind == widget.kind) {
      await _load(remove: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(apiClientProvider, (previous, next) {
      if (previous != next) {
        _generation++;
        _busy = false;
        _data = null;
        _image = null;
        Future.microtask(_load);
      }
    });
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      SizedBox(
          width: 112,
          height: 112,
          child: _image == null
              ? widget.fallback ?? const Icon(Icons.music_note, size: 64)
              : Image.memory(_image!,
                  fit: BoxFit.cover,
                  semanticLabel: 'Custom ${widget.kind} artwork',
                  errorBuilder: (_, __, ___) =>
                      const Icon(Icons.broken_image_outlined))),
      if (_data?['can_edit'] == true)
        Wrap(spacing: 8, children: [
          TextButton.icon(
              onPressed: _busy ? null : () => _load(pick: true),
              icon: const Icon(Icons.image_outlined),
              label: const Text('Change artwork')),
          if (_data?['has_artwork'] == true)
            TextButton(
                onPressed: _busy ? null : _remove,
                child: const Text('Remove artwork'))
        ]),
      if (_message != null) Text(_message!, semanticsLabel: _message),
      if (!_busy && _failed)
        TextButton(onPressed: _load, child: const Text('Retry artwork')),
    ]);
  }
}
