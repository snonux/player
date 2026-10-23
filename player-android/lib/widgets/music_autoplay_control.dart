import 'package:flutter/material.dart';
import '../l10n/music_localizations.dart';

/// Queue-local opt-in; disabling stays available during a related-song lookup.
class MusicAutoplayControl extends StatelessWidget {
  const MusicAutoplayControl({
    super.key,
    required this.enabled,
    required this.onChanged,
    this.available = true,
    this.status = '',
  });

  final bool enabled, available;
  final ValueChanged<bool> onChanged;
  final String status;

  @override
  Widget build(BuildContext context) => Column(children: [
        SwitchListTile(
          title: Text(context.tr('Autoplay related music')),
          subtitle: Text(context.tr(available
              ? 'After this queue ends · related songs from your library'
              : 'Autoplay needs a connection.')),
          value: enabled,
          onChanged: available || enabled ? onChanged : null,
        ),
        if (status.isNotEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
            child: Semantics(liveRegion: true, child: Text(context.tr(status))),
          ),
      ]);
}
