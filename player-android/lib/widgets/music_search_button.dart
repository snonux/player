import '../l10n/music_localizations.dart';
import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../app_routes.dart';

/// The same discoverable search entry is available throughout Music.
class MusicSearchButton extends StatelessWidget {
  const MusicSearchButton({super.key});
  @override
  Widget build(BuildContext context) => IconButton(
        tooltip: context.tr('Search music'),
        icon: const Icon(Icons.search),
        onPressed: () => context.push(AppRoutes.musicSearch),
      );
}
