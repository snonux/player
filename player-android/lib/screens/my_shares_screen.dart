import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// MyShares screen: lists all share links created by the authenticated user.
///
/// Design notes:
///   - [ConsumerStatefulWidget] is used so local list state can be managed
///     and [WidgetRef] is available for async API calls with [mounted] guards.
///   - The share list is stored locally: [_shares] is null during the initial
///     load (spinner shown), non-null after first successful fetch.
///   - Revoke is optimistic: the share is removed from the list immediately,
///     then the API call is made.  On error the item is re-inserted at its
///     original position and an error SnackBar is shown.
///   - Copy-link calls [shareUrl] on the client (no HTTP call) and writes to
///     the clipboard, then shows a confirmation SnackBar.
///   - All async continuations guard on [mounted] to prevent setState/context
///     calls after widget disposal.
class MySharesScreen extends ConsumerStatefulWidget {
  const MySharesScreen({super.key});

  @override
  ConsumerState<MySharesScreen> createState() => _MySharesScreenState();
}

class _MySharesScreenState extends ConsumerState<MySharesScreen> {
  // Null while the initial load is in-flight; empty list when server returns [].
  List<Share>? _shares;

  // Non-null when the last load attempt failed.
  String? _error;

  // True while the initial or refresh load is in flight.
  bool _isLoading = false;

  @override
  void initState() {
    super.initState();
    // Defer first load until after the first frame so provider overrides in
    // tests are applied before [ref] is used.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches the user's share list and updates local state.
  ///
  /// Called on first mount and on pull-to-refresh.  Errors are mapped by the
  /// top-level [sharesErrorMessage] helper so the widget stays simple.
  Future<void> _load() async {
    if (!mounted) return;
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final client = ref.read(apiClientProvider);
      final shares = await client.listMyShares();
      if (!mounted) return;
      setState(() {
        _shares = shares;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = sharesErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Copy-link action
  // ---------------------------------------------------------------------------

  /// Copies the public share URL for [share] to the clipboard and shows a
  /// confirmation SnackBar.
  ///
  /// [shareUrl] is a pure URL-builder on [PlayerApiClient] (no HTTP call).
  void _copyLink(Share share) {
    final client = ref.read(apiClientProvider);
    final url = client.shareUrl(share.token);
    Clipboard.setData(ClipboardData(text: url));
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        key: const Key('shares_copy_snackbar'),
        content: Text('Link copied: $url'),
        duration: const Duration(seconds: 4),
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Revoke action
  // ---------------------------------------------------------------------------

  /// Optimistically removes [share] from the list, calls [revokeShare], and
  /// reverts on error.
  ///
  /// The optimistic removal keeps the UI responsive: the row disappears
  /// immediately.  If the API call fails the item is re-inserted at [index]
  /// so the list is consistent with the server state.
  Future<void> _revoke(Share share, int index) async {
    // Optimistic removal.
    setState(() => _shares!.removeAt(index));

    try {
      final client = ref.read(apiClientProvider);
      await client.revokeShare(share.token);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          key: Key('shares_revoke_snackbar'),
          content: Text('Share revoked.'),
          duration: Duration(seconds: 3),
        ),
      );
    } catch (e) {
      // Revert optimistic removal on error.
      if (!mounted) return;
      setState(() => _shares!.insert(index, share));
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          key: const Key('shares_revoke_error_snackbar'),
          content: Text(sharesErrorMessage(e)),
          backgroundColor: Theme.of(context).colorScheme.error,
        ),
      );
    }
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('My Shares')),
      body: _buildBody(context),
    );
  }

  /// Builds the main body, delegating to the appropriate state widget:
  ///   - Full-screen spinner on the very first load (no data yet).
  ///   - Error view with a retry button when the load failed.
  ///   - Pull-to-refresh wrapper around the share list or empty-state view.
  Widget _buildBody(BuildContext context) {
    if (_isLoading && _shares == null) {
      return const Center(
        key: Key('shares_loading'),
        child: CircularProgressIndicator(),
      );
    }

    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: _shares == null || _shares!.isEmpty
          ? const _EmptyView()
          : _ShareList(
              shares: _shares!,
              onCopyLink: _copyLink,
              onRevoke: _revoke,
            ),
    );
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Scrollable list of [Share] rows.
///
/// Extracted into its own stateless widget so [_MySharesScreenState] stays
/// focused on data-loading concerns and the list UI is independently testable.
class _ShareList extends StatelessWidget {
  const _ShareList({
    required this.shares,
    required this.onCopyLink,
    required this.onRevoke,
  });

  final List<Share> shares;
  final void Function(Share share) onCopyLink;
  final Future<void> Function(Share share, int index) onRevoke;

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      key: const Key('shares_list'),
      itemCount: shares.length,
      separatorBuilder: (_, __) => const Divider(height: 1),
      itemBuilder: (context, index) {
        final share = shares[index];
        return _ShareTile(
          share: share,
          index: index,
          onCopyLink: onCopyLink,
          onRevoke: onRevoke,
        );
      },
    );
  }
}

/// A single share row with copy-link and revoke actions.
///
/// Shows the filename (falling back to media-ID when absent), the expiry date
/// (or "No expiry" for non-expiring shares), and the used/max-uses count.
/// A trailing icon row provides copy-link and revoke buttons.
class _ShareTile extends StatelessWidget {
  const _ShareTile({
    required this.share,
    required this.index,
    required this.onCopyLink,
    required this.onRevoke,
  });

  final Share share;
  final int index;
  final void Function(Share share) onCopyLink;
  final Future<void> Function(Share share, int index) onRevoke;

  @override
  Widget build(BuildContext context) {
    final title = share.fileName ?? 'Media #${share.mediaId}';
    final expiry = _formatExpiry(share.expiresAt);
    final uses = share.maxUses != null
        ? '${share.usedCount}/${share.maxUses} uses'
        : '${share.usedCount} uses';

    return ListTile(
      key: Key('share_tile_${share.token}'),
      title: Text(
        title,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: Text('$expiry · $uses'),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          // Copy-link: writes the share URL to the clipboard.
          IconButton(
            key: Key('share_copy_${share.token}'),
            icon: const Icon(Icons.copy_outlined),
            tooltip: 'Copy link',
            onPressed: () => onCopyLink(share),
          ),
          // Revoke: removes the share optimistically.
          IconButton(
            key: Key('share_revoke_${share.token}'),
            icon: const Icon(Icons.delete_outline),
            tooltip: 'Revoke share',
            onPressed: () => onRevoke(share, index),
          ),
        ],
      ),
    );
  }

  /// Formats [expiresAt] as "Expires YYYY-MM-DD" or "No expiry".
  ///
  /// Kept as a pure static helper so it can be called without a [BuildContext]
  /// and is easy to unit-test in isolation.
  static String _formatExpiry(DateTime? expiresAt) {
    if (expiresAt == null) return 'No expiry';
    final y = expiresAt.year.toString();
    final m = expiresAt.month.toString().padLeft(2, '0');
    final d = expiresAt.day.toString().padLeft(2, '0');
    return 'Expires $y-$m-$d';
  }
}

/// Full-screen empty-state view shown when [listMyShares] returns [].
///
/// Wrapped in a [ListView] with [AlwaysScrollableScrollPhysics] so the parent
/// [RefreshIndicator] can still trigger pull-to-refresh even with no content.
class _EmptyView extends StatelessWidget {
  const _EmptyView();

  @override
  Widget build(BuildContext context) {
    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      children: [
        SizedBox(
          height: MediaQuery.of(context).size.height * 0.6,
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(
                Icons.link_off,
                size: 72,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
              const SizedBox(height: 16),
              Text(
                'No shares yet',
                key: const Key('shares_empty'),
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 8),
              Text(
                'Share links you create will appear here.',
                style: Theme.of(context).textTheme.bodySmall,
                textAlign: TextAlign.center,
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// Full-screen error view with a retry button.
///
/// Shown when [listMyShares] throws (network error, server error, etc.).
/// The [message] comes from [sharesErrorMessage], which maps exceptions to
/// human-readable strings.
class _ErrorView extends StatelessWidget {
  const _ErrorView({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(
              Icons.error_outline,
              size: 56,
              color: Theme.of(context).colorScheme.error,
            ),
            const SizedBox(height: 16),
            Text(
              message,
              key: const Key('shares_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('shares_retry'),
              onPressed: onRetry,
              icon: const Icon(Icons.refresh),
              label: const Text('Retry'),
            ),
          ],
        ),
      ),
    );
  }
}
