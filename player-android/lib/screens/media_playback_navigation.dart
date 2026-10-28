import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/models.dart';
import '../providers/api_client_provider.dart';

/// Opens the player or viewer for [media] directly, without the details
/// screen. Audio keeps [sequence] so Next/Previous follow the list the user
/// tapped in. Completes when the opened route is popped.
///
/// Media of an unknown type falls back to the details screen, which explains
/// what the item is instead of failing to play it.
Future<void> openMediaPlayback(
  BuildContext context,
  WidgetRef ref,
  Media media, {
  List<Media> sequence = const [],
  bool traverseFolders = false,
}) async {
  final client = ref.read(apiClientProvider);
  final id = media.id.toString();
  final extra = <String, Object>{
    'sourceName': media.relPath,
    'title': media.displayTitle,
  };
  switch (media.type) {
    case 'image':
      await context.push(AppRoutes.imageViewerPath(id), extra: {
        ...extra,
        'mediaUrl': client.streamUrl(media.id),
        'sequence': sequence.isEmpty ? [media] : List<Media>.of(sequence)
      });
    case 'video':
      await context.push(AppRoutes.videoPlayerPath(id),
          extra: {...extra, 'mediaUrl': client.playbackUrl(media)});
    case 'audio':
      await context.push(AppRoutes.audioPlayerPath(id), extra: {
        ...extra,
        'mediaUrl': client.playbackUrl(media),
        'sequence': sequence.isEmpty ? [media] : List<Media>.of(sequence),
        'traverseFolders': traverseFolders,
      });
    default:
      await context.push(AppRoutes.mediaDetailPath(media.id));
  }
}

/// Like [openMediaPlayback] for callers that only know the media ID. The
/// metadata request decides which player opens; a failure shows a snack bar.
Future<void> openMediaPlaybackById(
    BuildContext context, WidgetRef ref, int mediaId) async {
  final Media media;
  try {
    media = await ref.read(apiClientProvider).getMedia(mediaId);
  } catch (_) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Could not open this item.')));
    }
    return;
  }
  if (context.mounted) await openMediaPlayback(context, ref, media);
}

/// Opens the details screen for the item currently shown by a player. The
/// details screen's Play button returns to that player instead of stacking a
/// second one.
void openPlaybackDetails(BuildContext context, int mediaId) =>
    context.push(AppRoutes.mediaDetailPath(mediaId),
        extra: const {'fromPlayback': true});
