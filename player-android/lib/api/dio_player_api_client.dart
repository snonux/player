import 'dart:typed_data';

import 'package:dio/dio.dart';

import '../models/models.dart';
import 'player_api_client.dart';

// Base path prefix used by all versioned API endpoints.
const _kApiV1 = '/api/v1';

/// Concrete [PlayerApiClient] implementation that delegates every call to the
/// [Dio] instance supplied at construction time.
///
/// All HTTP details (auth header injection, 401 redirect) are already handled
/// by the interceptors wired into [dio] — this class is concerned only with
/// mapping routes and JSON to/from typed Dart values (Single Responsibility).
///
/// The class is intentionally thin: it does no caching, no retry logic, and no
/// business rules. Higher-level constructs (Riverpod notifiers, use-cases) are
/// responsible for those concerns (Separation of Concerns / ISP).
class DioPlayerApiClient extends PlayerApiClient {
  /// Constructs the client.
  ///
  /// In production [dio] should come from [DioClient] which adds bearer-token
  /// injection and 401→login redirect interceptors. In tests, pass a plain or
  /// mock [Dio] to keep tests fast and hermetic.
  DioPlayerApiClient({required super.dio});

  // ---------------------------------------------------------------------------
  // Auth
  // ---------------------------------------------------------------------------

  /// Creates the first admin account (bootstrap flow).
  ///
  /// POST /api/v1/auth/bootstrap
  /// Returns a [User] and sets a session cookie on the Dio CookieJar (if any).
  @override
  Future<User> bootstrap({
    required String username,
    required String password,
  }) async {
    final response = await rawDio.post<Map<String, dynamic>>(
      '$_kApiV1/auth/bootstrap',
      data: {'username': username, 'password': password},
    );
    return User.fromJson(response.data!);
  }

  /// Authenticates with username/password and returns the logged-in [User].
  ///
  /// POST /api/v1/auth/login
  /// Sets a session cookie for subsequent cookie-authenticated requests.
  @override
  Future<User> login({
    required String username,
    required String password,
  }) async {
    final response = await rawDio.post<Map<String, dynamic>>(
      '$_kApiV1/auth/login',
      data: {'username': username, 'password': password},
    );
    return User.fromJson(response.data!);
  }

  /// Invalidates the current session cookie.
  ///
  /// POST /api/v1/logout — returns 204 No Content.
  /// Bearer-authenticated clients should revoke the token directly instead.
  @override
  Future<void> logout() async {
    await rawDio.post<void>('$_kApiV1/logout');
  }

  // ---------------------------------------------------------------------------
  // Health
  // ---------------------------------------------------------------------------

  /// Returns the total number of registered users from the server.
  ///
  /// GET /api/v1/auth/count — public endpoint; no session required.
  /// Mobile clients call this on startup to detect first-run (count == 0)
  /// and redirect to /bootstrap instead of /login.
  @override
  Future<int> countUsers() async {
    final response = await rawDio.get<Map<String, dynamic>>(
      '$_kApiV1/auth/count',
    );
    return (response.data?['count'] as int?) ?? 0;
  }

  /// Liveness probe — returns immediately without touching the database.
  ///
  /// GET /healthz — 200 means the server process is alive.
  @override
  Future<void> healthz() async {
    // Health endpoints sit outside the /api/v1/ prefix by convention.
    await rawDio.get<void>('/healthz');
  }

  /// Readiness probe — pings the database and returns 503 if unavailable.
  ///
  /// GET /readyz — 200 means the server can serve traffic.
  @override
  Future<void> readyz() async {
    await rawDio.get<void>('/readyz');
  }

  // ---------------------------------------------------------------------------
  // Sets
  // ---------------------------------------------------------------------------

  /// Returns all sets visible to the authenticated user.
  ///
  /// GET /api/v1/sets
  @override
  Future<List<MediaSet>> listSets() async {
    final response = await rawDio.get<List<dynamic>>('$_kApiV1/sets');
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(MediaSet.fromJson)
        .toList();
  }

  /// Browses the folder tree within a set, optionally scoped to [parent].
  ///
  /// GET /api/v1/sets/{id}/browse?parent=...
  /// Returns the raw JSON map because the response shape (folders, media,
  /// episodes) is context-dependent and has no single model counterpart yet.
  @override
  Future<Map<String, dynamic>> browseSet(int setId, {String? parent}) async {
    final response = await rawDio.get<Map<String, dynamic>>(
      '$_kApiV1/sets/$setId/browse',
      queryParameters: {if (parent != null) 'parent': parent},
    );
    return response.data ?? {};
  }

  // ---------------------------------------------------------------------------
  // Media
  // ---------------------------------------------------------------------------

  /// Lists or searches media visible to the authenticated user.
  ///
  /// GET /api/v1/media — supports a rich set of query parameters for filtering,
  /// sorting, and pagination. All parameters are optional.
  @override
  Future<List<Media>> listMedia({
    String? search,
    int? setId,
    List<int>? setIds,
    String? type,
    bool? favorites,
    List<String>? tags,
    double? minDuration,
    double? maxDuration,
    int? fileSizeMin,
    int? fileSizeMax,
    String? sort,
    int? limit,
    int? offset,
    String? folder,
    String? parent,
  }) async {
    // Build the query-parameter map, omitting null values so they are not sent.
    final params = <String, dynamic>{
      if (search != null) 'search': search,
      if (setId != null) 'set_id': setId,
      if (setIds != null && setIds.isNotEmpty)
        // The server expects a comma-separated string for set_ids.
        'set_ids': setIds.join(','),
      if (type != null) 'type': type,
      if (favorites != null) 'favorites': favorites ? 'true' : 'false',
      if (tags != null && tags.isNotEmpty) 'tags': tags.join(','),
      if (minDuration != null) 'min_duration': minDuration,
      if (maxDuration != null) 'max_duration': maxDuration,
      if (fileSizeMin != null) 'filesize_min': fileSizeMin,
      if (fileSizeMax != null) 'filesize_max': fileSizeMax,
      if (sort != null) 'sort': sort,
      if (limit != null) 'limit': limit,
      if (offset != null) 'offset': offset,
      if (folder != null) 'folder': folder,
      if (parent != null) 'parent': parent,
    };

    final response = await rawDio.get<List<dynamic>>(
      '$_kApiV1/media',
      queryParameters: params,
    );
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(Media.fromJson)
        .toList();
  }

  /// Returns a single media item including tags, favorite state, note, and
  /// saved playback progress.
  ///
  /// GET /api/v1/media/{id}
  /// The server envelope wraps the media object; this method unwraps it so
  /// callers receive a plain [Media].
  @override
  Future<Media> getMedia(int mediaId) async {
    final response = await rawDio.get<Map<String, dynamic>>(
      '$_kApiV1/media/$mediaId',
    );

    // Guard against a null or structurally unexpected response body.  In
    // practice Dio raises a DioException before we get here, but being
    // defensive avoids a crash if the server sends an empty 200.
    final envelope = response.data ?? {};

    // The API returns {"media": {...}, "tags": [...], "favorite": bool, ...}.
    // Merge the top-level `tags` list and `favorite` flag into the nested media
    // map before deserialising so Media.fromJson picks them up correctly.
    final rawMedia = envelope['media'];
    final mediaMap = rawMedia is Map<String, dynamic>
        ? Map<String, dynamic>.from(rawMedia)
        : <String, dynamic>{};

    // Inject the per-user fields from the envelope into the media map.
    final rawTags = envelope['tags'];
    if (rawTags is List) {
      // Tags are returned as [{id, name}, ...]; extract the name strings.
      mediaMap['tags'] =
          rawTags.cast<Map<String, dynamic>>().map((t) => t['name']).toList();
    }

    if (envelope['favorite'] is bool) {
      mediaMap['favorite'] = envelope['favorite'] as bool;
    }

    return Media.fromJson(mediaMap);
  }

  /// Streams a media file, optionally from a byte [range] offset.
  ///
  /// GET /api/v1/media/{id}/stream
  /// Supports the standard HTTP Range header for seeking. Returns the raw bytes
  /// so the caller can feed them to a local file or a video player.
  @override
  Future<Uint8List> streamMedia(int mediaId, {String? range}) {
    // Only set the Range header when a range is actually requested; an empty
    // headers map is harmless but adds noise to the request.
    final extraHeaders =
        range != null ? <String, dynamic>{'Range': range} : null;
    return _getBytesFromUrl(
      '$_kApiV1/media/$mediaId/stream',
      extraHeaders: extraHeaders,
    );
  }

  /// Downloads the original media file with Content-Disposition: attachment.
  ///
  /// GET /api/v1/media/{id}/download
  @override
  Future<Uint8List> downloadMedia(int mediaId) =>
      _getBytesFromUrl('$_kApiV1/media/$mediaId/download');

  /// Returns the JPEG thumbnail for a media item.
  ///
  /// GET /api/v1/media/{id}/thumbnail
  @override
  Future<Uint8List> getThumbnail(int mediaId) =>
      _getBytesFromUrl('$_kApiV1/media/$mediaId/thumbnail');

  // ---------------------------------------------------------------------------
  // Progress
  // ---------------------------------------------------------------------------

  /// Returns the last saved playback position for [mediaId], or `null`.
  ///
  /// GET /api/v1/media/{id} — extracts the `progress.position_seconds` field
  /// from the response envelope.  Returns `null` when there is no saved
  /// progress or when the item has been marked as finished (so the player
  /// starts from the beginning rather than the very end).
  @override
  Future<double?> getMediaProgress(int mediaId) async {
    final response = await rawDio.get<Map<String, dynamic>>(
      '$_kApiV1/media/$mediaId',
    );

    final envelope = response.data ?? {};
    final progress = envelope['progress'];

    // Return null when there is no progress row or when the item is finished
    // (a finished item should restart from the beginning, not resume near end).
    if (progress is! Map<String, dynamic>) return null;
    if (progress['finished'] == true) return null;

    final positionSeconds = progress['position_seconds'];
    if (positionSeconds is num) return positionSeconds.toDouble();
    return null;
  }

  /// Saves a playback position for a single media item.
  ///
  /// POST /api/v1/progress
  /// Call this periodically while the user is playing (e.g. every 5 seconds).
  /// The server increments [play_count] based on a 60-second accumulator so
  /// frequent updates are both safe and encouraged.
  @override
  Future<void> updateProgress({
    required int mediaId,
    required double positionSeconds,
  }) async {
    await rawDio.post<void>(
      '$_kApiV1/progress',
      data: {
        'media_id': mediaId,
        'position_seconds': positionSeconds,
      },
    );
  }

  /// Marks a media item as finished or resets its progress.
  ///
  /// POST /api/v1/progress/status
  /// [status] must be either `"finished"` or `"not_started"`.
  /// Use `"finished"` when playback reaches the 95% threshold.
  @override
  Future<void> updateProgressStatus({
    required int mediaId,
    required String status,
  }) async {
    await rawDio.post<void>(
      '$_kApiV1/progress/status',
      data: {
        'media_id': mediaId,
        'status': status,
      },
    );
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  /// Issues a GET request with [ResponseType.bytes] and returns the response
  /// body as a [Uint8List].
  ///
  /// Shared by [streamMedia], [downloadMedia], and [getThumbnail] to avoid
  /// repeating the same byte-response boilerplate in each method.
  Future<Uint8List> _getBytesFromUrl(
    String path, {
    Map<String, dynamic>? extraHeaders,
  }) async {
    final response = await rawDio.get<List<int>>(
      path,
      options: Options(
        responseType: ResponseType.bytes,
        headers: extraHeaders,
      ),
    );
    return Uint8List.fromList(response.data ?? []);
  }
}
