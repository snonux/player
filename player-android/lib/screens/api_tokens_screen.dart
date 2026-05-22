import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// Data model
// ---------------------------------------------------------------------------

/// Lightweight view-model for an API token row.
///
/// Uses raw map fields from the server rather than a dedicated model class
/// because API tokens have no corresponding model in models.dart, and adding
/// one solely for this screen would violate YAGNI.  The map is unwrapped once
/// here and never propagated further into the UI (Law of Demeter).
class _TokenRow {
  const _TokenRow({
    required this.id,
    required this.name,
    required this.createdAt,
    this.expiresAt,
  });

  final int id;
  final String name;

  /// ISO-8601 creation timestamp from the server response.
  final String createdAt;

  /// ISO-8601 expiry timestamp, or null for a non-expiring token.
  final String? expiresAt;

  /// Parses a raw JSON map from `listAPITokens` / `createAPIToken` into a row.
  ///
  /// The `createAPIToken` response carries `token` (plaintext) but not
  /// `created_at`; callers should pass a fallback timestamp in that case.
  factory _TokenRow.fromMap(Map<String, dynamic> map, {String? createdAtFallback}) {
    return _TokenRow(
      id: (map['id'] as num).toInt(),
      name: (map['name'] as String?) ?? '',
      createdAt: (map['created_at'] as String?) ?? createdAtFallback ?? '',
      expiresAt: map['expires_at'] as String?,
    );
  }

  /// Returns a display-friendly date string (YYYY-MM-DD) from an ISO-8601
  /// timestamp, or an empty string if the value is absent or malformed.
  static String _shortDate(String? iso) {
    if (iso == null || iso.isEmpty) return '';
    // Truncate after the date portion; ignore timezone offsets for display.
    return iso.length >= 10 ? iso.substring(0, 10) : iso;
  }

  String get createdAtDisplay => _shortDate(createdAt);
  String get expiresAtDisplay => expiresAt != null ? _shortDate(expiresAt) : 'No expiry';
}

// ---------------------------------------------------------------------------
// Main screen
// ---------------------------------------------------------------------------

/// Screen for managing the authenticated user's API tokens.
///
/// Design notes:
///   - Any authenticated user can manage their own tokens; this screen is in
///     the Account section of Settings, not the Admin section.
///   - Create uses optimistic UI with index-based slot tracking: the placeholder
///     is inserted at [_tokens!.length] before the API call; on success the
///     real row replaces that slot; on error the slot is removed.
///   - Revoke uses identity-based optimistic removal:
///     `_tokens!.removeWhere((t) => t.id == token.id)` first, then appended
///     back on error (mirrors AdminUsersScreen; append avoids unsafe index-based re-insert).
///   - The plaintext token from `createAPIToken` is shown exactly once in a
///     dialog with a copy button; after the user taps Done it is discarded.
///   - All async continuations guard on [mounted] to prevent setState / context
///     calls after widget disposal.
class ApiTokensScreen extends ConsumerStatefulWidget {
  const ApiTokensScreen({super.key});

  @override
  ConsumerState<ApiTokensScreen> createState() => _ApiTokensScreenState();
}

class _ApiTokensScreenState extends ConsumerState<ApiTokensScreen> {
  // Null while the initial load is in flight; non-null after first success.
  List<_TokenRow>? _tokens;

  // Non-null when the last load attempt failed.
  String? _error;

  // True while a load (initial or refresh) is in flight.
  bool _isLoading = false;

  // Generation counter: stale async completions are silently discarded.
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    // Defer until after first frame so provider overrides in tests are applied.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches the full token list and refreshes local state.
  ///
  /// Increments [_generation] so in-flight results from a previous call are
  /// silently dropped if a newer call starts first (stale-result guard).
  Future<void> _load() async {
    if (!mounted) return;
    final generation = ++_generation;

    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final rawList = await ref.read(apiClientProvider).listAPITokens();
      if (!mounted || generation != _generation) return;
      setState(() {
        _tokens = rawList.map((m) => _TokenRow.fromMap(m)).toList();
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _error = apiTokenErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Create token action
  // ---------------------------------------------------------------------------

  /// Opens the create dialog; on confirm submits the request and shows the
  /// plaintext token once.
  Future<void> _showCreateDialog() async {
    final result = await showDialog<_CreateTokenInput>(
      context: context,
      builder: (_) => const _CreateTokenDialog(),
    );
    if (result == null || !mounted) return;

    await _submitCreateToken(result);
  }

  /// Submits the create request with optimistic placeholder insertion.
  ///
  /// Index-based slot tracking ensures the placeholder is replaced (or removed
  /// on error) at the exact position it was inserted, even if a concurrent
  /// refresh or mutation changes the list length while the request is in flight.
  Future<void> _submitCreateToken(_CreateTokenInput input) async {
    // Capture the slot index before inserting the placeholder so both the
    // success and error paths operate on the same position.
    final placeholderIdx = _tokens?.length ?? 0;
    final placeholder = _TokenRow(
      id: 0,
      name: input.name,
      createdAt: '',
    );
    setState(() => _tokens = [...?_tokens, placeholder]);

    try {
      final raw = await ref.read(apiClientProvider).createAPIToken(
            name: input.name,
            expiresInDays: input.expiresInDays,
          );
      if (!mounted) return;

      final created = _TokenRow.fromMap(
        raw,
        createdAtFallback: DateTime.now().toUtc().toIso8601String(),
      );
      // Replace the placeholder slot with the real row from the server.
      // Bounds-check guards against a concurrent refresh that shrank the list.
      setState(() {
        if (placeholderIdx < (_tokens?.length ?? 0)) {
          _tokens![placeholderIdx] = created;
        }
      });

      // Show the plaintext token exactly once; the user must copy it before
      // tapping Done because it will never be returned by the API again.
      final plaintext = raw['token'] as String? ?? '';
      if (mounted && plaintext.isNotEmpty) {
        await _showPlaintextDialog(plaintext);
      }
    } catch (e) {
      if (!mounted) return;
      // Revert optimistic insertion by removing the placeholder slot.
      setState(() {
        if (placeholderIdx < (_tokens?.length ?? 0)) {
          _tokens!.removeAt(placeholderIdx);
        }
      });
      _showError(apiTokenErrorMessage(e));
    }
  }

  // ---------------------------------------------------------------------------
  // Plaintext token display
  // ---------------------------------------------------------------------------

  /// Shows the one-time plaintext token with a copy button.
  ///
  /// Called immediately after a successful create so the user can copy the
  /// token before it disappears.  The dialog blocks navigation until dismissed.
  Future<void> _showPlaintextDialog(String plaintext) async {
    await showDialog<void>(
      context: context,
      barrierDismissible: false, // force explicit Done to acknowledge loss
      builder: (ctx) => _PlaintextTokenDialog(plaintext: plaintext),
    );
  }

  // ---------------------------------------------------------------------------
  // Revoke token action
  // ---------------------------------------------------------------------------

  /// Shows a confirmation dialog, then revokes the token if confirmed.
  ///
  /// Identity-based optimistic removal: the token row is removed from the list
  /// immediately, then the API call is made.  On error the row is appended back
  /// (mirrors AdminUsersScreen, which also appends on revert).
  Future<void> _revokeToken(_TokenRow token) async {
    final confirmed = await _confirmRevoke(token.name);
    if (!confirmed || !mounted) return;

    // Optimistic removal by identity so the UI responds instantly.
    setState(() => _tokens!.removeWhere((t) => t.id == token.id));

    try {
      await ref.read(apiClientProvider).revokeAPIToken(token.id);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          key: const Key('api_tokens_revoke_snackbar'),
          content: Text('Token "${token.name}" revoked.'),
          duration: const Duration(seconds: 3),
        ),
      );
    } catch (e) {
      if (!mounted) return;
      // Re-append the token to restore the list after the failed revoke.
      // Append rather than re-insert at original index to match AdminUsersScreen;
      // index-based re-insert is unsafe if concurrent loads replace _tokens.
      setState(() => _tokens = [...?_tokens, token]);
      _showError(apiTokenErrorMessage(e));
    }
  }

  /// Shows a confirmation [AlertDialog] before revoking [tokenName].
  ///
  /// Returns true only when the user taps "Revoke".
  Future<bool> _confirmRevoke(String tokenName) async {
    final result = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Revoke token'),
        content: Text('Revoke "$tokenName"? It will stop working immediately.'),
        actions: [
          TextButton(
            key: const Key('api_tokens_confirm_cancel'),
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel'),
          ),
          TextButton(
            key: const Key('api_tokens_confirm_revoke'),
            style: TextButton.styleFrom(
              foregroundColor: Theme.of(ctx).colorScheme.error,
            ),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Revoke'),
          ),
        ],
      ),
    );
    return result ?? false;
  }

  // ---------------------------------------------------------------------------
  // Error display
  // ---------------------------------------------------------------------------

  void _showError(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        key: const Key('api_tokens_error_snackbar'),
        content: Text(message),
        backgroundColor: Theme.of(context).colorScheme.error,
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('API Tokens'),
        actions: [
          IconButton(
            key: const Key('api_tokens_refresh'),
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
            onPressed: _load,
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton(
        key: const Key('api_tokens_fab'),
        tooltip: 'Create token',
        onPressed: _showCreateDialog,
        child: const Icon(Icons.add),
      ),
      body: _buildBody(context),
    );
  }

  /// Builds the appropriate body widget for the current data/error/loading state.
  Widget _buildBody(BuildContext context) {
    if (_isLoading && _tokens == null) {
      return const Center(
        key: Key('api_tokens_loading'),
        child: CircularProgressIndicator(),
      );
    }

    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: _tokens == null || _tokens!.isEmpty
          ? const _EmptyView()
          : _TokenList(tokens: _tokens!, onRevoke: _revokeToken),
    );
  }
}

// ---------------------------------------------------------------------------
// Token list
// ---------------------------------------------------------------------------

/// Scrollable list of [_TokenRow] tiles.
///
/// Extracted as a separate stateless widget (SRP) so the state class stays
/// focused on data-loading and mutation concerns.
class _TokenList extends StatelessWidget {
  const _TokenList({required this.tokens, required this.onRevoke});

  final List<_TokenRow> tokens;
  final Future<void> Function(_TokenRow token) onRevoke;

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      key: const Key('api_tokens_list'),
      itemCount: tokens.length,
      separatorBuilder: (_, __) => const Divider(height: 1),
      itemBuilder: (_, index) {
        final token = tokens[index];
        return _TokenTile(token: token, onRevoke: onRevoke);
      },
    );
  }
}

/// A single token row showing name, created date, expiry, and a revoke button.
class _TokenTile extends StatelessWidget {
  const _TokenTile({required this.token, required this.onRevoke});

  final _TokenRow token;
  final Future<void> Function(_TokenRow token) onRevoke;

  @override
  Widget build(BuildContext context) {
    // Use id=0 key for placeholders (optimistic rows not yet confirmed by server).
    return ListTile(
      key: Key('api_token_tile_${token.id}'),
      leading: const Icon(Icons.key_outlined),
      title: Text(token.name),
      subtitle: _buildSubtitle(context),
      trailing: IconButton(
        key: Key('api_token_revoke_${token.id}'),
        icon: const Icon(Icons.delete_outline),
        tooltip: 'Revoke token',
        color: Theme.of(context).colorScheme.error,
        // Disable the revoke button on optimistic placeholder rows (id == 0)
        // to prevent double-revoke while the create request is still in flight.
        onPressed: token.id == 0 ? null : () => onRevoke(token),
      ),
    );
  }

  /// Builds the subtitle with created date and expiry information.
  Widget _buildSubtitle(BuildContext context) {
    final created = token.createdAtDisplay;
    final expiry = token.expiresAtDisplay;
    final parts = <String>[
      if (created.isNotEmpty) 'Created: $created',
      'Expires: $expiry',
    ];
    return Text(parts.join('  ·  '));
  }
}

// ---------------------------------------------------------------------------
// Empty and error views
// ---------------------------------------------------------------------------

/// Full-screen empty-state shown when no tokens exist.
///
/// Wrapped in a scrollable so the parent [RefreshIndicator] works even without
/// content present (mirrors the pattern used in AdminUsersScreen).
class _EmptyView extends StatelessWidget {
  const _EmptyView();

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        child: SizedBox(
          height: constraints.maxHeight,
          child: Center(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  Icons.key_off_outlined,
                  size: 72,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
                const SizedBox(height: 16),
                Text(
                  'No API tokens',
                  key: const Key('api_tokens_empty'),
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 8),
                Text(
                  'Tap + to create one',
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Full-screen error view with a retry button.
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
              key: const Key('api_tokens_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('api_tokens_retry'),
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

// ---------------------------------------------------------------------------
// Plaintext token dialog
// ---------------------------------------------------------------------------

/// Dialog that shows the one-time plaintext token value with a clipboard copy
/// button and a Done button.
///
/// [barrierDismissible] is set to false at the call-site so the user must
/// explicitly acknowledge that the token will not be shown again.
class _PlaintextTokenDialog extends StatelessWidget {
  const _PlaintextTokenDialog({required this.plaintext});

  final String plaintext;

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      key: const Key('api_tokens_plaintext_dialog'),
      title: const Text('Your new token'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Copy this token now — it will not be shown again.',
            style: TextStyle(fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 16),
          // Display the token in a selectable text widget so the user can also
          // long-press to select and copy via the OS text menu.
          SelectableText(
            plaintext,
            key: const Key('api_tokens_plaintext_value'),
            style: const TextStyle(fontFamily: 'monospace'),
          ),
        ],
      ),
      actions: [
        TextButton.icon(
          key: const Key('api_tokens_plaintext_copy'),
          icon: const Icon(Icons.copy_outlined),
          label: const Text('Copy'),
          onPressed: () async {
            await Clipboard.setData(ClipboardData(text: plaintext));
            if (context.mounted) {
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(
                  key: Key('api_tokens_copy_snackbar'),
                  content: Text('Token copied to clipboard.'),
                  duration: Duration(seconds: 2),
                ),
              );
            }
          },
        ),
        FilledButton(
          key: const Key('api_tokens_plaintext_done'),
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Done'),
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// Create token dialog
// ---------------------------------------------------------------------------

/// Input value returned by [_CreateTokenDialog] when the user confirms.
class _CreateTokenInput {
  const _CreateTokenInput({required this.name, this.expiresInDays});

  final String name;

  /// Null means the token never expires.
  final int? expiresInDays;
}

/// Dialog for creating a new API token.
///
/// Collects a required token name and an optional expiry date.
/// Validation is inline so the user gets immediate feedback without a round-trip.
///
/// Uses [StatefulWidget] because the dialog makes no API calls itself — the
/// parent [_ApiTokensScreenState] handles the network request (SRP).
class _CreateTokenDialog extends StatefulWidget {
  const _CreateTokenDialog();

  @override
  State<_CreateTokenDialog> createState() => _CreateTokenDialogState();
}

class _CreateTokenDialogState extends State<_CreateTokenDialog> {
  final _formKey = GlobalKey<FormState>();
  final _nameController = TextEditingController();

  // The selected expiry date, or null for a non-expiring token.
  DateTime? _expiryDate;

  @override
  void dispose() {
    _nameController.dispose();
    super.dispose();
  }

  /// Validates the form and pops with a [_CreateTokenInput] on success.
  void _submit() {
    if (_formKey.currentState?.validate() != true) return;

    // Calculate the number of days from today to the chosen expiry date so the
    // server can compute the absolute expiry timestamp using its own clock.
    // Using ceiling division ensures the token does not expire before the
    // selected date in any timezone.
    int? expiresInDays;
    if (_expiryDate != null) {
      final now = DateTime.now();
      final diff = _expiryDate!.difference(DateTime(now.year, now.month, now.day));
      expiresInDays = diff.inDays.clamp(1, 36500); // 1 day to 100 years
    }

    Navigator.of(context).pop(
      _CreateTokenInput(
        name: _nameController.text.trim(),
        expiresInDays: expiresInDays,
      ),
    );
  }

  /// Opens the date picker and stores the selected date in [_expiryDate].
  Future<void> _pickExpiryDate() async {
    final now = DateTime.now();
    final picked = await showDatePicker(
      context: context,
      initialDate: now.add(const Duration(days: 30)),
      firstDate: now.add(const Duration(days: 1)), // must be in the future
      lastDate: now.add(const Duration(days: 36500)),
    );
    if (picked != null) {
      setState(() => _expiryDate = picked);
    }
  }

  /// Builds the token name [TextFormField] with non-empty validation.
  Widget _buildNameField() {
    return TextFormField(
      key: const Key('api_tokens_create_name'),
      controller: _nameController,
      decoration: const InputDecoration(
        labelText: 'Token name',
        border: OutlineInputBorder(),
        hintText: 'e.g. android-client',
      ),
      textInputAction: TextInputAction.done,
      autocorrect: false,
      onFieldSubmitted: (_) => _submit(),
      validator: (value) {
        if (value == null || value.trim().isEmpty) {
          return 'Token name is required.';
        }
        return null;
      },
    );
  }

  /// Builds the expiry date picker row.
  ///
  /// Tapping the row opens a date picker; tapping the clear icon resets the
  /// date to null (no expiry).
  Widget _buildExpiryRow() {
    final expiryText = _expiryDate != null
        ? '${_expiryDate!.year.toString().padLeft(4, '0')}'
            '-${_expiryDate!.month.toString().padLeft(2, '0')}'
            '-${_expiryDate!.day.toString().padLeft(2, '0')}'
        : 'No expiry (optional)';

    return ListTile(
      key: const Key('api_tokens_expiry_tile'),
      contentPadding: EdgeInsets.zero,
      leading: const Icon(Icons.calendar_today_outlined),
      title: const Text('Expiry date'),
      subtitle: Text(expiryText),
      trailing: _expiryDate != null
          ? IconButton(
              key: const Key('api_tokens_expiry_clear'),
              icon: const Icon(Icons.clear),
              tooltip: 'Remove expiry',
              onPressed: () => setState(() => _expiryDate = null),
            )
          : null,
      onTap: _pickExpiryDate,
    );
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      key: const Key('api_tokens_create_dialog'),
      title: const Text('Create API token'),
      content: Form(
        key: _formKey,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            _buildNameField(),
            const SizedBox(height: 8),
            _buildExpiryRow(),
          ],
        ),
      ),
      actions: [
        TextButton(
          key: const Key('api_tokens_create_cancel'),
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('api_tokens_create_submit'),
          onPressed: _submit,
          child: const Text('Create'),
        ),
      ],
    );
  }
}
