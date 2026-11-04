import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'app_routes.dart';
import 'screens/audio_player_screen.dart';
import 'screens/podcast_list_screen.dart';
import 'screens/podcast_episodes_screen.dart';
import 'screens/standalone_podcasts_screen.dart';
import 'services/playback_request.dart';

/// One navigation stack for cached podcast browsing and playback in every mode.
List<GoRoute> podcastRoutes() => [
      GoRoute(
          path: AppRoutes.podcasts,
          builder: (_, state) => PodcastListScreen(
              serverSetId:
                  int.tryParse(state.uri.queryParameters['setId'] ?? ''),
              serverSetName: state.uri.queryParameters['name'])),
      GoRoute(
          path: AppRoutes.podcastFeed,
          builder: (_, state) {
            final key = state.uri.queryParameters['feed'];
            return key == null || key.isEmpty
                ? const PodcastListScreen()
                : LocalPodcastEpisodesScreen(
                    feedKey: key,
                    title: state.uri.queryParameters['title'] ??
                        'Podcast episodes');
          }),
      GoRoute(
          path: AppRoutes.podcastPlayer,
          builder: (context, state) {
            final request = state.extra;
            if (request is PodcastPlaybackRequest) {
              return AudioPlayerScreen(
                  mediaId: request.identity,
                  mediaTitle: request.title,
                  request: request);
            }
            return Scaffold(
                appBar: AppBar(title: const Text('Podcast playback')),
                body: Center(
                    child: TextButton(
                        onPressed: () => context.go(AppRoutes.podcasts),
                        child: const Text('Open podcast list'))));
          }),
      GoRoute(
          path: AppRoutes.podcastEpisodes,
          builder: (_, state) => PodcastEpisodesScreen(
              setId: int.tryParse(state.pathParameters['setId'] ?? '') ?? 0,
              setName: state.uri.queryParameters['name'])),
    ];
