import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'router.dart';

/// Entry point — wraps the whole widget tree in a [ProviderScope] so every
/// widget and provider has access to the Riverpod container.
void main() => runApp(const ProviderScope(child: PlayerAndroidApp()));

/// Root application widget.
///
/// Uses [ConsumerWidget] to read [routerProvider] from Riverpod so that the
/// same [GoRouter] instance (and its navigator key) is reused across rebuilds.
/// [MaterialApp.router] delegates all navigation decisions to go_router.
class PlayerAndroidApp extends ConsumerWidget {
  const PlayerAndroidApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(routerProvider);

    return MaterialApp.router(
      title: 'Player',
      routerConfig: router,
    );
  }
}
