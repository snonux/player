import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/local_media.dart';
import '../services/local_library_repository.dart';
import '../services/local_document_picker.dart';
import '../services/local_library_manager.dart';
import '../services/playback_session_coordinator.dart';
import 'playback_session_provider.dart';

/// Opens the standalone local database only when a local repository is used.
/// It is independent of server auth, settings, and the network progress queue.
final localLibraryDatabaseProvider = Provider<LocalLibraryDatabase>((ref) {
  final database = LocalLibraryDatabase();
  ref.onDispose(() => unawaited(database.close()));
  return database;
});

final localMediaRepositoryProvider = Provider<LocalMediaRepository>(
  (ref) => SqliteLocalMediaRepository(ref.watch(localLibraryDatabaseProvider)),
);

final localProgressRepositoryProvider = Provider<LocalProgressRepository>(
  (ref) =>
      SqliteLocalProgressRepository(ref.watch(localLibraryDatabaseProvider)),
);

final localGrantCleanupRepositoryProvider =
    Provider<LocalGrantCleanupRepository>(
  (ref) => SqliteLocalGrantCleanupRepository(
    ref.watch(localLibraryDatabaseProvider),
  ),
);

final localDocumentPickerProvider = Provider<LocalDocumentPicker>(
  (ref) => MethodChannelLocalDocumentPicker(),
);

final localDocumentUsageProvider = Provider<LocalDocumentUsage>(
  (ref) => _PlaybackDocumentUsage(
    ref.watch(playbackSessionCoordinatorProvider),
  ),
);

class _PlaybackDocumentUsage implements LocalDocumentPlaybackUsage {
  _PlaybackDocumentUsage(this.playback);

  final PlaybackSessionCoordinator playback;

  @override
  bool isInUse(String uri) => playback.isLocalSourceInUse(uri);

  @override
  Future<void> stopUsing(String uri) => playback.stopLocalSource(uri);
}

final localLibraryManagerProvider = Provider<LocalLibraryManager>((ref) {
  return LocalLibraryManager(
    picker: ref.watch(localDocumentPickerProvider),
    media: ref.watch(localMediaRepositoryProvider),
    cleanup: ref.watch(localGrantCleanupRepositoryProvider),
    usage: ref.watch(localDocumentUsageProvider),
  );
});

final localMediaListProvider = FutureProvider<List<LocalMedia>>(
  (ref) async {
    final items = await ref.watch(localMediaRepositoryProvider).list();
    await ref.read(localLibraryManagerProvider).retryPendingGrantReleases();
    return items;
  },
);

final localMediaByIdProvider =
    FutureProvider.autoDispose.family<LocalMedia?, int>((ref, id) {
  return ref.watch(localMediaRepositoryProvider).getById(id);
});
