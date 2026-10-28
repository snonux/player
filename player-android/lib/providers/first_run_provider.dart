import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'api_client_provider.dart';
import '../services/saved_server_credentials.dart';

/// Returns whether the server has no registered users yet (first-run state).
///
/// The go_router redirect callback reads this provider to decide whether
/// an unauthenticated visit should go to /login (normal case) or /bootstrap
/// (no accounts exist).
///
/// Implementation notes:
///   - Uses [FutureProvider] so the loading/error states are handled uniformly
///     alongside [authStateProvider] in the router's redirect callback.
///   - [FutureProvider] caches its result until the API client changes (for
///     example when the saved server URL changes) or it is invalidated.
///   - Errors (e.g. server unreachable) are treated as non-first-run so the
///     login screen is shown and the user can retry; this avoids looping to
///     /bootstrap on connectivity failures.
final firstRunProvider = FutureProvider<bool>((ref) async {
  final saved = ref.watch(selectedServerCredentialProvider.future);
  final apiClient = ref.watch(apiClientProvider);
  try {
    // A validated saved identity already establishes this is a returning user.
    // Optional setup detection must not delay offline fingerprint access.
    if ((await saved)?.isValid == true) return false;
  } catch (_) {/* Password login remains available if secure storage fails. */}
  try {
    final count =
        await apiClient.countUsers().timeout(const Duration(seconds: 4));
    return count == 0;
  } catch (_) {
    // Network or server error: assume not first-run so we show login.
    // The login screen will surface the connectivity problem when the user
    // attempts to authenticate.
    return false;
  }
});
