import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../api/dio_player_api_client.dart';
import '../services/podcast_repository.dart';
import '../services/saved_queue_sync.dart';
import 'api_client_provider.dart';
import 'auth_state_provider.dart';

final savedQueueSyncProvider =
    ChangeNotifierProvider<SavedQueueSync>((ref) => SavedQueueSync());

SavedQueueAccount? captureSavedQueueAccount(Ref ref) {
  final auth = ref.read(authStateProvider).valueOrNull;
  final mutations = ref.read(credentialMutationQueueProvider);
  if (auth?.isAuthenticated != true || !mutations.credentialsEnabled) {
    return null;
  }
  final generation = mutations.generation;
  final origin = ref.read(playerBaseUrlProvider).origin;
  final userId = auth!.user!.id;
  final client = ref.read(apiClientProvider);
  if (client is! DioPlayerApiClient) return null;
  bool current() =>
      mutations.credentialsEnabled &&
      mutations.generation == generation &&
      mutations.credentialOrigin == origin &&
      ref.read(authStateProvider).valueOrNull?.user?.id == userId;
  return SavedQueueAccount(
      scope: podcastAccountScope(origin, userId),
      current: current,
      read: () async {
        if (!current()) throw StateError('Account changed');
        final response = await client.rawDio.get<Map<String, dynamic>>(
            '/api/v1/queue',
            options: Options(extra: {'credentialEpoch': generation}));
        return response.data!;
      },
      write: (body, revision) async {
        if (!current()) throw StateError('Account changed');
        try {
          final response = await client.rawDio.put<Map<String, dynamic>>(
              '/api/v1/queue',
              data: body,
              options: Options(
                  extra: {'credentialEpoch': generation},
                  headers: {'If-Match': '"$revision"'}));
          return (conflict: false, queue: response.data!);
        } on DioException catch (error) {
          if (error.response?.statusCode != 409) rethrow;
          return (
            conflict: true,
            queue: Map<String, dynamic>.from(error.response!.data as Map)
          );
        }
      });
}

final savedQueueAccountProvider = Provider<SavedQueueAccount?>((ref) {
  ref.watch(authStateProvider);
  ref.watch(playerBaseUrlProvider);
  return captureSavedQueueAccount(ref);
});
