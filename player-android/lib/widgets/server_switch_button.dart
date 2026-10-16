import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../library_navigation.dart';
import '../navigation_key.dart';
import '../providers/auth_state_provider.dart';
import '../providers/first_run_provider.dart';
import '../providers/settings_provider.dart';

/// App bar action for switching between saved servers in one tap.
///
/// Hidden unless more than one server is saved. Picking a server locks the
/// current session (see [AuthStateNotifier.switchServer]), opens the sign-in
/// page for the new server, and offers the fingerprint unlock right away when
/// a saved login exists for it. Without one, the sign-in page stays open.
class ServerSwitchButton extends ConsumerStatefulWidget {
  const ServerSwitchButton({super.key});

  @override
  ConsumerState<ServerSwitchButton> createState() => _ServerSwitchButtonState();
}

class _ServerSwitchButtonState extends ConsumerState<ServerSwitchButton> {
  bool _switching = false;

  @override
  Widget build(BuildContext context) {
    final settings = ref.watch(settingsProvider).valueOrNull;
    final servers = settings?.savedServers ?? const <String>[];
    if (servers.length < 2) return const SizedBox.shrink();
    final current = settings?.destination == LibraryDestination.server
        ? settings?.serverBaseUrl
        : null;
    return PopupMenuButton<String>(
      key: const Key('server_switch_button'),
      tooltip: 'Switch server',
      icon: const Icon(Icons.swap_horiz),
      enabled: !_switching,
      onSelected: _switchTo,
      itemBuilder: (context) => [
        for (final server in servers)
          CheckedPopupMenuItem<String>(
            key: Key('server_switch_option_$server'),
            value: server,
            checked: server == current,
            enabled: server != current,
            child: Text(server, overflow: TextOverflow.ellipsis),
          ),
      ],
    );
  }

  Future<void> _switchTo(String server) async {
    setState(() => _switching = true);
    // The router may replace this screen as soon as the old session locks,
    // so keep long-lived handles instead of this widget's ref and context.
    final container = ProviderScope.containerOf(context, listen: false);
    final router = GoRouter.of(context);
    final messenger =
        appMessengerKey.currentState ?? ScaffoldMessenger.maybeOf(context);
    final auth = container.read(authStateProvider.notifier);
    try {
      await auth.switchServer(server);
    } catch (_) {
      messenger?.showSnackBar(
        const SnackBar(content: Text('Could not switch server.')),
      );
      if (mounted) setState(() => _switching = false);
      return;
    }
    container.invalidate(firstRunProvider);
    router.go(AppRoutes.login);
    if (mounted) setState(() => _switching = false);
    try {
      if (await auth.unlockSavedServer()) {
        final user = container.read(authStateProvider).requireValue.user!;
        await initializeServerProgressQueueWith(container.read, user);
      }
    } catch (_) {
      // The sign-in page offers the fingerprint and password options again.
    }
  }
}
