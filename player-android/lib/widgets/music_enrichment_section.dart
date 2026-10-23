import 'package:flutter/material.dart';
import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/api_client_provider.dart';
import '../services/external_metadata_links.dart';

/// Optional provider metadata remains separate from local tags and artwork.
class MusicEnrichmentSection extends ConsumerStatefulWidget {
  const MusicEnrichmentSection(
      {super.key, required this.kind, required this.entityKey});
  final String kind, entityKey;
  @override
  ConsumerState<MusicEnrichmentSection> createState() =>
      _MusicEnrichmentSectionState();
}

class _MusicEnrichmentSectionState
    extends ConsumerState<MusicEnrichmentSection> {
  Map<String, dynamic>? _data;
  bool _busy = true;
  String? _error;
  int _generation = 0;
  @override
  void initState() {
    super.initState();
    Future.microtask(_load);
  }

  @override
  void didUpdateWidget(covariant MusicEnrichmentSection oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.kind != widget.kind ||
        oldWidget.entityKey != widget.entityKey) {
      _data = null;
      _load();
    }
  }

  Future<void> _load({bool refresh = false}) async {
    final generation = ++_generation, client = ref.read(apiClientProvider);
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final data = await client.musicEnrichment(widget.kind, widget.entityKey,
          refresh: refresh);
      if (!mounted ||
          generation != _generation ||
          ref.read(apiClientProvider) != client) {
        return;
      }
      setState(() {
        _data = data;
        _busy = false;
      });
    } catch (error) {
      if (!mounted ||
          generation != _generation ||
          ref.read(apiClientProvider) != client) {
        return;
      }
      setState(() {
        if (error is DioException &&
            [401, 403, 404].contains(error.response?.statusCode)) {
          _data = null;
        }
        _error =
            'Could not load metadata. Check your connection or library access and try again.';
        _busy = false;
      });
    }
  }

  Future<void> _open(String url) async {
    try {
      await openMetadataLink(url);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content: Text(
                'Could not open metadata link. Check that a browser is installed.')));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(apiClientProvider, (previous, next) {
      if (previous != next) {
        _generation++;
        _data = null;
        _busy = true;
        _error = null;
        Future.microtask(_load);
      }
    });
    final data = _data;
    if (data == null && _busy) {
      return const SizedBox.shrink();
    }
    if (data?['enabled'] == false) {
      return const SizedBox.shrink();
    }
    final description = data?['description'] as String?;
    final fetched =
        DateTime.tryParse(data?['fetched_at'] as String? ?? '')?.toLocal();
    return ExpansionTile(
        key: ValueKey('${widget.kind}-${widget.entityKey}'),
        title: const Text('About'),
        childrenPadding: const EdgeInsets.all(16),
        expandedCrossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (_busy)
            const LinearProgressIndicator(semanticsLabel: 'Loading metadata'),
          if (data?['image_url'] != null)
            Image.network(data!['image_url'] as String,
                height: 160,
                fit: BoxFit.contain,
                semanticLabel: 'Provider image from ${data['source']}',
                errorBuilder: (_, error, stack) => const Text(
                    'Provider image is unavailable. Local artwork is unchanged.')),
          if (description != null && description.isNotEmpty)
            SelectableText(description),
          for (final link
              in (data?['links'] as List? ?? []).cast<Map<String, dynamic>>())
            TextButton.icon(
                onPressed: () => _open(link['url'] as String),
                icon: const Icon(Icons.open_in_new),
                label: Text(link['label'] as String)),
          if (data?['source'] != null) Text('Source: ${data!['source']}'),
          if (fetched != null)
            Text(
                'Updated: $fetched${data?['stale'] == true ? ' · May be out of date' : ''}'),
          if (data?['state'] == 'empty')
            const Text('No metadata found for this artist or album.'),
          if (data?['state'] == 'unfetched')
            Text(data?['can_refresh'] == true
                ? 'Choose Refresh metadata to look up this artist or album.'
                : 'No metadata fetched yet. An administrator can refresh it.'),
          if (data?['state'] == 'error')
            Text(data?['message'] as String? ??
                'Metadata refresh failed. Previous metadata is retained.'),
          if (_error != null) Text(_error!),
          if (_error != null)
            TextButton(
                onPressed: _busy ? null : () => _load(),
                child: const Text('Retry')),
          if (data?['can_refresh'] == true)
            TextButton.icon(
                onPressed: _busy ? null : () => _load(refresh: true),
                icon: const Icon(Icons.refresh),
                label: const Text('Refresh metadata')),
        ]);
  }
}
