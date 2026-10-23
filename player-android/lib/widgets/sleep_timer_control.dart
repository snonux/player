import 'package:flutter/material.dart';
import '../services/sleep_timer.dart';

/// Secondary playback control; its timer belongs to the audio service.
class SleepTimerControl extends StatelessWidget {
  const SleepTimerControl({super.key, required this.timer});
  final SleepTimer timer;

  Future<void> _choose(BuildContext context) async {
    final minutes = await showModalBottomSheet<int>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
          child: SingleChildScrollView(
              child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const ListTile(
              title: Text('Sleep timer'),
              subtitle: Text('Pause playback after a duration.')),
          for (final minutes in [1, 5, 15, 30, 45, 60, 90])
            ListTile(
                title: Text('$minutes minutes'),
                onTap: () => Navigator.pop(context, minutes)),
        ],
      ))),
    );
    if (minutes != null) timer.start(Duration(minutes: minutes));
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
        animation: timer,
        builder: (context, _) {
          final left = timer.remaining;
          final seconds =
              left == null ? 0 : (left.inMilliseconds / 1000).ceil();
          final label = timer.errorMessage ??
              (left == null
                  ? (timer.expired
                      ? 'Sleep timer ended · paused'
                      : 'Sleep timer')
                  : 'Sleep ${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')} remaining');
          return Material(
              color: Colors.black,
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 12),
                child: Wrap(
                    alignment: WrapAlignment.center,
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: [
                      TextButton.icon(
                          onPressed: () => _choose(context),
                          icon: const Icon(Icons.bedtime_outlined),
                          label: Text(label)),
                      if (left != null)
                        TextButton(
                            onPressed: timer.cancel,
                            child: const Text('Cancel timer')),
                    ]),
              ));
        },
      );
}
