import 'dart:async';
import 'dart:io';
import 'package:dio/dio.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../api/dio_client.dart';
import '../api/dio_player_api_client.dart';
import '../services/automatic_music.dart';
import '../services/podcast_sync.dart';
import '../services/download_quality.dart';
import '../services/network_policy.dart';
import '../services/offline_downloads.dart';
import 'api_client_provider.dart';
import 'auth_state_provider.dart';
import 'offline_downloads_provider.dart';
import 'playback_preparer_provider.dart';
import 'server_endpoint_provider.dart';

AutomaticMusicAccount? captureAutomaticMusicAccount(Ref ref) {
  final user = ref.read(authStateProvider).valueOrNull?.user;
  final mutations = ref.read(credentialMutationQueueProvider);
  final origin = ref.read(playerBaseUrlProvider).origin;
  if (user == null ||
      !mutations.credentialsEnabled ||
      mutations.credentialOrigin != origin) {
    return null;
  }
  final generation = mutations.generation;
  final api = ref.read(apiClientProvider);
  final quiet = DioPlayerApiClient(
      dio: api.rawDio,
      credentialEpoch: () => generation,
      quietUnauthorized: true);
  final downloads = ref.read(offlineDownloadsProvider);
  final preparer = ref.read(playbackPreparerProvider);
  final storage = ref.read(tokenStorageProvider),
      cookies = ref.read(cookieJarProvider);
  final router = ref.read(serverEndpointRouterProvider);
  final token = CancelToken();
  bool current() =>
      mutations.credentialsEnabled &&
      mutations.generation == generation &&
      mutations.credentialOrigin == origin;
  final quality = DownloadQuality.selected(api.rawDio.options.baseUrl);
  return AutomaticMusicAccount(
      origin: origin,
      userId: user.id,
      current: current,
      cancel: () => token.cancel('Automatic music canceled'),
      read: (path) async {
        if (!current()) throw StateError('Account changed.');
        final response = await api.rawDio.get<dynamic>(path,
            cancelToken: token,
            options: Options(extra: {
              'credentialEpoch': generation,
              'quietUnauthorized': true
            }));
        if (!current()) throw StateError('Account changed.');
        return response.data;
      },
      save: (media, authorized, onAbort) async {
        final profile = await quality;
        if (!authorized()) throw OfflineDownloadCancelled();
        final explicit = profile != 'original';
        final uri = Uri.parse(explicit
            ? '${api.compatUrl(media.id)}?profile=${Uri.encodeQueryComponent(profile)}'
            : api.downloadUrl(media.id));
        final headers = await accountRequestHeaders(
            uri: uri,
            baseUrl: Uri.parse(origin),
            storage: storage,
            cookieJar: cookies,
            mutations: mutations);
        if (!authorized()) throw OfflineDownloadCancelled();
        final transport = await router.resolve(uri);
        if (!authorized()) throw OfflineDownloadCancelled();
        try {
          await downloads.download(
              media: media,
              automationMusicEligible: true,
              automaticMusic: true,
              downloadProfile: profile,
              expectedContentType: DownloadQuality.contentType(profile),
              sourceName: DownloadQuality.sourceName(media.relPath, profile),
              originalBytes: !explicit,
              origin: origin,
              userId: user.id,
              uri: transport,
              trustedTransportOrigin: transport.origin,
              headers: headers,
              authorized: () => current() && authorized(),
              onStarted: (job) => onAbort(job.cancel),
              prepare: !explicit
                  ? null
                  : (job) async {
                      final preparation = preparer.prepare(uri,
                          probe: quiet.probePlayback,
                          stillWanted: () => authorized() && !job.cancelled);
                      job.abort = preparation.cancel;
                      if (!await preparation.ready) {
                        throw OfflineDownloadCancelled();
                      }
                    });
        } on SocketException {
          router.failed(transport);
          rethrow;
        } on TimeoutException {
          router.failed(transport);
          rethrow;
        }
      });
}

final automaticMusicProvider = ChangeNotifierProvider<AutomaticMusic>((ref) {
  final service = AutomaticMusic(
      AutomaticMusicLibrary(ref.watch(offlineDownloadRepositoryProvider)),
      load: (key) async => (await SharedPreferences.getInstance())
          .getString('automatic-music:$key'),
      persist: (key, value) async {
        if (!await (await SharedPreferences.getInstance())
            .setString('automatic-music:$key', value)) {
          throw StateError('Could not save automatic music settings.');
        }
      },
      network: () => MusicNetworkPolicy.network);
  void bind() {
    unawaited(service.bind(captureAutomaticMusicAccount(ref)?.key));
  }

  ref.listen(authStateProvider, (_, __) => bind());
  ref.listen(playerBaseUrlProvider, (_, __) => bind());
  MusicNetworkPolicy.changes.addListener(service.cancel);
  ref.onDispose(
      () => MusicNetworkPolicy.changes.removeListener(service.cancel));
  unawaited(MusicNetworkPolicy.initialize().then((_) => bind()));
  return service;
});

/// Only a live, unlocked engine can honor an OS wake. No headless credentials.
class AutomaticMusicScheduler {
  AutomaticMusicScheduler(this.service) {
    channel.setMethodCallHandler((call) async {
      if (call.method == 'stop') {
        if (invocation == call.arguments) wakeStopped = true;
        service.cancel(invocation: call.arguments as String);
        return null;
      }
      if (call.method == 'wake') {
        invocation = call.arguments as String;
        wakeStopped = false;
        try {
          await onWake?.call();
        } finally {
          invocation = null;
          wakeStopped = false;
        }
        return null;
      }
      throw MissingPluginException();
    });
    service.addListener(_schedule);
    _schedule();
  }
  static const channel =
      MethodChannel('zone.foo.player_android/automatic_music');
  final AutomaticMusic service;
  Future<void> Function()? onWake;
  String? invocation;
  bool wakeStopped = false;
  String? _last;
  String? error;
  bool _disposed = false;
  void _schedule() {
    final p = service.policy;
    final value =
        '${service.key}:${p.enabled}:${p.selected}:${p.hours}:${p.wifiOnly}';
    if (value == _last || service.loading) return;
    _last = value;
    unawaited(() async {
      try {
        await channel.invokeMethod<void>('schedule', {
          'enabled': service.key != null && p.enabled && p.selected,
          'hours': p.hours,
          'wifiOnly': p.wifiOnly
        });
        error = null;
      } catch (_) {
        error =
            'Android scheduling is unavailable. Reopen Player to retry; foreground checks still run.';
      }
      if (!_disposed) service.schedulerChanged();
    }());
  }

  void retry() {
    _last = null;
    _schedule();
  }

  Future<void> sync(AutomaticMusicAccount account, {bool explicit = false}) =>
      wakeStopped
          ? Future.error(const SyncDeferred('canceled'))
          : service.sync(account, explicit: explicit, invocation: invocation);
  void dispose() {
    _disposed = true;
    service.removeListener(_schedule);
    channel.setMethodCallHandler(null);
  }
}

final automaticMusicSchedulerProvider =
    Provider<AutomaticMusicScheduler>((ref) {
  // The scheduler owns its binding to this service instance. Watching the
  // ChangeNotifier value itself recreates it on every status notification,
  // which also recreates the global sync coordinator while a run is active.
  final scheduler =
      AutomaticMusicScheduler(ref.watch(automaticMusicProvider.notifier));
  ref.onDispose(scheduler.dispose);
  return scheduler;
});
