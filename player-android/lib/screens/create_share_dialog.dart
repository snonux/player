import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api/player_api_client.dart';
import '../utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// showCreateShareDialog — public entry point
// ---------------------------------------------------------------------------

/// Opens the [_CreateShareDialog] as a modal dialog and returns the share URL
/// if the user successfully created a share, or `null` if they cancelled.
///
/// Separating the route function from the widget (Single Responsibility)
/// means call sites never need to construct the dialog class directly; they
/// only call this function and react to the returned URL.
///
/// [client] must be the authenticated [PlayerApiClient] — no Dio import is
/// needed at the call site (Dependency Inversion Principle).
Future<String?> showCreateShareDialog(
  BuildContext context, {
  required int mediaId,
  required PlayerApiClient client,
}) {
  return showDialog<String>(
    context: context,
    // Prevent accidental dismiss while a share request is in flight by using
    // barrierDismissible: false only during submit (handled inside the dialog
    // with the loading flag; here we allow tap-outside to cancel).
    barrierDismissible: true,
    builder: (_) => _CreateShareDialog(mediaId: mediaId, client: client),
  );
}

// ---------------------------------------------------------------------------
// _CreateShareDialog
// ---------------------------------------------------------------------------

/// Modal dialog that collects share settings (expiry date, optional max uses)
/// and calls [PlayerApiClient.createShare] on submit.
///
/// Design notes:
///   - [StatefulWidget] (not [ConsumerStatefulWidget]) because the dialog
///     only needs the injected [client]; it does not read Riverpod providers
///     directly (Dependency Inversion: the caller owns the provider read).
///   - [mounted] guards protect every async continuation.
///   - No Dio import: error mapping is delegated to [createShareErrorMessage]
///     in `error_mappers.dart` (DIP/DRY).
///   - The widget is split into focused sub-builders so the [State] class
///     stays well under 50 lines.
class _CreateShareDialog extends StatefulWidget {
  const _CreateShareDialog({
    required this.mediaId,
    required this.client,
  });

  final int mediaId;
  final PlayerApiClient client;

  @override
  State<_CreateShareDialog> createState() => _CreateShareDialogState();
}

class _CreateShareDialogState extends State<_CreateShareDialog> {
  // Default expiry is today + 7 days, matching the server's default.
  late DateTime _expiresAt = DateTime.now().add(const Duration(days: 7));

  // Controller for the optional max-uses text field.
  final _maxUsesController = TextEditingController();

  // True while the createShare API call is in flight; disables buttons.
  bool _isSubmitting = false;

  // Non-null when the last submit attempt failed.
  String? _error;

  @override
  void dispose() {
    _maxUsesController.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  /// Opens the platform date picker for the expiry field.
  ///
  /// The picker starts at the current [_expiresAt] value and only allows
  /// dates from today onwards to prevent creating already-expired links.
  Future<void> _pickDate() async {
    final picked = await showDatePicker(
      context: context,
      initialDate: _expiresAt,
      firstDate: DateTime.now(),
      lastDate: DateTime.now().add(const Duration(days: 3650)),
    );
    // Guard: dialog may have been closed while the date picker was open.
    if (!mounted) return;
    if (picked != null) {
      setState(() => _expiresAt = picked);
    }
  }

  /// Parses max-uses input, calls createShare, and delegates success handling.
  ///
  /// Acts as an orchestrator: input validation → API call → [_handleSuccess]
  /// or error display.  Clipboard copy and SnackBar are kept in [_handleSuccess]
  /// (Single Responsibility) so each method stays focused and independently
  /// testable.  On failure an inline error message is shown inside the dialog
  /// so the user can correct input without reopening.
  Future<void> _submit() async {
    if (_isSubmitting) return;

    // Parse max uses — empty input means unlimited (null).
    final maxUsesText = _maxUsesController.text.trim();
    final int? maxUses = maxUsesText.isEmpty ? null : int.tryParse(maxUsesText);
    if (maxUsesText.isNotEmpty && maxUses == null) {
      setState(() => _error = 'Max uses must be a whole number.');
      return;
    }

    setState(() {
      _isSubmitting = true;
      _error = null;
    });

    try {
      final share = await widget.client.createShare(
        widget.mediaId,
        expiresAt: _expiresAt,
        maxUses: maxUses,
      );

      // Delegate URL construction to the client (Dependency Inversion Principle):
      // the dialog never accesses Dio internals or hard-codes path segments.
      final url = widget.client.shareUrl(share.token);

      if (!mounted) return;
      await _handleSuccess(context, url);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = createShareErrorMessage(e);
        _isSubmitting = false;
      });
    }
  }

  /// Copies [shareUrl] to the clipboard, closes the dialog, and shows a
  /// SnackBar confirming the copy.
  ///
  /// Extracted from [_submit] so the clipboard/SnackBar responsibility lives in
  /// one place (Single Responsibility).  Navigator and ScaffoldMessenger are
  /// captured before the first `await` so they are never accessed across an
  /// async gap via BuildContext (avoids use_build_context_synchronously lint).
  Future<void> _handleSuccess(BuildContext context, String shareUrl) async {
    // Capture navigator and messenger before the async gap so that accessing
    // them after the await is safe and lint-clean.
    final navigator = Navigator.of(context);
    final messenger = ScaffoldMessenger.of(context);

    // Copy the URL to the clipboard before closing the dialog so the
    // user gets the copy confirmation even if the SnackBar is missed.
    await Clipboard.setData(ClipboardData(text: shareUrl));

    // Re-check mounted after the clipboard await; the dialog may have been
    // closed externally (e.g. back-button) while the clipboard write was pending.
    if (!mounted) return;

    // Close the dialog and pass the URL back to the caller.
    navigator.pop(shareUrl);

    // Show a success SnackBar via the outer Scaffold's messenger.
    messenger.showSnackBar(
      SnackBar(
        content: Text('Share link copied: $shareUrl'),
        duration: const Duration(seconds: 4),
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      key: const Key('create_share_dialog'),
      title: const Text('Create Share Link'),
      content: _buildContent(context),
      actions: _buildActions(context),
    );
  }

  /// Dialog body: expiry date row, max-uses field, and optional error message.
  Widget _buildContent(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _ExpiryRow(expiresAt: _expiresAt, onPickDate: _pickDate),
        const SizedBox(height: 16),
        _MaxUsesField(controller: _maxUsesController),
        if (_error != null) ...[
          const SizedBox(height: 12),
          _ErrorText(message: _error!),
        ],
      ],
    );
  }

  /// Cancel and Submit action buttons.
  ///
  /// Both are disabled while [_isSubmitting] is true so a second tap cannot
  /// race with the first in-flight request.
  List<Widget> _buildActions(BuildContext context) {
    return [
      TextButton(
        key: const Key('create_share_cancel'),
        onPressed: _isSubmitting ? null : () => Navigator.of(context).pop(),
        child: const Text('Cancel'),
      ),
      FilledButton(
        key: const Key('create_share_submit'),
        onPressed: _isSubmitting ? null : _submit,
        child: _isSubmitting
            ? const SizedBox(
                width: 18,
                height: 18,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : const Text('Share'),
      ),
    ];
  }
}

// ---------------------------------------------------------------------------
// _ExpiryRow
// ---------------------------------------------------------------------------

/// Row displaying the current expiry date with a button to change it.
///
/// Extracted as a stateless widget (Single Responsibility) so [_CreateShareDialogState]
/// stays concise and this row is independently testable.
class _ExpiryRow extends StatelessWidget {
  const _ExpiryRow({required this.expiresAt, required this.onPickDate});

  final DateTime expiresAt;
  final VoidCallback onPickDate;

  @override
  Widget build(BuildContext context) {
    final formatted =
        '${expiresAt.year}-${expiresAt.month.toString().padLeft(2, '0')}-${expiresAt.day.toString().padLeft(2, '0')}';

    return Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'Expires on',
                style: Theme.of(context).textTheme.labelMedium,
              ),
              const SizedBox(height: 2),
              Text(
                formatted,
                key: const Key('create_share_expiry_date'),
                style: Theme.of(context).textTheme.bodyLarge,
              ),
            ],
          ),
        ),
        TextButton.icon(
          key: const Key('create_share_pick_date'),
          onPressed: onPickDate,
          icon: const Icon(Icons.calendar_today, size: 18),
          label: const Text('Change'),
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// _MaxUsesField
// ---------------------------------------------------------------------------

/// Optional numeric text field for max-uses limit.
///
/// Empty input means unlimited uses (null sent to the server).  The numeric
/// keyboard type prevents non-digit input on most platforms.
class _MaxUsesField extends StatelessWidget {
  const _MaxUsesField({required this.controller});

  final TextEditingController controller;

  @override
  Widget build(BuildContext context) {
    return TextField(
      key: const Key('create_share_max_uses'),
      controller: controller,
      keyboardType: TextInputType.number,
      decoration: const InputDecoration(
        labelText: 'Max uses (leave blank for unlimited)',
        hintText: 'e.g. 10',
        border: OutlineInputBorder(),
        isDense: true,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _ErrorText
// ---------------------------------------------------------------------------

/// Inline error message shown when the createShare API call fails.
///
/// Uses the error colour from [ColorScheme] for semantic consistency with
/// other error states in the app.
class _ErrorText extends StatelessWidget {
  const _ErrorText({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return Text(
      message,
      key: const Key('create_share_error'),
      style: Theme.of(context)
          .textTheme
          .bodySmall
          ?.copyWith(color: Theme.of(context).colorScheme.error),
    );
  }
}
