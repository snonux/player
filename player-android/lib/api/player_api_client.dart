import 'dart:typed_data';

import 'package:dio/dio.dart';

import '../models/models.dart';

/// High-level API surface that maps 1-to-1 with the player-server REST API
/// (see player-server/docs/api.md for the authoritative contract).
///
/// Each method corresponds to a single HTTP endpoint.  All HTTP plumbing
/// (bearer-token injection, 401 → login redirect, base-URL configuration) is
/// handled by the [Dio] instance provided at construction time.
///
/// In production, create the [Dio] via [DioClient] which wires up the auth
/// and 401-redirect interceptors.  In tests, pass a plain or mocked [Dio].
///
/// Concrete implementations of the stub methods will be added incrementally as
/// features are built.
class PlayerApiClient {
  /// Creates a client backed by [dio].
  ///
  /// Prefer creating [dio] via [DioClient] in production to get bearer-token
  /// injection and 401 → login redirect out of the box.
  PlayerApiClient({required Dio dio}) : _dio = dio;

  // The configured Dio instance with auth/401 interceptors already applied.
  final Dio _dio;

  // ---------------------------------------------------------------------------
  // Auth
  // ---------------------------------------------------------------------------

  Future<User> bootstrap({
    required String username,
    required String password,
  }) =>
      throw UnimplementedError();

  Future<User> login({
    required String username,
    required String password,
  }) =>
      throw UnimplementedError();

  Future<void> logout() => throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Health
  // ---------------------------------------------------------------------------

  Future<void> healthz() => throw UnimplementedError();
  Future<void> readyz() => throw UnimplementedError();

  /// Returns the total number of registered users.
  ///
  /// Clients use this to detect first-run (count == 0) and redirect to the
  /// bootstrap screen instead of the login screen.
  Future<int> countUsers() => throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Shared / public endpoints (no auth required)
  // ---------------------------------------------------------------------------

  Future<String> getSharedMediaPage(String token) =>
      throw UnimplementedError();

  Future<Uint8List> streamSharedMedia(String token, {String? range}) =>
      throw UnimplementedError();

  Future<Uint8List> getSharedThumbnail(String token) =>
      throw UnimplementedError();

  Future<Uint8List> downloadSharedMedia(String token) =>
      throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Config
  // ---------------------------------------------------------------------------

  Future<Map<String, dynamic>> getConfig() => throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Sets
  // ---------------------------------------------------------------------------

  Future<List<MediaSet>> listSets() => throw UnimplementedError();

  Future<Map<String, dynamic>> browseSet(int setId, {String? parent}) =>
      throw UnimplementedError();

  Future<Uint8List> getSetCover(int setId, {String? folder}) =>
      throw UnimplementedError();

  Future<void> updateSetCover(int setId, {String? folder}) =>
      throw UnimplementedError();

  Future<Media> uploadToSet(
    int setId, {
    required String fileName,
    required List<int> bytes,
  }) =>
      throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Media
  // ---------------------------------------------------------------------------

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
  }) =>
      throw UnimplementedError();

  Future<Media> getMedia(int mediaId) => throw UnimplementedError();

  Future<Uint8List> streamMedia(int mediaId, {String? range}) =>
      throw UnimplementedError();

  Future<Uint8List> downloadMedia(int mediaId) => throw UnimplementedError();

  Future<Uint8List> getThumbnail(int mediaId) => throw UnimplementedError();

  /// Returns the URL for a media item's thumbnail image.
  ///
  /// Constructing the URL here (rather than in the screen layer) keeps the API
  /// path constant `/api/v1/media/{id}/thumbnail` in one place and avoids
  /// exposing [Dio] or its [BaseOptions] to the UI layer (Dependency Inversion).
  ///
  /// The base URL is derived from the underlying Dio instance so it is always
  /// consistent with the rest of the API calls.
  String thumbnailUrl(int mediaId) =>
      '${rawDio.options.baseUrl}/api/v1/media/$mediaId/thumbnail';

  /// Returns the URL used to stream a media item.
  ///
  /// Mirrors [thumbnailUrl]: the API path `/api/v1/media/{id}/stream` is kept
  /// in one place and Dio internals are never leaked into the UI layer.
  /// The base URL is derived from the underlying Dio instance so it stays
  /// consistent with every other API call.
  String streamUrl(int mediaId) =>
      '${rawDio.options.baseUrl}/api/v1/media/$mediaId/stream';

  /// Returns the URL for the cover image of a set's root or subfolder.
  ///
  /// Mirrors [thumbnailUrl] and [streamUrl]: the API path
  /// `/api/v1/sets/{id}/cover` is kept in one place so the UI layer never
  /// needs to access Dio internals or hard-code URL segments (DIP).
  ///
  /// [folder] is the optional subfolder path (empty or null = set root cover).
  String setFolderCoverUrl(int setId, {String? folder}) {
    final base = '${rawDio.options.baseUrl}/api/v1/sets/$setId/cover';
    if (folder == null || folder.isEmpty) return base;
    return '$base?folder=${Uri.encodeComponent(folder)}';
  }

  /// Returns the public share URL for a share [token].
  ///
  /// Mirrors [thumbnailUrl] and [streamUrl]: the share path `/s/{token}` is
  /// kept in one place so the UI layer never needs to access Dio internals or
  /// hard-code URL segments (Dependency Inversion Principle).
  String shareUrl(String token) => '${rawDio.options.baseUrl}/s/$token';

  Future<void> regenerateThumbnail(int mediaId) => throw UnimplementedError();

  Future<bool> toggleFavorite(int mediaId) => throw UnimplementedError();

  Future<void> deleteMedia(int mediaId) => throw UnimplementedError();

  Future<void> restoreMedia(int mediaId) => throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Tags
  // ---------------------------------------------------------------------------

  Future<List<Tag>> listTags() => throw UnimplementedError();

  Future<void> addTag(int mediaId, String tag) => throw UnimplementedError();

  Future<void> removeTag(int mediaId, String tag) =>
      throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Shares
  // ---------------------------------------------------------------------------

  Future<Share> createShare(
    int mediaId, {
    DateTime? expiresAt,
    int? maxUses,
  }) =>
      throw UnimplementedError();

  Future<List<Share>> listSharesForMedia(int mediaId) =>
      throw UnimplementedError();

  Future<List<Share>> listMyShares() => throw UnimplementedError();

  Future<void> revokeShare(String token) => throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Notes
  // ---------------------------------------------------------------------------

  Future<Note?> getNote(int mediaId) => throw UnimplementedError();

  Future<Note> upsertNote(int mediaId, String content) =>
      throw UnimplementedError();

  Future<void> deleteNote(int mediaId) => throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Progress
  // ---------------------------------------------------------------------------

  Future<void> updateProgress({
    required int mediaId,
    required double positionSeconds,
  }) =>
      throw UnimplementedError();

  Future<void> updateProgressStatus({
    required int mediaId,
    required String status,
  }) =>
      throw UnimplementedError();

  /// Returns the last saved playback position in seconds for [mediaId],
  /// or `null` if the user has never started this item.
  ///
  /// Fetches the progress from the `GET /api/v1/media/{id}` envelope rather
  /// than a dedicated progress endpoint, keeping the surface area small while
  /// still giving the player screen the position it needs for resume-on-open.
  Future<double?> getMediaProgress(int mediaId) => throw UnimplementedError();

  Future<List<Media>> listInProgress() => throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Podcasts
  // ---------------------------------------------------------------------------

  Future<List<PodcastFeed>> listPodcasts() => throw UnimplementedError();

  Future<List<PodcastEpisode>> listEpisodes(
    int podcastSetId, {
    int? limit,
    int? offset,
  }) =>
      throw UnimplementedError();

  Future<Media> downloadEpisode(int episodeId) => throw UnimplementedError();

  Future<void> toggleEpisodeComplete(int episodeId) =>
      throw UnimplementedError();

  Future<PodcastFeed> subscribePodcast({
    required String feedUrl,
    String? setName,
  }) =>
      throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Admin – Users
  // ---------------------------------------------------------------------------

  Future<List<User>> listUsers() => throw UnimplementedError();

  Future<User> createUser({
    required String username,
    required String password,
    required bool isAdmin,
  }) =>
      throw UnimplementedError();

  Future<void> deleteUser(int userId) => throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Admin – Permissions
  // ---------------------------------------------------------------------------

  /// Returns the full permission matrix (sets, users, and permission rows).
  ///
  /// GET /api/v1/admin/permissions — returns a map with keys `sets`, `users`,
  /// and `permissions`.  The raw map is returned because the response combines
  /// three distinct object types that have no single unified model.
  Future<Map<String, dynamic>> listPermissions() =>
      throw UnimplementedError();

  Future<void> grantPermission({
    required int setId,
    required int userId,
    required String role,
  }) =>
      throw UnimplementedError();

  Future<void> revokePermission({
    required int setId,
    required int userId,
  }) =>
      throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Admin – Scanner
  // ---------------------------------------------------------------------------

  Future<void> triggerRescan() => throw UnimplementedError();

  Future<Map<String, dynamic>> getScanProgress() => throw UnimplementedError();

  Future<List<Media>> listTrash() => throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // API Tokens
  // ---------------------------------------------------------------------------

  /// Lists all API tokens belonging to the authenticated user.
  ///
  /// Plaintext token values are never returned by this endpoint — they are only
  /// visible once at creation time.
  Future<List<Map<String, dynamic>>> listAPITokens() =>
      throw UnimplementedError();

  /// Mints a new Bearer API token for the authenticated user.
  ///
  /// [name] is a human-readable label.  [expiresInDays] is optional; omit or
  /// pass `null` for a non-expiring token.
  ///
  /// Returns the raw JSON map because the token plaintext is only present in
  /// the creation response and there is no dedicated model for API tokens.
  Future<Map<String, dynamic>> createAPIToken({
    required String name,
    int? expiresInDays,
  }) =>
      throw UnimplementedError();

  /// Revokes a Bearer API token by its numeric [tokenId].
  ///
  /// DELETE /api/v1/auth/tokens/{id} — returns 204 No Content.
  Future<void> revokeAPIToken(int tokenId) => throw UnimplementedError();

  // ---------------------------------------------------------------------------
  // Progress (batch)
  // ---------------------------------------------------------------------------

  /// Submits multiple progress updates in one request.
  ///
  /// Designed for offline clients that accumulate updates while disconnected
  /// and sync on reconnect.  Each entry must include [mediaId],
  /// [positionSeconds], and [observedAt] (ISO-8601 UTC string).
  Future<void> batchUpdateProgress(
    List<Map<String, dynamic>> updates,
  ) =>
      throw UnimplementedError();

  // Expose the underlying Dio for advanced callers (e.g. binary streaming).
  // This should not be used for ordinary JSON requests; prefer the typed
  // methods above.
  Dio get rawDio => _dio;
}
