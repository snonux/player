import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/local_library_repository.dart';

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
