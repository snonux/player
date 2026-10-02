import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/playback_session_coordinator.dart';

final playbackSessionCoordinatorProvider =
    Provider<PlaybackSessionCoordinator>((ref) => PlaybackSessionCoordinator());
