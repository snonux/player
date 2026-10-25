import 'package:flutter/material.dart';
import 'standalone_podcasts_screen.dart';

/// Compatibility destination for saved numeric server-set routes. Server set
/// IDs cannot identify standalone subscriptions; choose a device podcast here.
/// Existing server copies remain available in the ordinary media library.
class PodcastEpisodesScreen extends StatelessWidget {
  const PodcastEpisodesScreen({super.key, required this.setId, this.setName});
  final int setId;
  final String? setName;
  @override
  Widget build(BuildContext context) => const StandalonePodcastsScreen();
}
