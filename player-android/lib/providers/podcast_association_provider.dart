import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/dio_player_api_client.dart';
import '../services/podcast_repository.dart';
import 'api_client_provider.dart';
import 'auth_state_provider.dart';
import 'podcast_library_provider.dart';
import 'settings_provider.dart';

/// The account named by a consent prompt, including its credential lease.
class PodcastAssociationAccount {
  const PodcastAssociationAccount(
      {required this.scope,
      required this.label,
      required this.current,
      required this.supported});
  final String scope;
  final String label;
  final bool Function() current;
  final Future<bool> Function() supported;
}

final podcastAssociationAccountProvider =
    Provider<PodcastAssociationAccount?>((ref) {
  final auth = ref.watch(authStateProvider).valueOrNull;
  final settings = ref.watch(settingsProvider).valueOrNull;
  if (settings?.destination != LibraryDestination.server ||
      auth?.isAuthenticated != true ||
      auth?.user == null) {
    return null;
  }
  final mutations = ref.watch(credentialMutationQueueProvider);
  final origin = ref.watch(playerBaseUrlProvider).origin;
  if (!mutations.credentialsEnabled || mutations.credentialOrigin != origin) {
    return null;
  }
  final api = ref.watch(apiClientProvider);
  if (api is! DioPlayerApiClient) return null;
  final user = auth!.user!;
  final generation = mutations.generation;
  var alive = true;
  ref.onDispose(() => alive = false);
  bool current() =>
      alive &&
      mutations.credentialsEnabled &&
      mutations.generation == generation &&
      mutations.credentialOrigin == origin &&
      ref.read(authStateProvider).valueOrNull?.user?.id == user.id &&
      ref.read(settingsProvider).valueOrNull?.destination ==
          LibraryDestination.server;
  final client = DioPlayerApiClient(
      dio: api.rawDio,
      credentialEpoch: () => generation,
      quietUnauthorized: true);
  return PodcastAssociationAccount(
      scope: podcastAccountScope(origin, user.id),
      label: '${user.username}@$origin',
      current: current,
      supported: () async =>
          current() &&
          (await client
                  .getConfig()
                  .timeout(const Duration(seconds: 3)))['podcast_sync'] ==
              1);
});

/// Consent decisions are scoped separately from the unchanged local library.
class PodcastAssociationController {
  PodcastAssociationController(this.repository, this.preferences);
  final PodcastRepository repository;
  final Future<SharedPreferences> Function() preferences;
  String _key(PodcastAssociationAccount account) =>
      'podcast_association_prompted:${account.scope}';

  Future<PodcastAssociationAccount?> offer(PodcastAssociationAccount account,
      {bool repeat = false}) async {
    if (!account.current()) return null;
    final prefs = await preferences();
    if (!account.current() ||
        (!repeat && prefs.getBool(_key(account)) == true)) {
      return null;
    }
    if (!await repository.canAssociateLocal(account.scope) ||
        !account.current()) {
      return null;
    }
    try {
      if (!await account.supported() || !account.current()) return null;
    } catch (_) {
      return null; // Optional consent never interrupts offline use or sign-in.
    }
    return account;
  }

  Future<bool> markPrompted(PodcastAssociationAccount account) async {
    final prefs = await preferences();
    if (!account.current()) return false;
    return prefs.setBool(_key(account), true);
  }

  Future<void> associate(PodcastAssociationAccount account) async {
    if (!account.current()) throw StateError('Account changed');
    await repository.associateLocal(account.scope, current: account.current);
  }
}

final podcastAssociationControllerProvider = Provider((ref) =>
    PodcastAssociationController(
        ref.watch(podcastRepositoryProvider), SharedPreferences.getInstance));

final podcastAssociationOfferProvider =
    FutureProvider<PodcastAssociationAccount?>((ref) async {
  final account = ref.watch(podcastAssociationAccountProvider);
  if (account == null) return null;
  // Successful global passes invalidate subscriptions, also retrying an offer
  // whose initial capability lookup was unavailable at sign-in.
  ref.watch(podcastSubscriptionsProvider);
  return ref.watch(podcastAssociationControllerProvider).offer(account);
});
