import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/api_client_provider.dart';

/// An account-scoped entity heart, independent of song favorites.
class MusicFavoriteButton extends ConsumerStatefulWidget {
  const MusicFavoriteButton(
      {super.key,
      required this.kind,
      required this.entity,
      required this.onChanged});
  final String kind;
  final Map<String, dynamic> entity;
  final VoidCallback onChanged;
  @override
  ConsumerState<MusicFavoriteButton> createState() =>
      _MusicFavoriteButtonState();
}

class _MusicFavoriteButtonState extends ConsumerState<MusicFavoriteButton> {
  bool _saving = false;
  @override
  Widget build(BuildContext context) {
    final favorite = widget.entity['is_favorite'] == true;
    return IconButton(
        tooltip: '${favorite ? 'Unfavorite' : 'Favorite'} ${widget.kind}',
        isSelected: favorite,
        icon: const Icon(Icons.favorite_border),
        selectedIcon: const Icon(Icons.favorite),
        onPressed: _saving
            ? null
            : () async {
                final api = ref.read(apiClientProvider), entity = widget.entity;
                final messenger = ScaffoldMessenger.of(context);
                final credentialEpoch =
                    ref.read(credentialMutationQueueProvider).generation;
                setState(() => _saving = true);
                try {
                  final key = entity[widget.kind == 'playlist'
                      ? 'playlist_id'
                      : '${widget.kind}_key'] as String;
                  await api.setMusicFavorite(widget.kind, key, !favorite);
                  if (!mounted ||
                      ref.read(apiClientProvider) != api ||
                      ref.read(credentialMutationQueueProvider).generation !=
                          credentialEpoch ||
                      widget.entity != entity) {
                    return;
                  }
                  entity['is_favorite'] = !favorite;
                  widget.onChanged();
                  messenger.hideCurrentSnackBar();
                  messenger.showSnackBar(SnackBar(
                      content: Text(!favorite
                          ? 'Added to favorites.'
                          : 'Removed from favorites.')));
                } catch (_) {
                  if (mounted &&
                      ref.read(apiClientProvider) == api &&
                      ref.read(credentialMutationQueueProvider).generation ==
                          credentialEpoch) {
                    messenger.hideCurrentSnackBar();
                    messenger.showSnackBar(const SnackBar(
                        content: Text(
                            'Could not save favorite. Check your connection and retry.')));
                  }
                } finally {
                  if (mounted) setState(() => _saving = false);
                }
              });
  }
}
