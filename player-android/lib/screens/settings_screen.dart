import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../navigation_key.dart';
import '../providers/auth_state_provider.dart';
import '../providers/current_user_provider.dart';
import '../providers/settings_provider.dart';
import '../providers/first_run_provider.dart';
import '../providers/theme_provider.dart';

/// Settings screen: editable server base URL, current username, and logout.
///
/// Design notes:
///   - [ConsumerStatefulWidget] is used so that the text controller can be
///     initialised from the persisted settings and [WidgetRef] is available
///     throughout the async logout path without storing a stale ref.
///   - The base URL is pre-filled from [settingsProvider] and saved on every
///     submit (Enter key or "Save" button).
///   - Logout clears the saved identity via [AuthStateNotifier.logout], which
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
  const SettingsScreen({super.key, this.serverOnly = false});

  /// Public server connection page shown before authentication.
  final bool serverOnly;

  @override
  ConsumerState<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends ConsumerState<SettingsScreen> {
  // Controller for the server base URL text field.  Initialised once from the
  // persisted settings value and disposed when the widget leaves the tree.
  final _urlController = TextEditingController();

  // True while the logout state update is in
  // progress; prevents double-tapping the logout button.
  bool _isLoggingOut = false;

  // Tracks whether the URL controller has been seeded from the loaded settings
  // so we populate it exactly once (on the first non-loading build).
  bool _urlInitialised = false;
  bool _isSavingUrl = false;
  String? _urlError;

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
    try {
      parseServerBaseUrl(url);
      setState(() {
        _isSavingUrl = true;
        _urlError = null;
      });
      // The notifier owns the full transition, even if logout redirects and
      // disposes this screen before the persisted URL write completes.
      await ref.read(authStateProvider.notifier).switchServer(url);
      if (!mounted) return;
      ref.invalidate(firstRunProvider);
      FocusScope.of(context).unfocus();
      if (widget.serverOnly) context.go(AppRoutes.home);
    } on FormatException catch (e) {
      if (mounted) setState(() => _urlError = e.message);
    } catch (_) {
      if (mounted) setState(() => _urlError = 'Could not save server address.');
    } finally {
      if (mounted) setState(() => _isSavingUrl = false);
    }
  }

  // ---------------------------------------------------------------------------
  // Logout logic
  // ---------------------------------------------------------------------------

  /// Clears the saved identity and transitions to the unauthenticated state.
  ///
  /// [AuthStateNotifier.logout] removes the session marker and identity and sets
  /// state to [AuthStatus.unauthenticated].  The router's [refreshListenable]
  /// picks up the change and the redirect callback routes to /login automatically.
  /// The explicit [context.go] below acts as a safety net.
  Future<void> _logout() async {
    setState(() => _isLoggingOut = true);
    try {
      var serverRevoked = false;
      try {
        serverRevoked = await ref.read(authStateProvider.notifier).logout();
      } catch (_) {
        // The notifier still clears local auth on failures; keep the warning
        // and login navigation available if an unexpected error escapes.
      }
      if (!serverRevoked) {
        final messenger = appMessengerKey.currentState ??
            (mounted ? ScaffoldMessenger.maybeOf(context) : null);
        messenger?.showSnackBar(const SnackBar(
          content: Text('Signed out on this device. Server sign-out could not '
              'be confirmed; check API Tokens from another session.'),
        ));
      }
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

    if (widget.serverOnly) {
      return Scaffold(
        appBar: AppBar(title: const Text('Server')),
        body: SafeArea(
          child: SingleChildScrollView(
            padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text('Connect to Player',
                    style: Theme.of(context).textTheme.headlineMedium),
                const SizedBox(height: 24),
                _serverSection(context),
              ],
            ),
          ),
        ),
      );
    }

    final currentUser = ref.watch(currentUserProvider).valueOrNull;
    final isAdmin = currentUser?.isAdmin ?? false;

    final username = currentUser?.username ?? '—';

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

              // API Tokens tile — navigates to /settings/api-tokens.
              // Available to all authenticated users (not admin-only) so they
              // can manage their own Bearer tokens for external integrations.
              ListTile(
                key: const Key('settings_api_tokens'),
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.key_outlined),
                title: const Text('API Tokens'),
                subtitle: const Text('Create and revoke Bearer API tokens'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => context.push(AppRoutes.apiTokens),
              ),

              const SizedBox(height: 24),

              // Logout button: shows a spinner while auth state is cleared.
              _isLoggingOut
                  ? const Center(child: CircularProgressIndicator())
                  : OutlinedButton(
                      key: const Key('settings_logout'),
                      onPressed: _logout,
                      style: OutlinedButton.styleFrom(
                        foregroundColor: Theme.of(context).colorScheme.error,
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
              _serverSection(context),

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
                onTap: () => context.push(AppRoutes.shares),
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

  Widget _serverSection(BuildContext context) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('Server', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 12),
          TextField(
            key: const Key('settings_base_url'),
            controller: _urlController,
            decoration: InputDecoration(
              labelText: 'Server base URL',
              border: const OutlineInputBorder(),
              helperText:
                  'e.g. https://player.example.com or http://10.0.2.2:8080',
              errorText: _urlError,
            ),
            keyboardType: TextInputType.url,
            autocorrect: false,
            textInputAction: TextInputAction.done,
            onSubmitted: (_) => _isSavingUrl ? null : _saveBaseUrl(),
          ),
          const SizedBox(height: 12),
          ElevatedButton(
            key: const Key('settings_save_url'),
            onPressed: _isSavingUrl ? null : _saveBaseUrl,
            child: const Text('Save URL'),
          ),
        ],
      );
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
          onTap: () => context.push(AppRoutes.adminUsers),
        ),

        // Permissions tile — navigates to /admin/permissions.
        ListTile(
          key: const Key('settings_permissions'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.lock_outlined),
          title: const Text('Permissions'),
          subtitle: const Text('Manage set access per user'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push(AppRoutes.adminPermissions),
        ),

        // Rescan tile — navigates to /admin/rescan.
        ListTile(
          key: const Key('settings_rescan'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.sync_outlined),
          title: const Text('Rescan Library'),
          subtitle: const Text('Trigger a full media library scan'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push(AppRoutes.adminRescan),
        ),

        // Trash tile — navigates to /admin/trash.
        ListTile(
          key: const Key('settings_trash'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.delete_outline),
          title: const Text('Trash'),
          subtitle: const Text('Restore or permanently delete trashed items'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push(AppRoutes.adminTrash),
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// File-level helpers
// ---------------------------------------------------------------------------
