import 'dart:typed_data';

import '../models/models.dart';

class PlayerApiClient {
  final Uri baseUrl;
  final String bearerToken;

  // Normal (non-const) constructor: Uri is not const-constructable, so the
  // constructor must not be declared const.
  PlayerApiClient({required this.baseUrl, required this.bearerToken});

  Future<User> bootstrap({required String username, required String password}) => throw UnimplementedError();
  Future<User> login({required String username, required String password}) => throw UnimplementedError();
  Future<void> logout() => throw UnimplementedError();
  Future<void> healthz() => throw UnimplementedError();
  Future<void> readyz() => throw UnimplementedError();
  Future<String> getSharedMediaPage(String token) => throw UnimplementedError();
  Future<Uint8List> streamSharedMedia(String token, {String? range}) => throw UnimplementedError();
  Future<Uint8List> getSharedThumbnail(String token) => throw UnimplementedError();
  Future<Uint8List> downloadSharedMedia(String token) => throw UnimplementedError();
  Future<Map<String, dynamic>> getConfig() => throw UnimplementedError();
  Future<List<MediaSet>> listSets() => throw UnimplementedError();
  Future<Map<String, dynamic>> browseSet(int setId, {String? parent}) => throw UnimplementedError();
  Future<Uint8List> getSetCover(int setId, {String? folder}) => throw UnimplementedError();
  Future<void> updateSetCover(int setId, {String? folder}) => throw UnimplementedError();
  Future<Media> uploadToSet(int setId, {required String fileName, required List<int> bytes}) => throw UnimplementedError();
  Future<List<Media>> listMedia({String? search, int? setId, List<int>? setIds, String? type, bool? favorites, List<String>? tags, double? minDuration, double? maxDuration, int? fileSizeMin, int? fileSizeMax, String? sort, int? limit, int? offset, String? folder, String? parent}) => throw UnimplementedError();
  Future<Media> getMedia(int mediaId) => throw UnimplementedError();
  Future<Uint8List> streamMedia(int mediaId, {String? range}) => throw UnimplementedError();
  Future<Uint8List> downloadMedia(int mediaId) => throw UnimplementedError();
  Future<Uint8List> getThumbnail(int mediaId) => throw UnimplementedError();
  Future<void> regenerateThumbnail(int mediaId) => throw UnimplementedError();
  Future<bool> toggleFavorite(int mediaId) => throw UnimplementedError();
  Future<List<Tag>> listTags() => throw UnimplementedError();
  Future<void> addTag(int mediaId, String tag) => throw UnimplementedError();
  Future<void> removeTag(int mediaId, String tag) => throw UnimplementedError();
  Future<Share> createShare(int mediaId, {DateTime? expiresAt, int? maxUses}) => throw UnimplementedError();
  Future<List<Share>> listSharesForMedia(int mediaId) => throw UnimplementedError();
  Future<List<Share>> listMyShares() => throw UnimplementedError();
  Future<void> revokeShare(String token) => throw UnimplementedError();
  Future<Note?> getNote(int mediaId) => throw UnimplementedError();
  Future<Note> upsertNote(int mediaId, String content) => throw UnimplementedError();
  Future<void> deleteNote(int mediaId) => throw UnimplementedError();
  Future<void> updateProgress({required int mediaId, required double positionSeconds}) => throw UnimplementedError();
  Future<void> updateProgressStatus({required int mediaId, required String status}) => throw UnimplementedError();
  Future<List<Media>> listInProgress() => throw UnimplementedError();
  Future<void> deleteMedia(int mediaId) => throw UnimplementedError();
  Future<void> restoreMedia(int mediaId) => throw UnimplementedError();
  Future<List<PodcastFeed>> listPodcasts() => throw UnimplementedError();
  Future<List<PodcastEpisode>> listEpisodes(int podcastSetId, {int? limit, int? offset}) => throw UnimplementedError();
  Future<Media> downloadEpisode(int episodeId) => throw UnimplementedError();
  Future<void> toggleEpisodeComplete(int episodeId) => throw UnimplementedError();
  Future<PodcastFeed> subscribePodcast({required String feedUrl, String? setName}) => throw UnimplementedError();
  Future<List<User>> listUsers() => throw UnimplementedError();
  Future<User> createUser({required String username, required String password, required bool isAdmin}) => throw UnimplementedError();
  Future<void> deleteUser(int userId) => throw UnimplementedError();
  Future<List<Map<String, dynamic>>> listPermissions() => throw UnimplementedError();
  Future<void> grantPermission({required int setId, required int userId, required String role}) => throw UnimplementedError();
  Future<void> revokePermission({required int setId, required int userId}) => throw UnimplementedError();
  Future<void> triggerRescan() => throw UnimplementedError();
  Future<Map<String, dynamic>> getScanProgress() => throw UnimplementedError();
  Future<List<Media>> listTrash() => throw UnimplementedError();
}
