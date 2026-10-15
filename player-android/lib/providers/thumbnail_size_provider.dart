import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

const thumbnailScales = [0.65, 0.8, 1.0, 1.3, 1.65];
const defaultThumbnailSize = 2;
const _storageKey = 'library_thumbnail_size';

class ThumbnailSizeNotifier extends AsyncNotifier<int> {
  Future<void> _writes = Future.value();

  @override
  Future<int> build() async {
    try {
      final saved = (await SharedPreferences.getInstance()).getInt(_storageKey);
      if (saved != null && saved >= 0 && saved < thumbnailScales.length) {
        return saved;
      }
    } catch (_) {
      // An unavailable or malformed preference must not block the library.
    }
    return defaultThumbnailSize;
  }

  Future<void> setSize(int requested) async {
    await future;
    final index = requested.clamp(0, thumbnailScales.length - 1).toInt();
    state = AsyncData(index);
    // Preserve the order of rapid button presses without blocking the UI.
    _writes = _writes.catchError((Object _) {}).then((_) async {
      final prefs = await SharedPreferences.getInstance();
      if (!await prefs.setInt(_storageKey, index)) {
        throw StateError('Could not save preview size');
      }
    });
    return _writes;
  }
}

final thumbnailSizeProvider = AsyncNotifierProvider<ThumbnailSizeNotifier, int>(
    ThumbnailSizeNotifier.new);
