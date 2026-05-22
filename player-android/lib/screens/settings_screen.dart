import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';
import '../providers/current_user_provider.dart';
import '../providers/settings_provider.dart';
import '../providers/theme_provider.dart';

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
///   - The Admin section (Manage Users entry) is shown only when
///     [currentUserProvider] resolves to a user with [User.isAdmin] == true.
///     Non-admin users never see the tile; the server also enforces this via
///     403 on the API endpoints, so the gating is defence-in-depth in the UI.
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

    // Watch the current user to conditionally show the Admin section.
    // currentUserProvider is autoDispose and resolves to null for non-admins.
    final isAdmin = ref.watch(currentUserProvider).valueOrNull?.isAdmin ?? false;

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

              const SizedBox(height: 32),
              const Divider(),
              const SizedBox(height: 24),

              // ----------------------------------------------------------------
              // Appearance section: light / dark / system theme toggle.
              // ----------------------------------------------------------------
              Text(
                'Appearance',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 12),

              const _ThemeToggle(),

              const SizedBox(height: 32),
              const Divider(),
              const SizedBox(height: 24),

              // ----------------------------------------------------------------
              // Sharing section: navigate to MyShares screen.
              // ----------------------------------------------------------------
              Text(
                'Sharing',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 12),

              // My Shares tile — navigates to /shares.
              ListTile(
                key: const Key('settings_my_shares'),
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.link_outlined),
                title: const Text('My Shares'),
                subtitle: const Text('View and revoke your share links'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => context.go(AppRoutes.shares),
              ),

              // Admin section: only visible to admin users.
              // Non-admin users are gated out here; the server enforces this
              // independently via 403 responses, making this defence-in-depth.
              if (isAdmin) const _AdminSection(),
            ],
          ),
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Theme toggle widget
// ---------------------------------------------------------------------------

/// Segmented-button control that lets the user choose between
/// light, dark, and system (follow OS) theme modes.
///
/// Kept as a separate [ConsumerWidget] (SRP) so [_SettingsScreenState] does
/// not need to know about [themeProvider] — it only needs to place the widget.
class _ThemeToggle extends ConsumerWidget {
  const _ThemeToggle();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Default to system while the provider is loading so the toggle renders
    // immediately rather than showing an empty state.
    final current = ref.watch(themeProvider).valueOrNull ?? ThemeMode.system;

    return SegmentedButton<ThemeMode>(
      key: const Key('settings_theme_toggle'),
      segments: const [
        ButtonSegment(
          value: ThemeMode.light,
          icon: Icon(Icons.light_mode_outlined),
          label: Text('Light'),
        ),
        ButtonSegment(
          value: ThemeMode.system,
          icon: Icon(Icons.brightness_auto_outlined),
          label: Text('System'),
        ),
        ButtonSegment(
          value: ThemeMode.dark,
          icon: Icon(Icons.dark_mode_outlined),
          label: Text('Dark'),
        ),
      ],
      selected: {current},
      onSelectionChanged: (selection) {
        // emptySelectionAllowed defaults to false, but guard defensively against future API changes.
        if (selection.isNotEmpty) {
          ref.read(themeProvider.notifier).setThemeMode(selection.first);
        }
      },
    );
  }
}

// ---------------------------------------------------------------------------
// Admin section widget
// ---------------------------------------------------------------------------

/// Administration section shown only to admin users in [SettingsScreen].
///
/// Extracted as a [ConsumerWidget] (following the [_ThemeToggle] pattern) so
/// [_SettingsScreenState.build] does not need to reference [AppRoutes.adminUsers]
/// directly and stays focused on layout concerns.  The server independently
/// enforces admin-only access via 403, so this UI gate is defence-in-depth.
class _AdminSection extends ConsumerWidget {
  const _AdminSection();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const SizedBox(height: 32),
        const Divider(),
        const SizedBox(height: 24),

        Text(
          'Administration',
          style: Theme.of(context).textTheme.titleMedium,
        ),
        const SizedBox(height: 12),

        // Manage Users tile — navigates to /admin/users.
        ListTile(
          key: const Key('settings_manage_users'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.manage_accounts_outlined),
          title: const Text('Manage Users'),
          subtitle: const Text('Create and delete user accounts'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.go(AppRoutes.adminUsers),
        ),

        // Permissions tile — navigates to /admin/permissions.
        ListTile(
          key: const Key('settings_permissions'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.lock_outlined),
          title: const Text('Permissions'),
          subtitle: const Text('Manage set access per user'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.go(AppRoutes.adminPermissions),
        ),

        // Rescan tile — navigates to /admin/rescan.
        ListTile(
          key: const Key('settings_rescan'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.sync_outlined),
          title: const Text('Rescan Library'),
          subtitle: const Text('Trigger a full media library scan'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.go(AppRoutes.adminRescan),
        ),

        // Trash tile — navigates to /admin/trash.
        ListTile(
          key: const Key('settings_trash'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.delete_outline),
          title: const Text('Trash'),
          subtitle: const Text('Restore or permanently delete trashed items'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.go(AppRoutes.adminTrash),
        ),
      ],
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
