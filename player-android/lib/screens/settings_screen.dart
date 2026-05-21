import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';
import '../providers/settings_provider.dart';

/// Settings screen: editable server base URL, current username, and logout.
///
/// Design notes:
///   - [ConsumerStatefulWidget] is used so that the text controller can be
///     initialised from the persisted settings and [WidgetRef] is available
///     throughout the async logout path without storing a stale ref.
///   - The base URL is pre-filled from [settingsProvider] and saved on every
///     submit (Enter key or "Save" button).
///   - Logout clears the bearer token via [AuthStateNotifier.logout], which
///     triggers go_router's redirect callback (via [refreshListenable]) and
///     navigates to /login automatically.  An explicit [context.go] acts as a
///     safety net in case the redirect has not fired yet.
///   - All async continuations guard on [mounted] to prevent setState/context
///     calls after widget disposal.
class SettingsScreen extends ConsumerStatefulWidget {
  const SettingsScreen({super.key});

  @override
  ConsumerState<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends ConsumerState<SettingsScreen> {
  // Controller for the server base URL text field.  Initialised once from the
  // persisted settings value and disposed when the widget leaves the tree.
  final _urlController = TextEditingController();

  // True while the logout round-trip (token deletion + state update) is in
  // progress; prevents double-tapping the logout button.
  bool _isLoggingOut = false;

  // Tracks whether the URL controller has been seeded from the loaded settings
  // so we populate it exactly once (on the first non-loading build).
  bool _urlInitialised = false;

  @override
  void dispose() {
    _urlController.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // URL save logic
  // ---------------------------------------------------------------------------

  /// Validates the URL field and persists the new value via [SettingsNotifier].
  ///
  /// Trims whitespace so that a trailing newline from keyboard submission does
  /// not get saved as part of the URL.
  Future<void> _saveBaseUrl() async {
    final url = _urlController.text.trim();
    if (url.isEmpty) return;

    // Persist the new URL; [SettingsNotifier] updates in-memory state first so
    // the UI reflects the change immediately without waiting for the disk write.
    await ref.read(settingsProvider.notifier).setServerBaseUrl(url);

    // Dismiss the keyboard now that the value has been committed.
    if (mounted) FocusScope.of(context).unfocus();
  }

  // ---------------------------------------------------------------------------
  // Logout logic
  // ---------------------------------------------------------------------------

  /// Clears the stored bearer token and transitions to the unauthenticated state.
  ///
  /// [AuthStateNotifier.logout] deletes the token from secure storage and sets
  /// state to [AuthStatus.unauthenticated].  The router's [refreshListenable]
  /// picks up the change and the redirect callback routes to /login automatically.
  /// The explicit [context.go] below acts as a safety net.
  Future<void> _logout() async {
    setState(() => _isLoggingOut = true);
    try {
      await ref.read(authStateProvider.notifier).logout();
      // Safety-net navigation in case the router redirect has not fired yet.
      if (mounted) context.go(AppRoutes.login);
    } finally {
      // Only call setState if the widget is still in the tree; navigation may
      // have triggered dispose before the finally block executes.
      if (mounted) setState(() => _isLoggingOut = false);
    }
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    // Watch settings to seed the URL field on first load.
    final settingsAsync = ref.watch(settingsProvider);

    // Seed the URL text field exactly once, after settings have loaded.
    // Doing this in build (rather than initState) ensures we have the loaded
    // value; [_urlInitialised] prevents clobbering an in-progress edit.
    settingsAsync.whenData((settings) {
      if (!_urlInitialised) {
        _urlController.text = settings.serverBaseUrl;
        _urlInitialised = true;
      }
    });

    // Read the stored token as the username display.  The token stored by
    // AuthStateNotifier is the username string (LoginScreen and BootstrapScreen
    // both call `authStateProvider.notifier.login(user.username)`).
    final usernameAsync = ref.watch(_currentUsernameProvider);
    final username = usernameAsync.valueOrNull ?? '—';

    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              // ----------------------------------------------------------------
              // Account section: signed-in username + logout.
              // ----------------------------------------------------------------
              Text(
                'Account',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 12),

              // Current username row.
              Row(
                children: [
                  const Icon(Icons.person_outline),
                  const SizedBox(width: 12),
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Signed in as',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                      Text(
                        username,
                        key: const Key('settings_username'),
                        style: Theme.of(context).textTheme.bodyLarge,
                      ),
                    ],
                  ),
                ],
              ),
              const SizedBox(height: 24),

              // Logout button: shows a spinner while the token is being deleted.
              _isLoggingOut
                  ? const Center(child: CircularProgressIndicator())
                  : OutlinedButton(
                      key: const Key('settings_logout'),
                      onPressed: _logout,
                      style: OutlinedButton.styleFrom(
                        foregroundColor:
                            Theme.of(context).colorScheme.error,
                        side: BorderSide(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                      child: const Text('Log Out'),
                    ),

              const SizedBox(height: 32),
              const Divider(),
              const SizedBox(height: 24),

              // ----------------------------------------------------------------
              // Server section: editable base URL.
              // ----------------------------------------------------------------
              Text(
                'Server',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 12),

              // Server base URL field pre-filled from persisted settings.
              TextField(
                key: const Key('settings_base_url'),
                controller: _urlController,
                decoration: const InputDecoration(
                  labelText: 'Server base URL',
                  border: OutlineInputBorder(),
                  helperText:
                      'e.g. https://player.example.com  or  http://10.0.2.2:8080',
                ),
                keyboardType: TextInputType.url,
                autocorrect: false,
                textInputAction: TextInputAction.done,
                // Persist when the user presses "Done" on the keyboard.
                onSubmitted: (_) => _saveBaseUrl(),
              ),
              const SizedBox(height: 12),

              ElevatedButton(
                key: const Key('settings_save_url'),
                onPressed: _saveBaseUrl,
                child: const Text('Save URL'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// File-level helpers
// ---------------------------------------------------------------------------

/// Reads the current username from [tokenStorageProvider].
///
/// The username is stored as the bearer token value by [AuthStateNotifier.login]
/// (both LoginScreen and BootstrapScreen call `login(user.username)`).
/// This autoDispose FutureProvider is re-evaluated whenever the provider scope
/// changes, ensuring the display is up-to-date after logout/login transitions.
///
/// Kept private (underscore prefix) because it is an implementation detail of
/// this screen — no other file should depend on it.
final _currentUsernameProvider = FutureProvider.autoDispose<String?>((ref) {
  final storage = ref.watch(tokenStorageProvider);
  return storage.readToken();
});
