import 'package:flutter/material.dart';

import '../api/player_api_client.dart';
import '../utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// showSubscribeDialog — public entry point
// ---------------------------------------------------------------------------

/// Opens the [_SubscribeDialog] as a modal dialog.
///
/// Returns the subscribed feed's set name on success, or `null` if the user
/// cancelled.  Separating the entry-point function from the widget (Single
/// Responsibility) means call sites never construct the dialog class directly —
/// they call this function and react to the returned value.
///
/// [client] must be an authenticated [PlayerApiClient]; no Dio import is
/// needed at the call site (Dependency Inversion Principle).
Future<String?> showSubscribeDialog(
  BuildContext context, {
  required PlayerApiClient client,
}) {
  return showDialog<String>(
    context: context,
    barrierDismissible: true,
    builder: (_) => _SubscribeDialog(client: client),
  );
}

// ---------------------------------------------------------------------------
// _SubscribeDialog
// ---------------------------------------------------------------------------

/// Modal dialog that collects a feed URL and optional set name, then calls
/// [PlayerApiClient.subscribePodcast] on submit.
///
/// Design notes:
///   - [StatefulWidget] (not [ConsumerStatefulWidget]) because the dialog
///     only needs the injected [client]; it does not read Riverpod providers
///     directly (Dependency Inversion: the caller owns the provider read).
///   - [mounted] guards protect every async continuation.
///   - No Dio import: error mapping is delegated to [podcastErrorMessage]
///     in `error_mappers.dart` (DIP/DRY).
///   - Clipboard/SnackBar logic lives in [_handleSuccess] (Single Responsibility)
///     so the submit orchestrator stays focused on flow control only.
///   - The widget is split into focused sub-builders so the [State] class
///     stays well under 50 lines.
class _SubscribeDialog extends StatefulWidget {
  const _SubscribeDialog({required this.client});

  final PlayerApiClient client;

  @override
  State<_SubscribeDialog> createState() => _SubscribeDialogState();
}

class _SubscribeDialogState extends State<_SubscribeDialog> {
  // Controller for the required feed URL text field.
  final _feedUrlController = TextEditingController();

  // Controller for the optional set-name text field.
  final _setNameController = TextEditingController();

  // True while the subscribePodcast API call is in flight; disables buttons.
  bool _isSubmitting = false;

  // Non-null when the last submit attempt failed.
  String? _error;

  @override
  void dispose() {
    _feedUrlController.dispose();
    _setNameController.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  /// Validates inputs, calls [subscribePodcast], and delegates to
  /// [_handleSuccess] or displays an inline error.
  ///
  /// Acts as an orchestrator: validation → API call → [_handleSuccess] or
  /// error display.  SnackBar/Navigator logic stays in [_handleSuccess]
  /// (Single Responsibility) so each method has one reason to change.
  Future<void> _submit() async {
    if (_isSubmitting) return;

    final feedUrl = _feedUrlController.text.trim();
    if (feedUrl.isEmpty) {
      setState(() => _error = 'Feed URL is required.');
      return;
    }

    setState(() {
      _isSubmitting = true;
      _error = null;
    });

    try {
      final setName = _setNameController.text.trim();
      await widget.client.subscribePodcast(
        feedUrl: feedUrl,
        setName: setName.isEmpty ? null : setName,
      );

      if (!mounted) return;
      await _handleSuccess(context);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = podcastErrorMessage(e);
        _isSubmitting = false;
      });
    }
  }

  /// Closes the dialog and shows a success SnackBar.
  ///
  /// Extracted from [_submit] so the SnackBar/Navigator responsibility lives
  /// in one place (Single Responsibility).  Navigator and ScaffoldMessenger
  /// are captured before the first `await` so they are never accessed across
  /// an async gap via BuildContext (avoids use_build_context_synchronously).
  Future<void> _handleSuccess(BuildContext context) async {
    // Capture navigator and messenger before any async gap.
    final navigator = Navigator.of(context);
    final messenger = ScaffoldMessenger.of(context);
    final feedTitle = _feedUrlController.text.trim();

    // Close the dialog and pass back the feed URL as a success signal.
    navigator.pop(feedTitle);

    // Show a success SnackBar through the outer Scaffold's messenger.
    messenger.showSnackBar(
      const SnackBar(
        content: Text('Podcast subscribed. The feed will be fetched shortly.'),
        duration: Duration(seconds: 4),
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      key: const Key('subscribe_dialog'),
      title: const Text('Subscribe to Podcast'),
      content: _buildContent(context),
      actions: _buildActions(context),
    );
  }

  /// Dialog body: feed URL field, set-name field, and optional error message.
  Widget _buildContent(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _FeedUrlField(controller: _feedUrlController),
        const SizedBox(height: 16),
        _SetNameField(controller: _setNameController),
        if (_error != null) ...[
          const SizedBox(height: 12),
          _ErrorText(message: _error!),
        ],
      ],
    );
  }

  /// Cancel and Subscribe action buttons.
  ///
  /// Both are disabled while [_isSubmitting] is true to prevent double-submit.
  List<Widget> _buildActions(BuildContext context) {
    return [
      TextButton(
        key: const Key('subscribe_cancel'),
        onPressed: _isSubmitting ? null : () => Navigator.of(context).pop(),
        child: const Text('Cancel'),
      ),
      FilledButton(
        key: const Key('subscribe_submit'),
        onPressed: _isSubmitting ? null : _submit,
        child: _isSubmitting
            ? const SizedBox(
                width: 18,
                height: 18,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : const Text('Subscribe'),
      ),
    ];
  }
}

// ---------------------------------------------------------------------------
// _FeedUrlField
// ---------------------------------------------------------------------------

/// Required text field for the podcast feed URL.
///
/// Extracted as a stateless widget (Single Responsibility) so
/// [_SubscribeDialogState] stays concise and the field is independently
/// testable.
class _FeedUrlField extends StatelessWidget {
  const _FeedUrlField({required this.controller});

  final TextEditingController controller;

  @override
  Widget build(BuildContext context) {
    return TextField(
      key: const Key('subscribe_feed_url'),
      controller: controller,
      keyboardType: TextInputType.url,
      autocorrect: false,
      decoration: const InputDecoration(
        labelText: 'Feed URL',
        hintText: 'https://example.com/feed.rss',
        border: OutlineInputBorder(),
        isDense: true,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _SetNameField
// ---------------------------------------------------------------------------

/// Optional text field for the podcast set name.
///
/// When left blank the server derives the name from the feed's own title.
/// Extracted as a stateless widget (SRP) for independent testability.
class _SetNameField extends StatelessWidget {
  const _SetNameField({required this.controller});

  final TextEditingController controller;

  @override
  Widget build(BuildContext context) {
    return TextField(
      key: const Key('subscribe_set_name'),
      controller: controller,
      decoration: const InputDecoration(
        labelText: 'Set name (optional)',
        hintText: 'Leave blank to use the feed title',
        border: OutlineInputBorder(),
        isDense: true,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _ErrorText
// ---------------------------------------------------------------------------

/// Inline error message shown when the subscribePodcast API call fails.
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
      key: const Key('subscribe_error'),
      style: Theme.of(context)
          .textTheme
          .bodySmall
          ?.copyWith(color: Theme.of(context).colorScheme.error),
    );
  }
}
