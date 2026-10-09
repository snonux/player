import 'package:flutter/material.dart';

import '../utils/playback_errors.dart';

/// Spinner for the audio and video player screens while a source loads.
///
/// With [preparing] it also says why loading takes longer than usual: the
/// server is still producing a compatibility stream, which can take minutes
/// for a long video the first time it is played.
class PlaybackLoadingView extends StatelessWidget {
  const PlaybackLoadingView({super.key, required this.preparing});

  final bool preparing;

  @override
  Widget build(BuildContext context) => Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const CircularProgressIndicator(),
            if (preparing) ...[
              const SizedBox(height: 16),
              const Text(
                kPreparingPlaybackLabel,
                key: Key('playback_preparing_label'),
                style: TextStyle(color: Colors.white70),
                textAlign: TextAlign.center,
              ),
            ],
          ],
        ),
      );
}
