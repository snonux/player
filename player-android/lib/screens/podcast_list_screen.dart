import 'package:flutter/material.dart';
import 'standalone_podcasts_screen.dart';

/// Native podcasts use device subscriptions, independent of server sets.
class PodcastListScreen extends StatelessWidget {
  const PodcastListScreen({super.key});
  @override
  Widget build(BuildContext context) => const StandalonePodcastsScreen();
}
