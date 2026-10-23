import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/api_client_provider.dart';
import '../providers/presentation_provider.dart';
import '../providers/auth_state_provider.dart';
import '../services/playback_request.dart';
import 'authenticated_network_image.dart';
import 'public_network_image.dart';

/// Artwork uses the same source and account boundary as playback. Device files
/// and offline copies do not have a server artwork source.
class AudioCoverArt extends ConsumerWidget {
  const AudioCoverArt({super.key, required this.request});

  final PlaybackRequest request;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final presentation = ref.watch(presentationProvider).valueOrNull ??
        const PresentationPreferences();
    final fit = presentation.display == ArtworkDisplay.cover
        ? BoxFit.cover
        : BoxFit.contain;
    const fallback = Icon(Icons.headphones,
        key: Key('audio_artwork_unavailable'), size: 80, color: Colors.white54);
    const loading = Icon(Icons.headphones,
        key: Key('audio_artwork_loading'), size: 80, color: Colors.white54);
    Widget image = fallback;
    final source = request;
    if (source is ServerPlaybackRequest &&
        source.serverOrigin == ref.watch(playerBaseUrlProvider).origin &&
        _ownsAccount(ref, source)) {
      image = AuthenticatedNetworkImage(
        imageUrl: ref.read(apiClientProvider).thumbnailUrl(source.mediaId),
        fit: fit,
        placeholder: (_, __) => loading,
        errorWidget: (_, __, ___) => fallback,
      );
    } else if (source is PublicSharePlaybackRequest) {
      // Keep the viewing credential and any server path prefix. This endpoint
      // only probes the share and never opens another viewing.
      final uri = source.sourceUri;
      image = PublicNetworkImage(
        imageUrl: uri.resolve('thumbnail').replace(query: uri.query).toString(),
        fit: fit,
        placeholder: (_, __) => loading,
        errorWidget: (_, __, ___) => fallback,
      );
    }
    return Container(
      key: const Key('audio_player_cover_art'),
      width: 200,
      height: 200,
      clipBehavior: Clip.antiAlias,
      decoration: BoxDecoration(
        color: Colors.grey[850],
        borderRadius: BorderRadius.circular(switch (presentation.shape) {
          ArtworkShape.rounded => 12,
          ArtworkShape.square => 0,
          ArtworkShape.circle => 100,
        }),
      ),
      child: image,
    );
  }

  bool _ownsAccount(WidgetRef ref, ServerPlaybackRequest source) {
    // Legacy routes without an account ID use the current credential gate.
    // Typed requests with an owner must never adopt another account's token.
    if (source.userId == 0) return true;
    final auth = ref.watch(authStateProvider).valueOrNull;
    return auth?.isAuthenticated == true && auth?.user?.id == source.userId;
  }
}
