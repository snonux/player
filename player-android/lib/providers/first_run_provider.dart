import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'api_client_provider.dart';

/// Returns whether the server has no registered users yet (first-run state).
///
/// The go_router redirect callback reads this provider to decide whether
/// an unauthenticated visit should go to /login (normal case) or /bootstrap
/// (no accounts exist).
///
/// Implementation notes:
///   - Uses [FutureProvider] so the loading/error states are handled uniformly
///     alongside [authStateProvider] in the router's redirect callback.
///   - [FutureProvider] caches its result for the lifetime of the enclosing
///     [ProviderScope]; the value is re-fetched only when the provider is
///     explicitly invalidated (e.g. via `ref.invalidate`) or the scope is
///     recreated.  After bootstrap completes the app navigates away and the
///     cached value is simply never re-read in the same session.
///   - Errors (e.g. server unreachable) are treated as non-first-run so the
///     login screen is shown and the user can retry; this avoids looping to
///     /bootstrap on connectivity failures.
final firstRunProvider = FutureProvider<bool>((ref) async {
  final apiClient = ref.read(apiClientProvider);
  try {
    final count = await apiClient.countUsers();
    return count == 0;
  } catch (_) {
    // Network or server error: assume not first-run so we show login.
    // The login screen will surface the connectivity problem when the user
    // attempts to authenticate.
    return false;
  }
});
