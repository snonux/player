import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/local_media.dart';
import '../services/local_library_repository.dart';
import '../services/local_document_picker.dart';
import '../services/local_library_manager.dart';

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

/// Playback owns additional URI grants while it is actively using a source.
/// Replace this default with the active-session tracker when local playback is
/// connected; the empty implementation is correct for the current library UI.
final localDocumentUsageProvider = Provider<LocalDocumentUsage>(
  (ref) => const NoActiveLocalDocumentUsage(),
);

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
