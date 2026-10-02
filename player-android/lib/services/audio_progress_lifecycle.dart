import 'dart:async';

import 'package:flutter/widgets.dart';

/// Saves a final sample when the UI leaves the foreground. The media handler
/// retains source ownership and continues background playback independently.
class AudioProgressLifecycle extends WidgetsBindingObserver {
  AudioProgressLifecycle(this.saveProgress);

  final Future<void> Function() saveProgress;

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.inactive ||
        state == AppLifecycleState.hidden ||
        state == AppLifecycleState.paused ||
        state == AppLifecycleState.detached) {
      unawaited(saveProgress());
    }
  }
}
