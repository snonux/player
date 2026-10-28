import '../l10n/music_localizations.dart';
import '../widgets/language_settings.dart';
import '../widgets/network_settings.dart';
import '../widgets/automatic_music_settings.dart';
import '../widgets/download_quality_picker.dart';
import '../widgets/listening_history_settings.dart';
import '../providers/api_client_provider.dart';
import '../widgets/playback_cache_settings.dart';
import '../widgets/headset_settings.dart';
import '../widgets/music_automation_settings.dart';
import '../widgets/navigation_settings.dart';
import '../widgets/server_endpoint_settings.dart';
import '../widgets/podcast_association.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../widgets/keyboard_access.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../app_routes.dart';
import '../navigation_key.dart';
import '../providers/auth_state_provider.dart';
import '../providers/current_user_provider.dart';
import '../providers/settings_provider.dart';
import '../providers/first_run_provider.dart';
import '../providers/theme_provider.dart';
import '../widgets/presentation_settings.dart';
import '../library_navigation.dart';
import 'server_plugins_screen.dart';

/// Settings screen: short category destinations for account, appearance,
/// headphones, automation, music/offline, servers, sharing, and admin.
///
/// Design notes:
///   - [ConsumerStatefulWidget] keeps the public [serverOnly] connection page
///     and shared navigation helpers without embedding every control in one
///     scrolling column.
///   - Category tiles push a detail page so common groups are easy to find;
///     every advanced control and disconnected/server variant is preserved.
///   - Logout clears the saved identity via [AuthStateNotifier.logout], which
///     triggers go_router's redirect callback (via [refreshListenable]) and
///     navigates to /login automatically.  An explicit [context.go] acts as a
///     safety net in case the redirect has not fired yet.
///   - The Administration category is shown only when [currentUserProvider]
///     resolves to a user with [User.isAdmin] == true.
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
  late final _packageInfo = PackageInfo.fromPlatform();

  @override
  Widget build(BuildContext context) {
    if (widget.serverOnly) {
      return Scaffold(
        appBar: AppBar(title: const Text('Server')),
        body: SafeArea(
          child: SingleChildScrollView(
            padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text('Choose a server',
                    style: Theme.of(context).textTheme.headlineMedium),
                const SizedBox(height: 24),
                const _ServerConnectionPanel(showSavedServers: true),
                TextButton(
                  onPressed: _openLocalLibrary,
                  child: const Text('Use files on this device'),
                ),
              ],
            ),
          ),
        ),
      );
    }

    final settingsAsync = ref.watch(settingsProvider);
    final destination = settingsAsync.valueOrNull?.destination;
    final currentUser = destination == LibraryDestination.server
        ? ref.watch(currentUserProvider).valueOrNull
        : null;
    final isAdmin = currentUser?.isAdmin ?? false;
    final onServer = destination == LibraryDestination.server;

    return Scaffold(
      appBar: AppBar(title: Text(context.tr('Settings'))),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              ListTile(
                leading: const Icon(Icons.keyboard),
                title: const Text('Keyboard shortcuts'),
                subtitle: const Text(
                    'With a keyboard: ; or F2 shows underlined control shortcuts. Escape cancels.'),
                onTap: () => KeyboardAccess.showHints(context),
              ),
              ListTile(
                key: const Key('settings_local_library'),
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.phone_android),
                title: const Text('On this device'),
                subtitle: const Text('Switch to files stored on this device'),
                trailing: const Icon(Icons.chevron_right),
                onTap: _openLocalLibrary,
              ),
              const PodcastAssociationSettings(),
              if (!onServer)
                ListTile(
                  key: const Key('settings_connect_server'),
                  contentPadding: EdgeInsets.zero,
                  leading: const Icon(Icons.cloud_outlined),
                  title: const Text('Connect to server'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push(AppRoutes.server),
                ),
              if (onServer)
                _SettingsCategoryTile(
                  key: const Key('settings_category_account'),
                  icon: Icons.person_outline,
                  title: 'Account',
                  subtitle: currentUser?.username ?? 'Signed in',
                  onTap: () => _openCategory(
                    context,
                    title: 'Account',
                    child: _AccountSettingsPanel(
                      username: currentUser?.username ?? '—',
                    ),
                  ),
                ),
              _SettingsCategoryTile(
                key: const Key('settings_category_appearance'),
                icon: Icons.palette_outlined,
                title: 'Appearance',
                subtitle: 'Theme, language, artwork',
                onTap: () => _openCategory(
                  context,
                  title: 'Appearance',
                  child: const Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      _ThemeToggle(),
                      LanguageSettings(),
                      PresentationSettings(),
                    ],
                  ),
                ),
              ),
              _SettingsCategoryTile(
                key: const Key('settings_category_headphones'),
                icon: Icons.headphones_outlined,
                title: 'Headphones',
                subtitle: 'Pause and resume with headset connect',
                onTap: () => _openCategory(
                  context,
                  title: 'Headphones',
                  child: const HeadsetSettings(),
                ),
              ),
              _SettingsCategoryTile(
                key: const Key('settings_category_automation'),
                icon: Icons.auto_mode_outlined,
                title: 'Automation',
                subtitle: 'Tasker / Locale music actions',
                onTap: () => _openCategory(
                  context,
                  title: 'Automation',
                  child: const MusicAutomationSettings(),
                ),
              ),
              if (onServer)
                _SettingsCategoryTile(
                  key: const Key('settings_category_music'),
                  icon: Icons.library_music_outlined,
                  title: 'Music & offline',
                  subtitle:
                      'Navigation, network, cache, downloads, history',
                  onTap: () {
                    final historyKey =
                        'history:${ref.read(playerBaseUrlProvider).origin}:${currentUser?.id}';
                    final api = ref.read(apiClientProvider);
                    _openCategory(
                      context,
                      title: 'Music & offline',
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          const NavigationSettings(),
                          const MusicNetworkSettings(),
                          const AutomaticMusicSettings(),
                          const PlaybackCacheSettings(),
                          ListTile(
                            contentPadding: EdgeInsets.zero,
                            leading: const Icon(Icons.download_outlined),
                            title: const Text('Download quality'),
                            subtitle: const Text(
                                'Saved separately from streaming quality'),
                            trailing: const Icon(Icons.chevron_right),
                            onTap: () =>
                                showDownloadQualitySheet(context, api),
                          ),
                          ListeningHistorySettings(key: ValueKey(historyKey)),
                        ],
                      ),
                    );
                  },
                ),
              _SettingsCategoryTile(
                key: const Key('settings_category_servers'),
                icon: Icons.dns_outlined,
                title: 'Servers',
                subtitle: 'Base URL and connection profiles',
                onTap: () => _openCategory(
                  context,
                  title: 'Servers',
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      if (settingsAsync.valueOrNull?.configurationError !=
                          null)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 12),
                          child: Text(
                            settingsAsync.valueOrNull!.configurationError!,
                            style: TextStyle(
                                color: Theme.of(context).colorScheme.error),
                          ),
                        ),
                      const _ServerConnectionPanel(showSavedServers: true),
                    ],
                  ),
                ),
              ),
              if (onServer) ...[
                _SettingsCategoryTile(
                  key: const Key('settings_category_sharing'),
                  icon: Icons.link_outlined,
                  title: 'Sharing',
                  subtitle: 'View and revoke share links',
                  onTap: () => _openCategory(
                    context,
                    title: 'Sharing',
                    child: ListTile(
                      key: const Key('settings_my_shares'),
                      contentPadding: EdgeInsets.zero,
                      leading: const Icon(Icons.link_outlined),
                      title: const Text('My Shares'),
                      subtitle:
                          const Text('View and revoke your share links'),
                      trailing: const Icon(Icons.chevron_right),
                      onTap: () => context.push(AppRoutes.shares),
                    ),
                  ),
                ),
                if (isAdmin)
                  _SettingsCategoryTile(
                    key: const Key('settings_category_administration'),
                    icon: Icons.admin_panel_settings_outlined,
                    title: 'Administration',
                    subtitle: 'Users, permissions, library tools',
                    onTap: () => _openCategory(
                      context,
                      title: 'Administration',
                      child: const _AdminSection(),
                    ),
                  ),
              ],
              const SizedBox(height: 32),
              const Divider(),
              FutureBuilder<PackageInfo>(
                future: _packageInfo,
                builder: (context, snapshot) {
                  final info = snapshot.data;
                  return ListTile(
                    key: const Key('settings_app_version'),
                    contentPadding: EdgeInsets.zero,
                    leading: const Icon(Icons.info_outline),
                    title: const Text('Player'),
                    subtitle: Text(info != null
                        ? 'Version ${info.version} (build ${info.buildNumber})'
                        : snapshot.hasError
                            ? 'Version unavailable'
                            : 'Loading version…'),
                  );
                },
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _openLocalLibrary() async {
    await switchToLocalLibrary(ref, context);
  }

  void _openCategory(
    BuildContext context, {
    required String title,
    required Widget child,
  }) {
    Navigator.of(context).push(MaterialPageRoute<void>(
      builder: (_) => _SettingsCategoryPage(title: title, child: child),
    ));
  }
}

// ---------------------------------------------------------------------------
// Category chrome
// ---------------------------------------------------------------------------

class _SettingsCategoryTile extends StatelessWidget {
  const _SettingsCategoryTile({
    super.key,
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.onTap,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      contentPadding: EdgeInsets.zero,
      leading: Icon(icon),
      title: Text(title),
      subtitle: Text(subtitle),
      trailing: const Icon(Icons.chevron_right),
      onTap: onTap,
    );
  }
}

class _SettingsCategoryPage extends StatelessWidget {
  const _SettingsCategoryPage({required this.title, required this.child});

  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(title)),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
          child: child,
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Account category
// ---------------------------------------------------------------------------

class _AccountSettingsPanel extends ConsumerStatefulWidget {
  const _AccountSettingsPanel({required this.username});

  final String username;

  @override
  ConsumerState<_AccountSettingsPanel> createState() =>
      _AccountSettingsPanelState();
}

class _AccountSettingsPanelState extends ConsumerState<_AccountSettingsPanel> {
  bool _isLoggingOut = false;

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
      if (mounted) context.go(AppRoutes.login);
    } finally {
      if (mounted) setState(() => _isLoggingOut = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
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
                  widget.username,
                  key: const Key('settings_username'),
                  style: Theme.of(context).textTheme.bodyLarge,
                ),
              ],
            ),
          ],
        ),
        const SizedBox(height: 24),
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
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// Servers category / public server-only page
// ---------------------------------------------------------------------------

class _ServerConnectionPanel extends ConsumerStatefulWidget {
  const _ServerConnectionPanel({required this.showSavedServers});

  final bool showSavedServers;

  @override
  ConsumerState<_ServerConnectionPanel> createState() =>
      _ServerConnectionPanelState();
}

class _ServerConnectionPanelState
    extends ConsumerState<_ServerConnectionPanel> {
  final _urlController = TextEditingController();
  bool _urlInitialised = false;
  bool _isSavingUrl = false;
  String? _urlError;

  @override
  void dispose() {
    _urlController.dispose();
    super.dispose();
  }

  Future<void> _saveBaseUrl() async {
    final url = _urlController.text.trim();
    try {
      parseServerBaseUrl(url);
      setState(() {
        _isSavingUrl = true;
        _urlError = null;
      });
      await ref.read(authStateProvider.notifier).switchServer(url);
      if (!mounted) return;
      ref.invalidate(firstRunProvider);
      FocusScope.of(context).unfocus();
      context.go(AppRoutes.login);
    } on FormatException catch (e) {
      if (mounted) setState(() => _urlError = e.message);
    } catch (_) {
      if (mounted) setState(() => _urlError = 'Could not save server address.');
    } finally {
      if (mounted) setState(() => _isSavingUrl = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final settingsAsync = ref.watch(settingsProvider);
    settingsAsync.whenData((settings) {
      if (!_urlInitialised) {
        _urlController.text = settings.serverBaseUrl ?? kPlayerBaseUrl;
        _urlInitialised = true;
      }
    });

    final servers = settingsAsync.valueOrNull?.savedServers ?? [];

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text('Servers', style: Theme.of(context).textTheme.titleMedium),
        const ServerEndpointSettings(),
        if (widget.showSavedServers)
          for (final server in servers)
            ListTile(
              leading: const Icon(Icons.dns_outlined),
              title: Text(server),
              trailing: const Icon(Icons.chevron_right),
              onTap: _isSavingUrl
                  ? null
                  : () {
                      _urlController.text = server;
                      _saveBaseUrl();
                    },
            ),
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
}

// ---------------------------------------------------------------------------
// Theme toggle widget
// ---------------------------------------------------------------------------

/// Segmented-button control that lets the user choose between
/// light, dark, and system (follow OS) theme modes.
class _ThemeToggle extends ConsumerWidget {
  const _ThemeToggle();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
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

/// Administration controls shown inside the Administration category.
class _AdminSection extends ConsumerWidget {
  const _AdminSection();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ListTile(
          key: const Key('settings_server_plugins'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.extension_outlined),
          title: const Text('Server plugins'),
          subtitle: const Text('Advanced: optional server extensions'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => Navigator.of(context).push(MaterialPageRoute<void>(
              builder: (_) => const ServerPluginsScreen())),
        ),
        ListTile(
          key: const Key('settings_manage_users'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.manage_accounts_outlined),
          title: const Text('Manage Users'),
          subtitle: const Text('Create and delete user accounts'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push(AppRoutes.adminUsers),
        ),
        ListTile(
          key: const Key('settings_permissions'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.lock_outlined),
          title: const Text('Permissions'),
          subtitle: const Text('Manage set access per user'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push(AppRoutes.adminPermissions),
        ),
        ListTile(
          key: const Key('settings_rescan'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.sync_outlined),
          title: const Text('Rescan Library'),
          subtitle: const Text('Trigger a full media library scan'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push(AppRoutes.adminRescan),
        ),
        ListTile(
          key: const Key('settings_trash'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.delete_outline),
          title: const Text('Trash'),
          subtitle: const Text('Restore or permanently delete trashed items'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push(AppRoutes.adminTrash),
        ),
        ListTile(
          key: const Key('settings_libraries'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.library_music_outlined),
          title: const Text('Libraries'),
          subtitle: const Text('Advanced: music roots and library access'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push(AppRoutes.adminLibraries),
        ),
        ListTile(
          key: const Key('settings_music_identity'),
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.fingerprint_outlined),
          title: const Text('Music identity'),
          subtitle: const Text('Advanced: resolve ambiguous song matches'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push(AppRoutes.adminMusicIdentity),
        ),
      ],
    );
  }
}
