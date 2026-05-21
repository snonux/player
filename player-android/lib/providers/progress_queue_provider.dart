import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/progress_queue.dart';
import 'api_client_provider.dart';

/// Provides the singleton [ProgressQueueBase] for the whole application.
///
/// The provider return type is [ProgressQueueBase] (not the concrete
/// [ProgressQueue]) so that callers depend only on the abstract interface
/// (Dependency Inversion).  Tests override this provider with a lightweight
/// [ProgressQueueBase] implementation that never opens a real database.
///
/// [ProgressQueueBase.init] must be called before the queue is useful; this is
/// done once in `main()` after [ProviderScope] is set up so the DB is open
/// and the connectivity subscription is active before any player screen opens.
///
/// Dependency Inversion: [PlayerApiClient] is injected from [apiClientProvider]
/// via its [ProgressSyncClient] interface so [ProgressQueue] has no knowledge
/// of Dio or concrete HTTP classes.  The [databaseFactory] is left null so
/// [ProgressQueue] opens the default on-disk database; tests override the
/// entire provider to avoid touching the filesystem.
final progressQueueProvider = Provider<ProgressQueueBase>((ref) {
  final apiClient = ref.watch(apiClientProvider);
  // databaseFactory is null → ProgressQueue.init() opens the on-disk DB.
  // Connectivity() is created lazily inside ProgressQueue; no extra wiring needed.
  return ProgressQueue(apiClient: apiClient);
});
