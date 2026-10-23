import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';

import '../models/media.dart';
import '../models/offline_media.dart';
import 'offline_download_repository.dart';
import 'playback_session_coordinator.dart';

class OfflineDownloadCancelled implements Exception {}

class OfflineDownloadJob {
  OfflineDownloadJob(this.key, this.title);
  final String key;
  final String title;
  int received = 0;
  int? total;
  bool cancelled = false;
  void Function()? abort;

  void cancel() {
    cancelled = true;
    abort?.call();
  }
}

/// One foreground download at a time. Bytes are written with backpressure,
/// never accumulated into a whole-file buffer. No remote mutation API exists.
class OfflineDownloads extends ChangeNotifier {
  OfflineDownloads(this.repository, this.playback);

  final OfflineDownloadRepository repository;
  final PlaybackSessionCoordinator playback;
  OfflineDownloadJob? active;
  String? error;
  int _libraryRevision = 0;
  int get libraryRevision => _libraryRevision;
  bool _disposed = false;

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  void cancel() => active?.cancel();

  Future<OfflineMedia> download({
    required Media media,
    required String origin,
    String? trustedTransportOrigin,
    required int userId,
    required Uri uri,
    required Map<String, String> headers,
    required bool Function() authorized,
    Future<void> Function(OfflineDownloadJob job)? prepare,
  }) async {
    if (active != null) throw StateError('Wait for the current download.');
    if ((uri.origin != origin && uri.origin != trustedTransportOrigin) ||
        !{'http', 'https'}.contains(uri.scheme) ||
        uri.userInfo.isNotEmpty ||
        userId <= 0 ||
        media.id <= 0 ||
        !{'audio', 'video', 'image'}.contains(media.type) ||
        headers.isEmpty ||
        !authorized()) {
      throw StateError('Sign in to this server before downloading.');
    }
    final key = offlineMediaKey(origin, userId, media.id);
    final job = OfflineDownloadJob(key, media.displayTitle);
    active = job;
    error = null;
    _changed();
    HttpClient? client;
    RandomAccessFile? output;
    var createdFinal = false;
    var storageReady = false;
    void check() {
      if (job.cancelled || !authorized() || _disposed) {
        throw OfflineDownloadCancelled();
      }
    }

    try {
      await repository.database;
      storageReady = true;
      check();
      final existing = await repository.get(key);
      if (existing != null) {
        await repository.playbackRequest(key);
        return existing;
      }
      if (prepare != null) await prepare(job);
      check();
      client = HttpClient()
        ..autoUncompress = false
        ..connectionTimeout = const Duration(seconds: 30);
      job.abort = () => client?.close(force: true);
      final request = await client.getUrl(uri);
      request.followRedirects = false;
      headers.forEach(request.headers.set);
      final response =
          await request.close().timeout(const Duration(seconds: 30));
      check();
      if (response.statusCode != HttpStatus.ok) {
        throw HttpException(
            'Server refused download (HTTP ${response.statusCode}).');
      }
      final mime = response.headers.contentType?.mimeType;
      if (mime != null &&
          mime != 'application/octet-stream' &&
          !mime.startsWith('${media.type}/')) {
        throw const HttpException('Server did not return a media file.');
      }
      final declared =
          response.contentLength >= 0 ? response.contentLength : null;
      final expected = !media.transcoded && media.fileSizeBytes > 0
          ? media.fileSizeBytes
          : null;
      if (declared != null && expected != null && declared != expected) {
        throw const HttpException(
            'File size changed. Refresh the media and try again.');
      }
      job.total = declared ?? expected;
      output =
          await repository.file(key, partial: true).open(mode: FileMode.write);
      var lastNotice = DateTime.now();
      await for (final chunk in response.timeout(const Duration(seconds: 30))) {
        check();
        job.received += chunk.length;
        if (job.total != null && job.received > job.total!) {
          throw const HttpException(
              'Downloaded file exceeds its declared size.');
        }
        await output.writeFrom(chunk);
        if (DateTime.now().difference(lastNotice).inMilliseconds >= 150) {
          lastNotice = DateTime.now();
          _changed();
        }
      }
      check();
      if (job.received == 0 ||
          (job.total != null && job.received != job.total)) {
        throw const HttpException('Download is incomplete. Please try again.');
      }
      await output.flush();
      await output.close();
      output = null;
      check();
      await repository
          .file(key, partial: true)
          .rename(repository.file(key).path);
      createdFinal = true;
      check();
      final item = OfflineMedia(
        key: key,
        origin: origin,
        userId: userId,
        mediaId: media.id,
        title: media.displayTitle,
        music: media.music,
        sourceName: media.relPath,
        type: media.type,
        sizeBytes: job.received,
        addedAt: DateTime.now().toUtc(),
      );
      await repository.add(item);
      check();
      createdFinal = false;
      _libraryRevision++;
      return item;
    } catch (failure) {
      if (job.cancelled ||
          !authorized() ||
          failure is OfflineDownloadCancelled) {
        error = null;
        throw OfflineDownloadCancelled();
      }
      error = failure is FileSystemException
          ? 'Could not save the download. Check available device storage and try again.'
          : failure is HttpException
              ? failure.message
              : 'Download failed. Check your connection and try again.';
      rethrow;
    } finally {
      client?.close(force: true);
      try {
        try {
          await output?.close();
        } finally {
          // Cleanup also handles disk-full writes and cancellation during commit.
          if (createdFinal) await repository.remove(key);
          if (storageReady) {
            final partial = repository.file(key, partial: true);
            if (await partial.exists()) await partial.delete();
          }
        }
      } catch (_) {
        // Retain the original download failure. Startup retries owned partial
        // cleanup, and a cleanup failure must never leave the manager busy.
        error ??=
            'Temporary download data could not be removed. Restart Player to retry cleanup.';
      } finally {
        active = null;
        _changed();
      }
    }
  }

  Future<void> remove(String key) async {
    if (active?.key == key) throw StateError('Cancel this download first.');
    await playback.stopLocalSource(repository.file(key).uri.toString());
    await repository.remove(key);
    _libraryRevision++;
    _changed();
  }

  @override
  void dispose() {
    _disposed = true;
    cancel();
    super.dispose();
  }
}
