import 'dart:async';
import 'dart:io';
import 'package:dio/dio.dart';
import 'podcast_sync_store.dart';

const syncBeforePlaybackTimeout = Duration(seconds: 3);

class PodcastSyncAccount {
  PodcastSyncAccount(
      {required this.scope,
      required this.current,
      required this.supported,
      required this.send,
      this.supportsDeletion});
  final Future<bool> Function()? supportsDeletion;
  final String scope;
  final bool Function() current;
  final Future<bool> Function() supported;
  final Future<Map<String, dynamic>> Function(Map<String, dynamic>) send;
}

class SyncDeferred implements Exception {
  const SyncDeferred(this.kind);
  final String kind;
}

String syncErrorKind(Object error) {
  if (error is SyncDeferred) return error.kind;
  if (error is DioException) {
    if (error.response?.statusCode == 401) return 'auth';
    if ([404, 405, 501].contains(error.response?.statusCode)) {
      return 'unsupported';
    }
    if (error.response != null) return 'server';
    return 'offline';
  }
  if (error is TimeoutException ||
      error is SocketException ||
      error is HandshakeException) {
    return 'offline';
  }
  return 'server';
}

class PodcastSyncResult {
  const PodcastSyncResult(
      {required this.full, this.errorKind, this.supported = true});
  final bool full;
  final bool supported;
  final String? errorKind;
}

class PodcastSync {
  PodcastSync(this.store);
  final PodcastSyncStore store;
  Future<PodcastSyncResult> sync(PodcastSyncAccount account) async {
    try {
      if (!account.current()) {
        return const PodcastSyncResult(full: false, errorKind: 'auth');
      }
      if (!await account.supported()) {
        return const PodcastSyncResult(full: true, supported: false);
      }
      final includeDeletion = await account.supportsDeletion?.call() ?? true;
      final starting =
          await store.pending(account.scope, includeDeletion: includeDeletion);
      var rejected = false;
      var pageSize = 500;
      var hasMore = true;
      final attempted = <String>{};
      while (account.current()) {
        final request = await store.payload(account.scope,
            limit: pageSize, includeDeletion: includeDeletion);
        final edits = PodcastSyncStore.editIds(request);
        if (!hasMore && edits.isEmpty) {
          final remaining = await store.pending(account.scope,
              includeDeletion: includeDeletion);
          return PodcastSyncResult(
              full: !rejected && starting.intersection(remaining).isEmpty,
              errorKind: rejected ? 'rejected' : null);
        }
        // An acknowledged register that was replaced during the request may
        // remain pending. Do not let continuously arriving edits starve a run.
        if (!hasMore && edits.every(attempted.contains)) {
          return PodcastSyncResult(
              full: false, errorKind: rejected ? 'rejected' : 'server');
        }
        Map<String, dynamic> response;
        try {
          if (!account.current()) break;
          response = await account.send(request);
        } on DioException catch (error) {
          if (error.response?.statusCode == 413 && pageSize > 1) {
            pageSize = (pageSize / 2).floor();
            continue;
          }
          rethrow;
        }
        if (!account.current()) break;
        rejected =
            !await store.apply(account.scope, response, account.current) ||
                rejected;
        attempted.addAll(edits);
        hasMore = response['has_more'] as bool;
      }
      return const PodcastSyncResult(full: false, errorKind: 'auth');
    } catch (error) {
      return PodcastSyncResult(full: false, errorKind: syncErrorKind(error));
    }
  }

  /// A late reply updates storage only. This service never owns or seeks a player.
  Future<void> beforePlayback(
      PodcastSyncAccount account, String feed, String episode) async {
    Future<void> attempt() async {
      try {
        if (!account.current() ||
            !await account.supported() ||
            !account.current()) {
          return;
        }
        final request = await store.payload(account.scope,
            feed: feed,
            episode: episode,
            includeDeletion: await account.supportsDeletion?.call() ?? true);
        if (!account.current()) return;
        final response = await account.send(request);
        if (account.current()) {
          await store.apply(account.scope, response, account.current,
              scoped: true);
        }
      } catch (_) {/* Playback uses its durable local state. */}
    }

    await attempt().timeout(syncBeforePlaybackTimeout, onTimeout: () {});
  }
}
