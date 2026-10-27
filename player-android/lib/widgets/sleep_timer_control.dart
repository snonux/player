import 'package:flutter/material.dart';
import '../services/sleep_timer.dart';
import 'overflow_menu.dart';

/// Lets the user pick a duration, then starts [timer].
Future<void> chooseSleepTimer(BuildContext context, SleepTimer timer) async {
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

/// Sleep timer status; its timer belongs to the audio service. Hidden while
/// no timer runs, has just ended or failed; [SleepTimerMenuItem] starts one.
class SleepTimerControl extends StatelessWidget {
  const SleepTimerControl({super.key, required this.timer});
  final SleepTimer timer;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
        animation: timer,
        builder: (context, _) {
          final left = timer.remaining;
          if (left == null && !timer.expired && timer.errorMessage == null) {
            return const SizedBox.shrink();
          }
          final seconds =
              left == null ? 0 : (left.inMilliseconds / 1000).ceil();
          final label = timer.errorMessage ??
              (left == null
                  ? 'Sleep timer ended · paused'
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
                          onPressed: () => chooseSleepTimer(context, timer),
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

/// Player menu row that starts or changes the sleep timer. [playerContext]
/// opens the chooser, since the menu closes first.
class SleepTimerMenuItem extends StatelessWidget {
  const SleepTimerMenuItem(
      {super.key, required this.timer, required this.playerContext});
  final SleepTimer timer;
  final BuildContext playerContext;

  @override
  Widget build(BuildContext context) => OverflowMenuItem(
        key: const Key('sleep_timer_menu_item'),
        icon: Icons.bedtime_outlined,
        label: 'Sleep timer',
        subtitle: timer.remaining == null ? null : 'Running',
        onSelected: () => chooseSleepTimer(playerContext, timer),
      );
}
