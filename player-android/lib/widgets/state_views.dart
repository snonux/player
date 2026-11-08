import 'package:flutter/material.dart';

import '../l10n/music_localizations.dart';

/// Full-screen load failure with a retry button, shared by list screens.
///
/// [keyPrefix] names the widget keys tests look up: `<prefix>_error` on the
/// message and `<prefix>_retry` on the button.
class ErrorRetryView extends StatelessWidget {
  const ErrorRetryView({
    super.key,
    required this.message,
    required this.onRetry,
    required this.keyPrefix,
    this.icon = Icons.error_outline,
  });

  final String message;
  final VoidCallback onRetry;
  final String keyPrefix;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(icon, size: 56, color: theme.colorScheme.error),
            const SizedBox(height: 16),
            Text(
              message,
              key: Key('${keyPrefix}_error'),
              textAlign: TextAlign.center,
              style: theme.textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: Key('${keyPrefix}_retry'),
              onPressed: onRetry,
              icon: const Icon(Icons.refresh),
              label: Text(context.tr('Retry')),
            ),
          ],
        ),
      ),
    );
  }
}

/// Centered empty-list state: icon, title, optional hint and action.
///
/// With [scrollable] (the default) it fills its parent inside an always
/// scrollable view so a surrounding [RefreshIndicator] still works.
class EmptyStateView extends StatelessWidget {
  const EmptyStateView({
    super.key,
    required this.icon,
    required this.title,
    required this.titleKey,
    this.message,
    this.action,
    this.scrollable = true,
  });

  final IconData icon;
  final String title;
  final Key titleKey;
  final String? message;
  final Widget? action;
  final bool scrollable;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final content = Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 72, color: theme.colorScheme.onSurfaceVariant),
            const SizedBox(height: 16),
            Text(
              title,
              key: titleKey,
              textAlign: TextAlign.center,
              style: theme.textTheme.titleMedium,
            ),
            if (message != null) ...[
              const SizedBox(height: 8),
              Text(
                message!,
                textAlign: TextAlign.center,
                style: theme.textTheme.bodyMedium,
              ),
            ],
            if (action != null) ...[const SizedBox(height: 8), action!],
          ],
        ),
      ),
    );
    if (!scrollable) return content;
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        child: SizedBox(
          height: constraints.hasBoundedHeight
              ? constraints.maxHeight
              : MediaQuery.sizeOf(context).height * 0.6,
          child: content,
        ),
      ),
    );
  }
}

/// Small inline error line under a dialog form.
class InlineErrorText extends StatelessWidget {
  const InlineErrorText(this.message, {super.key});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Text(
      message,
      style: theme.textTheme.bodySmall?.copyWith(
        color: theme.colorScheme.error,
      ),
    );
  }
}
