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

  /// Returns all media items the authenticated user has started but not finished.
  ///
  /// GET /api/v1/in-progress — returns the same [Media] array as GET /api/v1/media.
  /// The caller (ContinueWatchingScreen) uses this to populate the resume list.
  @override
  Future<List<Media>> listInProgress() async {
    final response = await rawDio.get<List<dynamic>>('$_kApiV1/in-progress');
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(Media.fromJson)
        .toList();
  }

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
  // Tags
  // ---------------------------------------------------------------------------

  /// Returns all tag names visible to the authenticated user.
  ///
  /// GET /api/v1/tags — returns [{"id": 1, "name": "documentary"}, ...]
  /// Used by the tag-picker autocomplete to offer suggestions.
  @override
  Future<List<Tag>> listTags() async {
    final response = await rawDio.get<List<dynamic>>('$_kApiV1/tags');
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(Tag.fromJson)
        .toList();
  }

  /// Attaches a tag to a media item by name.
  ///
  /// POST /api/v1/media/{id}/tags  body: {"tag": "<name>"}
  /// Returns 200 {"status": "ok"} on success; throws [DioException] on error.
  @override
  Future<void> addTag(int mediaId, String tag) async {
    await rawDio.post<void>(
      '$_kApiV1/media/$mediaId/tags',
      data: {'tag': tag},
    );
  }

  /// Removes a named tag from a media item.
  ///
  /// DELETE /api/v1/media/{id}/tags/{tag} where {tag} is URL-encoded.
  /// Returns 200 {"status": "ok"} on success; throws [DioException] on error.
  @override
  Future<void> removeTag(int mediaId, String tag) async {
    // Uri.encodeComponent encodes the tag name so characters like spaces or
    // slashes in tag names do not break the URL path segment.
    final encoded = Uri.encodeComponent(tag);
    await rawDio.delete<void>('$_kApiV1/media/$mediaId/tags/$encoded');
  }

  // ---------------------------------------------------------------------------
  // Favourites
  // ---------------------------------------------------------------------------

  /// Toggles the authenticated user's favourite status for [mediaId].
  ///
  /// POST /api/v1/media/{id}/favorite
  ///
  /// The server flips the current favourite state and responds with:
  ///   `{ "favorite": true | false }`
  ///
  /// Returns the **new** favourite state so the caller can reconcile the UI
  /// without performing a second GET request.
  @override
  Future<bool> toggleFavorite(int mediaId) async {
    final response = await rawDio.post<Map<String, dynamic>>(
      '$_kApiV1/media/$mediaId/favorite',
    );
    // The envelope always contains a boolean "favorite" field per the API spec.
    final data = response.data ?? {};
    return (data['favorite'] as bool?) ?? false;
  }

  // ---------------------------------------------------------------------------
  // Shares
  // ---------------------------------------------------------------------------

  /// Creates a share link for [mediaId].
  ///
  /// POST /api/v1/media/{id}/shares
  ///
  /// [expiresAt] and [maxUses] are sent as optional JSON fields so the server
  /// can apply custom expiry and use-count limits in place of its built-in
  /// defaults.  Null values are omitted from the request body so the server
  /// falls back to [SHARE_DEFAULT_EXPIRY_DAYS] for expiry and unlimited uses.
  ///
  /// Returns the newly created [Share] on success.
  @override
  Future<Share> createShare(
    int mediaId, {
    DateTime? expiresAt,
    int? maxUses,
  }) async {
    // Build the optional request body; omit null fields so the server applies
    // its own defaults rather than receiving explicit nulls.
    final body = <String, dynamic>{
      if (expiresAt != null) 'expires_at': expiresAt.toUtc().toIso8601String(),
      if (maxUses != null) 'max_uses': maxUses,
    };

    final response = await rawDio.post<Map<String, dynamic>>(
      '$_kApiV1/media/$mediaId/shares',
      data: body.isNotEmpty ? body : null,
    );
    return Share.fromJson(response.data!);
  }

  // ---------------------------------------------------------------------------
  // Podcasts
  // ---------------------------------------------------------------------------

  /// Subscribes to a new podcast feed and returns the created [PodcastFeed].
  ///
  /// POST /api/v1/podcasts
  /// Requires admin privileges (the server returns 403 for non-admin users).
  ///
  /// [feedUrl] is the URL of the RSS/Atom feed to subscribe to.
  /// [setName] is an optional human-readable name for the podcast set;
  /// when omitted the server derives it from the feed's own title element.
  @override
  Future<PodcastFeed> subscribePodcast({
    required String feedUrl,
    String? setName,
  }) async {
    // Omit set_name from the request body when not provided so the server falls
    // back to the feed's title rather than receiving an explicit null.
    final body = <String, dynamic>{
      'feed_url': feedUrl,
      if (setName != null && setName.isNotEmpty) 'set_name': setName,
    };

    final response = await rawDio.post<Map<String, dynamic>>(
      '$_kApiV1/podcasts',
      data: body,
    );
    return PodcastFeed.fromJson(response.data!);
  }

  // ---------------------------------------------------------------------------
  // Notes
  // ---------------------------------------------------------------------------

  /// Returns the authenticated user's note for [mediaId], or `null` if none.
  ///
  /// GET /api/v1/media/{id}/notes
  /// The server responds with 200 + a Note JSON object when a note exists, or
  /// 204 No Content when there is no note.  Dio raises no exception on 204, so
  /// we detect the empty body and return null rather than trying to decode it.
  @override
  Future<Note?> getNote(int mediaId) async {
    final response = await rawDio.get<dynamic>(
      '$_kApiV1/media/$mediaId/notes',
    );
    // 204 No Content — the server signals "no note exists" with an empty body.
    if (response.statusCode == 204 || response.data == null) return null;
    final data = response.data;
    if (data is Map<String, dynamic>) return Note.fromJson(data);
    return null;
  }

  /// Creates or updates the authenticated user's note for [mediaId].
  ///
  /// POST /api/v1/media/{id}/notes  body: {"content": "<text>"}
  /// Returns the saved [Note] on success.
  @override
  Future<Note> upsertNote(int mediaId, String content) async {
    final response = await rawDio.post<Map<String, dynamic>>(
      '$_kApiV1/media/$mediaId/notes',
      data: {'content': content},
    );
    return Note.fromJson(response.data!);
  }

  /// Deletes the authenticated user's note for [mediaId].
  ///
  /// DELETE /api/v1/media/{id}/notes — returns 200 {"status": "ok"}.
  @override
  Future<void> deleteNote(int mediaId) async {
    await rawDio.delete<void>('$_kApiV1/media/$mediaId/notes');
  }

  // ---------------------------------------------------------------------------
  // Shares (authenticated — per-user management)
  // ---------------------------------------------------------------------------

  /// Lists active shares for a specific media item.
  ///
  /// GET /api/v1/media/{id}/shares
  /// Returns all [Share] objects currently active for the given media item.
  @override
  Future<List<Share>> listSharesForMedia(int mediaId) async {
    final response = await rawDio.get<List<dynamic>>(
      '$_kApiV1/media/$mediaId/shares',
    );
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(Share.fromJson)
        .toList();
  }

  /// Lists all share links created by the authenticated user.
  ///
  /// GET /api/v1/shares — returns a flat list of all shares the caller owns,
  /// across all media items.
  @override
  Future<List<Share>> listMyShares() async {
    final response = await rawDio.get<List<dynamic>>('$_kApiV1/shares');
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(Share.fromJson)
        .toList();
  }

  /// Revokes a share link by its [token].
  ///
  /// DELETE /api/v1/shares/{token} — only the creator can revoke their share.
  /// Returns immediately; callers should remove the share from any local cache.
  @override
  Future<void> revokeShare(String token) async {
    await rawDio.delete<void>('$_kApiV1/shares/$token');
  }

  // ---------------------------------------------------------------------------
  // Shared / public endpoints (no auth required)
  // ---------------------------------------------------------------------------

  /// Returns the share viewer page JSON for the given share [token].
  ///
  /// GET /s/{token} with Accept: application/json — returns the share metadata
  /// as a JSON string (the raw response body) so the caller can display media
  /// info without authentication.
  ///
  /// Note: this endpoint sits outside the /api/v1/ prefix; it has no v1 alias.
  @override
  Future<String> getSharedMediaPage(String token) async {
    final response = await rawDio.get<dynamic>(
      '/s/$token',
      options: Options(
        headers: {'Accept': 'application/json'},
        responseType: ResponseType.plain,
      ),
    );
    return (response.data as String?) ?? '';
  }

  /// Streams a shared media file, optionally from a byte [range] offset.
  ///
  /// GET /s/{token}/stream — no auth required; supports the Range header.
  @override
  Future<Uint8List> streamSharedMedia(String token, {String? range}) {
    final extraHeaders =
        range != null ? <String, dynamic>{'Range': range} : null;
    return _getBytesFromUrl('/s/$token/stream', extraHeaders: extraHeaders);
  }

  /// Returns the thumbnail image for a shared media item.
  ///
  /// GET /s/{token}/thumbnail — no auth required.
  @override
  Future<Uint8List> getSharedThumbnail(String token) =>
      _getBytesFromUrl('/s/$token/thumbnail');

  /// Downloads the original file for a shared media item.
  ///
  /// GET /s/{token}/download — no auth required; sets Content-Disposition.
  @override
  Future<Uint8List> downloadSharedMedia(String token) =>
      _getBytesFromUrl('/s/$token/download');

  // ---------------------------------------------------------------------------
  // Config
  // ---------------------------------------------------------------------------

  /// Returns client configuration from the server.
  ///
  /// GET /api/v1/config — currently exposes the server-side page size so
  /// clients can paginate consistently with the server defaults.
  @override
  Future<Map<String, dynamic>> getConfig() async {
    final response = await rawDio.get<Map<String, dynamic>>('$_kApiV1/config');
    return response.data ?? {};
  }

  // ---------------------------------------------------------------------------
  // Sets (remaining methods)
  // ---------------------------------------------------------------------------

  /// Returns the cover image bytes for a set or subfolder.
  ///
  /// GET /api/v1/sets/{id}/cover?folder=...
  /// The optional [folder] query parameter scopes the cover to a subfolder.
  @override
  Future<Uint8List> getSetCover(int setId, {String? folder}) {
    final query = folder != null ? '?folder=${Uri.encodeComponent(folder)}' : '';
    return _getBytesFromUrl('$_kApiV1/sets/$setId/cover$query');
  }

  /// Regenerates the cover image for a set or subfolder.
  ///
  /// POST /api/v1/sets/{id}/cover?folder=...
  /// Requires owner permission on the set.  The server regenerates the cover
  /// synchronously using ffmpeg and returns {"status": "ok"}.
  @override
  Future<void> updateSetCover(int setId, {String? folder}) async {
    await rawDio.post<void>(
      '$_kApiV1/sets/$setId/cover',
      queryParameters: {if (folder != null) 'folder': folder},
    );
  }

  /// Uploads a media file to a set using multipart/form-data.
  ///
  /// POST /api/v1/sets/{id}/upload — requires `owner` permission.
  /// [fileName] is the name used for the Content-Disposition filename.
  /// [bytes] is the raw file content as a byte list.
  ///
  /// Returns the newly created [Media] object on success.
  @override
  Future<Media> uploadToSet(
    int setId, {
    required String fileName,
    required List<int> bytes,
  }) async {
    // Wrap the bytes in a Dio MultipartFile so the request uses the correct
    // Content-Type: multipart/form-data encoding expected by the server.
    final formData = FormData.fromMap({
      'file': MultipartFile.fromBytes(bytes, filename: fileName),
    });

    final response = await rawDio.post<Map<String, dynamic>>(
      '$_kApiV1/sets/$setId/upload',
      data: formData,
    );
    return Media.fromJson(response.data!);
  }

  // ---------------------------------------------------------------------------
  // Media (remaining methods)
  // ---------------------------------------------------------------------------

  /// Regenerates the thumbnail for a media item using ffmpeg.
  ///
  /// POST /api/v1/media/{id}/thumbnail — requires owner permission.
  @override
  Future<void> regenerateThumbnail(int mediaId) async {
    await rawDio.post<void>('$_kApiV1/media/$mediaId/thumbnail');
  }

  /// Soft-deletes a media item, moving it to trash.
  ///
  /// DELETE /api/v1/media/{id} — item is excluded from GET /api/v1/media
  /// until restored.  Requires owner permission or admin.
  @override
  Future<void> deleteMedia(int mediaId) async {
    await rawDio.delete<void>('$_kApiV1/media/$mediaId');
  }

  /// Restores a soft-deleted media item from trash.
  ///
  /// POST /api/v1/media/{id}/restore — requires owner permission or admin.
  @override
  Future<void> restoreMedia(int mediaId) async {
    await rawDio.post<void>('$_kApiV1/media/$mediaId/restore');
  }

  // ---------------------------------------------------------------------------
  // Progress – batch
  // ---------------------------------------------------------------------------

  /// Submits multiple playback progress updates in a single request.
  ///
  /// POST /api/v1/progress/batch — designed for offline clients that
  /// accumulate updates while disconnected and sync on reconnect.
  ///
  /// Each item in [updates] must have `media_id` (int),
  /// `position_seconds` (double), and `observed_at` (ISO-8601 UTC string).
  /// The server processes them in `observed_at` order so older updates never
  /// overwrite newer ones.
  @override
  Future<void> batchUpdateProgress(
    List<Map<String, dynamic>> updates,
  ) async {
    await rawDio.post<void>(
      '$_kApiV1/progress/batch',
      data: {'updates': updates},
    );
  }

  // ---------------------------------------------------------------------------
  // Podcasts (remaining methods)
  // ---------------------------------------------------------------------------

  /// Lists all subscribed podcast feeds visible to the authenticated user.
  ///
  /// GET /api/v1/podcasts — returns all [PodcastFeed] objects the user can see.
  @override
  Future<List<PodcastFeed>> listPodcasts() async {
    final response = await rawDio.get<List<dynamic>>('$_kApiV1/podcasts');
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(PodcastFeed.fromJson)
        .toList();
  }

  /// Lists episodes for a podcast feed identified by its set ID.
  ///
  /// GET /api/v1/podcasts/{id}/episodes — [podcastSetId] is the **set ID**
  /// (not the feed ID).  Supports optional pagination via [limit] and [offset].
  @override
  Future<List<PodcastEpisode>> listEpisodes(
    int podcastSetId, {
    int? limit,
    int? offset,
  }) async {
    final response = await rawDio.get<List<dynamic>>(
      '$_kApiV1/podcasts/$podcastSetId/episodes',
      queryParameters: {
        if (limit != null) 'limit': limit,
        if (offset != null) 'offset': offset,
      },
    );
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(PodcastEpisode.fromJson)
        .toList();
  }

  /// Triggers a server-side download of a podcast episode.
  ///
  /// POST /api/v1/podcasts/episodes/{episode_id}/download
  /// The server downloads the audio file and creates a [Media] row for it.
  /// Returns the newly created [Media] on success.
  @override
  Future<Media> downloadEpisode(int episodeId) async {
    final response = await rawDio.post<Map<String, dynamic>>(
      '$_kApiV1/podcasts/episodes/$episodeId/download',
    );
    return Media.fromJson(response.data!);
  }

  /// Toggles the per-user completion state of a podcast episode.
  ///
  /// POST /api/v1/podcasts/episodes/{episode_id}/complete
  /// Returns 204 No Content — the new state must be re-fetched if needed.
  @override
  Future<void> toggleEpisodeComplete(int episodeId) async {
    await rawDio.post<void>(
      '$_kApiV1/podcasts/episodes/$episodeId/complete',
    );
  }

  // ---------------------------------------------------------------------------
  // Admin – Users
  // ---------------------------------------------------------------------------

  /// Lists all registered user accounts.
  ///
  /// GET /api/v1/admin/users — requires admin.
  @override
  Future<List<User>> listUsers() async {
    final response = await rawDio.get<List<dynamic>>('$_kApiV1/admin/users');
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(User.fromJson)
        .toList();
  }

  /// Creates a new user account.
  ///
  /// POST /api/v1/admin/users — requires admin.
  /// [isAdmin] controls whether the new account has administrative privileges.
  @override
  Future<User> createUser({
    required String username,
    required String password,
    required bool isAdmin,
  }) async {
    final response = await rawDio.post<Map<String, dynamic>>(
      '$_kApiV1/admin/users',
      data: {
        'username': username,
        'password': password,
        'is_admin': isAdmin,
      },
    );
    return User.fromJson(response.data!);
  }

  /// Deletes a user account by [userId].
  ///
  /// DELETE /api/v1/admin/users/{id} — requires admin.
  /// Admins cannot delete themselves (server returns 400 in that case).
  @override
  Future<void> deleteUser(int userId) async {
    await rawDio.delete<void>('$_kApiV1/admin/users/$userId');
  }

  // ---------------------------------------------------------------------------
  // Admin – Permissions
  // ---------------------------------------------------------------------------

  /// Returns the full permission matrix: sets, users, and permission rows.
  ///
  /// GET /api/v1/admin/permissions — requires admin.
  /// The raw map is returned because the response combines three distinct object
  /// types (sets, users, and permission rows) that have no single unified model.
  @override
  Future<Map<String, dynamic>> listPermissions() async {
    final response = await rawDio.get<Map<String, dynamic>>(
      '$_kApiV1/admin/permissions',
    );
    return response.data ?? {};
  }

  /// Grants [userId] access to [setId] with the given [role].
  ///
  /// POST /api/v1/admin/permissions — requires admin.
  /// [role] must be either `"owner"` (can upload/delete) or `"viewer"`.
  @override
  Future<void> grantPermission({
    required int setId,
    required int userId,
    required String role,
  }) async {
    await rawDio.post<void>(
      '$_kApiV1/admin/permissions',
      data: {
        'set_id': setId,
        'user_id': userId,
        'role': role,
      },
    );
  }

  /// Revokes [userId]'s access to [setId].
  ///
  /// DELETE /api/v1/admin/permissions — requires admin.
  /// The body carries the set and user IDs because Dio DELETE requests support
  /// request bodies and the server requires them.
  @override
  Future<void> revokePermission({
    required int setId,
    required int userId,
  }) async {
    await rawDio.delete<void>(
      '$_kApiV1/admin/permissions',
      data: {
        'set_id': setId,
        'user_id': userId,
      },
    );
  }

  // ---------------------------------------------------------------------------
  // Admin – Scanner
  // ---------------------------------------------------------------------------

  /// Triggers an asynchronous library rescan.
  ///
  /// POST /api/v1/admin/rescan — requires admin.
  /// The scan runs in the background; poll [getScanProgress] to track it.
  @override
  Future<void> triggerRescan() async {
    await rawDio.post<void>('$_kApiV1/admin/rescan');
  }

  /// Returns the current or most recent scan progress state.
  ///
  /// GET /api/v1/admin/scan-progress — requires admin.
  /// Returns a raw map with fields: running, current_set, sets_total,
  /// sets_done, files_total, files_done, last_error.
  @override
  Future<Map<String, dynamic>> getScanProgress() async {
    final response = await rawDio.get<Map<String, dynamic>>(
      '$_kApiV1/admin/scan-progress',
    );
    return response.data ?? {};
  }

  /// Lists all soft-deleted media items (the trash).
  ///
  /// GET /api/v1/admin/trash — requires admin.
  /// Returns [Media] objects with `deleted_at` set.
  @override
  Future<List<Media>> listTrash() async {
    final response = await rawDio.get<List<dynamic>>('$_kApiV1/admin/trash');
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(Media.fromJson)
        .toList();
  }

  // ---------------------------------------------------------------------------
  // API Tokens
  // ---------------------------------------------------------------------------

  /// Lists all API tokens belonging to the authenticated user.
  ///
  /// GET /api/v1/auth/tokens — plaintext values are never returned here.
  /// Returns raw maps because there is no dedicated [ApiToken] model yet.
  @override
  Future<List<Map<String, dynamic>>> listAPITokens() async {
    final response = await rawDio.get<List<dynamic>>('$_kApiV1/auth/tokens');
    return (response.data ?? []).cast<Map<String, dynamic>>().toList();
  }

  /// Mints a new Bearer API token for the authenticated user.
  ///
  /// POST /api/v1/auth/tokens — the plaintext token is returned **once** in
  /// the `token` field and never again.  Store it securely on the device.
  ///
  /// [name] is a human-readable label (e.g. "android-client").
  /// [expiresInDays] is optional; omit for a non-expiring token.
  @override
  Future<Map<String, dynamic>> createAPIToken({
    required String name,
    int? expiresInDays,
  }) async {
    final body = <String, dynamic>{
      'name': name,
      if (expiresInDays != null) 'expires_in_days': expiresInDays,
    };
    final response = await rawDio.post<Map<String, dynamic>>(
      '$_kApiV1/auth/tokens',
      data: body,
    );
    return response.data!;
  }

  /// Revokes a Bearer API token by its numeric [tokenId].
  ///
  /// DELETE /api/v1/auth/tokens/{id} — returns 204 No Content.
  /// Only the owning user can revoke their own tokens.
  @override
  Future<void> revokeAPIToken(int tokenId) async {
    await rawDio.delete<void>('$_kApiV1/auth/tokens/$tokenId');
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  /// Issues a GET request with [ResponseType.bytes] and returns the response
  /// body as a [Uint8List].
  ///
  /// Shared by [streamMedia], [downloadMedia], [getThumbnail], and all binary
  /// shared-media endpoints to avoid repeating byte-response boilerplate.
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
