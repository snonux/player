import 'package:flutter/material.dart';

import '../services/saved_server_credentials.dart';

/// Consent follows the biometric prompt and a failed reachability check.
Future<bool> offerOfflineServer(
    BuildContext context, SavedServerCredential credential) async {
  if (!context.mounted) return false;
  return await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('Server unavailable'),
          content: Text('${credential.origin} could not be reached.\n\n'
              'Use offline mode for ${credential.user.username}? '
              'Only this account’s saved files will be shown. '
              'Playback progress syncs after reconnecting.'),
          actions: [
            TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('Stay signed out')),
            FilledButton(
                key: const Key('use_server_offline'),
                onPressed: () => Navigator.pop(context, true),
                child: const Text('Use offline mode')),
          ],
        ),
      ) ??
      false;
}
