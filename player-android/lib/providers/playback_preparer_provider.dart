import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/playback_preparer.dart';

/// Retry policy for server compatibility streams, shared by the audio and
/// video screens. Tests override it to shorten the total wait.
final playbackPreparerProvider =
    Provider<PlaybackPreparer>((ref) => const PlaybackPreparer());
