/// A device-local document indexed by its opaque Storage Access Framework URI.
/// This is separate from the server-owned Media model and IDs.
class LocalMedia {
  const LocalMedia({
    required this.id,
    required this.uri,
    required this.title,
    required this.mimeType,
    required this.addedAt,
    this.ownsPersistedReadGrant = false,
    this.sizeBytes,
    this.durationMs,
    this.lastOpenedAt,
  });

  final int id;
  final String uri;
  final String title;
  final String mimeType;
  final bool ownsPersistedReadGrant;
  final int? sizeBytes;
  final int? durationMs;
  final DateTime addedAt;
  final DateTime? lastOpenedAt;
}

/// Last locally recorded playback state for a [LocalMedia] item.
class LocalProgress {
  const LocalProgress({
    required this.localMediaId,
    required this.positionSeconds,
    required this.finished,
    required this.updatedAt,
  });

  final int localMediaId;
  final double positionSeconds;
  final bool finished;
  final DateTime updatedAt;
}
