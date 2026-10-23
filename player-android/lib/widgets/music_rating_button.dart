import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/api_client_provider.dart';

/// An account-scoped star rating, separate from hearts.
class MusicRatingButton extends ConsumerStatefulWidget {
  const MusicRatingButton(
      {super.key,
      required this.kind,
      required this.entity,
      required this.onChanged});
  final String kind;
  final Map<String, dynamic> entity;
  final VoidCallback onChanged;
  @override
  ConsumerState<MusicRatingButton> createState() => _MusicRatingButtonState();
}

class _MusicRatingButtonState extends ConsumerState<MusicRatingButton> {
  bool _saving = false;
  @override
  Widget build(BuildContext context) {
    final rating = widget.entity['rating'] as int? ?? 0;
    return PopupMenuButton<int>(
        tooltip:
            '${widget.kind} rating: ${rating == 0 ? "Unrated" : "$rating of 5 stars"}',
        enabled: !_saving,
        initialValue: rating,
        icon: Semantics(
            label: '${widget.kind} rating: $rating of 5 stars',
            child: Row(mainAxisSize: MainAxisSize.min, children: [
              Icon(rating == 0 ? Icons.star_border : Icons.star),
              if (rating > 0) Text('$rating')
            ])),
        itemBuilder: (_) => [
              const PopupMenuItem(
                  value: 0, child: Text('Unrated · Clear rating')),
              for (var n = 1; n <= 5; n++)
                PopupMenuItem(
                    value: n, child: Text('$n ${n == 1 ? "star" : "stars"}')),
            ],
        onSelected: (value) async {
          final api = ref.read(apiClientProvider), entity = widget.entity;
          final messenger = ScaffoldMessenger.of(context);
          final credentialEpoch =
              ref.read(credentialMutationQueueProvider).generation;
          setState(() => _saving = true);
          try {
            final key =
                '${entity[widget.kind == 'song' ? 'id' : '${widget.kind}_key']}';
            await api.setMusicRating(widget.kind, key, value);
            if (!mounted ||
                ref.read(apiClientProvider) != api ||
                ref.read(credentialMutationQueueProvider).generation !=
                    credentialEpoch ||
                widget.entity != entity) {
              return;
            }
            entity['rating'] = value;
            widget.onChanged();
            messenger.hideCurrentSnackBar();
            messenger.showSnackBar(SnackBar(
                content: Text(value == 0
                    ? 'Rating cleared.'
                    : 'Rated $value of 5 stars.')));
          } catch (_) {
            if (mounted &&
                ref.read(apiClientProvider) == api &&
                ref.read(credentialMutationQueueProvider).generation ==
                    credentialEpoch) {
              messenger.hideCurrentSnackBar();
              messenger.showSnackBar(const SnackBar(
                  content: Text(
                      'Could not save rating. Check your connection and retry.')));
            }
          } finally {
            if (mounted) setState(() => _saving = false);
          }
        });
  }
}
