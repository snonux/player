// Unit tests for all Dart models in player-android/lib/models/.
//
// Each test group covers one model and exercises the fromJson/toJson
// round-trip: a hand-crafted JSON map is decoded via fromJson, and then
// re-encoded via toJson; the resulting map is compared field-by-field to the
// original so that every serialisation key is covered exactly once.
//
// Nullable DateTime fields are verified by comparing the round-tripped
// ISO-8601 string to the original input string, because
// DateTime.parse(s).toIso8601String() is lossless for UTC timestamps.
// Each model is also tested with all fields absent to verify defaults.

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/models/media_set.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/models/tag.dart';
import 'package:player_android/models/note.dart';
import 'package:player_android/models/share.dart';
import 'package:player_android/models/podcast_feed.dart';
import 'package:player_android/models/podcast_episode.dart';
import 'package:player_android/models/playback_hint.dart';
import 'package:player_android/api/player_api_client.dart';

void main() {
  // ---------------------------------------------------------------------------
  // Media
  // ---------------------------------------------------------------------------
  group('Media', () {
    // A fully-populated JSON map used for the round-trip test.
    // deleted_at is null to cover the null-timestamp path; created_at is set to
    // cover the non-null path.  Use an integer for duration to verify that the
    // num-to-double cast in fromJson works for int JSON values.
    final Map<String, dynamic> fullJson = {
      'id': 1,
      'set_id': 2,
      'rel_path': 'videos/foo.mp4',
      'file_name': 'foo.mp4',
      'abs_path': '/media/videos/foo.mp4',
      'type': 'video',
      'duration': 120, // int in JSON; must be cast to double
      'codec': 'h264',
      'resolution': '1920x1080',
      'bitrate': 4000,
      'file_size_bytes': 512000,
      'width': 1920,
      'height': 1080,
      'thumbnail_path': '/thumbs/foo.jpg',
      'play_count': 3,
      'favorite': true,
      'tags': ['action', 'sci-fi'],
      'deleted_at': null,
      'created_at': '2024-01-15T10:00:00.000Z',
    };

    test('fromJson/toJson round-trip with timestamps', () {
      final media = Media.fromJson(fullJson);

      // Verify every field decoded correctly.
      expect(media.id, 1);
      expect(media.setId, 2);
      expect(media.relPath, 'videos/foo.mp4');
      expect(media.fileName, 'foo.mp4');
      expect(media.absPath, '/media/videos/foo.mp4');
      expect(media.type, 'video');
      expect(media.duration, 120.0); // int input becomes double
      expect(media.codec, 'h264');
      expect(media.resolution, '1920x1080');
      expect(media.bitrate, 4000);
      expect(media.fileSizeBytes, 512000);
      expect(media.width, 1920);
      expect(media.height, 1080);
      expect(media.thumbnailPath, '/thumbs/foo.jpg');
      expect(media.playCount, 3);
      expect(media.favorite, isTrue);
      expect(media.tags, ['action', 'sci-fi']);
      expect(media.deletedAt, isNull);
      // Verify the decoded DateTime matches the original ISO-8601 string.
      expect(media.createdAt?.toIso8601String(), fullJson['created_at']);

      // Re-encoding must preserve every key present in the original map.
      final out = media.toJson();
      expect(out['id'], fullJson['id']);
      expect(out['set_id'], fullJson['set_id']);
      expect(out['rel_path'], fullJson['rel_path']);
      expect(out['file_name'], fullJson['file_name']);
      expect(out['abs_path'], fullJson['abs_path']);
      expect(out['type'], fullJson['type']);
      expect(out['duration'], 120.0); // value comes back as double
      expect(out['codec'], fullJson['codec']);
      expect(out['resolution'], fullJson['resolution']);
      expect(out['bitrate'], fullJson['bitrate']);
      expect(out['file_size_bytes'], fullJson['file_size_bytes']);
      expect(out['width'], fullJson['width']);
      expect(out['height'], fullJson['height']);
      expect(out['thumbnail_path'], fullJson['thumbnail_path']);
      expect(out['play_count'], fullJson['play_count']);
      expect(out['favorite'], fullJson['favorite']);
      expect(out['tags'], fullJson['tags']);
      expect(out['deleted_at'], isNull);
      // The ISO-8601 string must round-trip exactly through DateTime.parse /
      // toIso8601String for UTC timestamps.
      expect(out['created_at'], fullJson['created_at']);
    });

    test('fromJson applies defaults for all missing fields', () {
      final media = Media.fromJson({});

      expect(media.id, 0);
      expect(media.setId, 0);
      expect(media.relPath, '');
      expect(media.fileName, '');
      expect(media.absPath, '');
      expect(media.type, '');
      expect(media.duration, 0.0);
      expect(media.codec, '');
      expect(media.resolution, '');
      expect(media.bitrate, 0);
      expect(media.fileSizeBytes, 0);
      expect(media.width, 0);
      expect(media.height, 0);
      expect(media.thumbnailPath, '');
      expect(media.playCount, 0);
      expect(media.tags, isEmpty);
      expect(media.favorite, isFalse);
      expect(media.deletedAt, isNull);
      expect(media.createdAt, isNull);
    });
  });

  // ---------------------------------------------------------------------------
  // MediaSet
  // ---------------------------------------------------------------------------
  group('MediaSet', () {
    final Map<String, dynamic> json = {
      'id': 10,
      'name': 'My Videos',
      'root_path': '/media/videos',
      'cover_thumbnail_path': '/thumbs/cover.jpg',
      'is_podcast': false,
      'created_at': '2024-03-01T08:00:00.000Z',
    };

    test('fromJson/toJson round-trip', () {
      final ms = MediaSet.fromJson(json);

      expect(ms.id, 10);
      expect(ms.name, 'My Videos');
      expect(ms.rootPath, '/media/videos');
      expect(ms.coverThumbnailPath, '/thumbs/cover.jpg');
      expect(ms.isPodcast, isFalse);
      expect(ms.createdAt?.toIso8601String(), json['created_at']);

      final out = ms.toJson();
      expect(out['id'], json['id']);
      expect(out['name'], json['name']);
      expect(out['root_path'], json['root_path']);
      expect(out['cover_thumbnail_path'], json['cover_thumbnail_path']);
      expect(out['is_podcast'], json['is_podcast']);
      expect(out['created_at'], json['created_at']);
    });

    test('fromJson applies defaults for all missing fields', () {
      final ms = MediaSet.fromJson({});

      expect(ms.id, 0);
      expect(ms.name, '');
      expect(ms.rootPath, '');
      expect(ms.coverThumbnailPath, '');
      expect(ms.isPodcast, isFalse);
      expect(ms.createdAt, isNull);
    });
  });

  // ---------------------------------------------------------------------------
  // User
  // ---------------------------------------------------------------------------
  group('User', () {
    final Map<String, dynamic> json = {
      'id': 42,
      'username': 'alice',
      'is_admin': true,
      'created_at': '2024-02-10T12:00:00.000Z',
    };

    test('fromJson/toJson round-trip', () {
      final user = User.fromJson(json);

      expect(user.id, 42);
      expect(user.username, 'alice');
      expect(user.isAdmin, isTrue);
      expect(user.createdAt?.toIso8601String(), json['created_at']);

      final out = user.toJson();
      expect(out['id'], json['id']);
      expect(out['username'], json['username']);
      expect(out['is_admin'], json['is_admin']);
      expect(out['created_at'], json['created_at']);
    });

    test('fromJson applies defaults for all missing fields', () {
      final user = User.fromJson({});

      expect(user.id, 0);
      expect(user.username, '');
      expect(user.isAdmin, isFalse);
      expect(user.createdAt, isNull);
    });
  });

  // ---------------------------------------------------------------------------
  // Tag
  // ---------------------------------------------------------------------------
  group('Tag', () {
    final Map<String, dynamic> json = {'id': 7, 'name': 'sci-fi'};

    test('fromJson/toJson round-trip', () {
      final tag = Tag.fromJson(json);

      expect(tag.id, 7);
      expect(tag.name, 'sci-fi');

      final out = tag.toJson();
      expect(out['id'], json['id']);
      expect(out['name'], json['name']);
    });

    test('fromJson applies defaults for all missing fields', () {
      final tag = Tag.fromJson({});

      expect(tag.id, 0);
      expect(tag.name, '');
    });
  });

  // ---------------------------------------------------------------------------
  // Note
  // ---------------------------------------------------------------------------
  group('Note', () {
    final Map<String, dynamic> json = {
      'id': 5,
      'media_id': 100,
      'user_id': 42,
      'content': 'Great scene at 00:45',
      'created_at': '2024-04-01T09:00:00.000Z',
      'updated_at': '2024-04-02T09:00:00.000Z',
    };

    test('fromJson/toJson round-trip', () {
      final note = Note.fromJson(json);

      expect(note.id, 5);
      expect(note.mediaId, 100);
      expect(note.userId, 42);
      expect(note.content, 'Great scene at 00:45');
      expect(note.createdAt?.toIso8601String(), json['created_at']);
      expect(note.updatedAt?.toIso8601String(), json['updated_at']);

      final out = note.toJson();
      expect(out['id'], json['id']);
      expect(out['media_id'], json['media_id']);
      expect(out['user_id'], json['user_id']);
      expect(out['content'], json['content']);
      expect(out['created_at'], json['created_at']);
      expect(out['updated_at'], json['updated_at']);
    });

    test('fromJson applies defaults for all missing fields', () {
      final note = Note.fromJson({});

      expect(note.id, 0);
      expect(note.mediaId, 0);
      expect(note.userId, 0);
      expect(note.content, '');
      expect(note.createdAt, isNull);
      expect(note.updatedAt, isNull);
    });
  });

  // ---------------------------------------------------------------------------
  // Share
  // ---------------------------------------------------------------------------
  group('Share', () {
    final Map<String, dynamic> json = {
      'token': 'abc123',
      'media_id': 200,
      'created_by': 1,
      'created_at': '2024-05-01T00:00:00.000Z',
      'expires_at': '2024-06-01T00:00:00.000Z',
      'max_uses': 5,
      'used_count': 2,
    };

    test('fromJson/toJson round-trip with optional fields present', () {
      final share = Share.fromJson(json);

      expect(share.token, 'abc123');
      expect(share.mediaId, 200);
      expect(share.createdBy, 1);
      expect(share.createdAt?.toIso8601String(), json['created_at']);
      expect(share.expiresAt?.toIso8601String(), json['expires_at']);
      expect(share.maxUses, 5);
      expect(share.usedCount, 2);

      final out = share.toJson();
      expect(out['token'], json['token']);
      expect(out['media_id'], json['media_id']);
      expect(out['created_by'], json['created_by']);
      expect(out['created_at'], json['created_at']);
      expect(out['expires_at'], json['expires_at']);
      expect(out['max_uses'], json['max_uses']);
      expect(out['used_count'], json['used_count']);
    });

    test('fromJson applies defaults for all missing fields; maxUses stays null', () {
      final share = Share.fromJson({});

      expect(share.token, '');
      expect(share.mediaId, 0);
      expect(share.createdBy, 0);
      expect(share.maxUses, isNull);
      expect(share.usedCount, 0);
      expect(share.createdAt, isNull);
      expect(share.expiresAt, isNull);
    });
  });

  // ---------------------------------------------------------------------------
  // PodcastFeed
  // ---------------------------------------------------------------------------
  group('PodcastFeed', () {
    final Map<String, dynamic> json = {
      'id': 3,
      'set_id': 10,
      'feed_url': 'https://example.com/feed.rss',
      'title': 'My Podcast',
      'description': 'A great show',
      'image_url': 'https://example.com/cover.jpg',
      'last_checked_at': '2024-05-10T06:00:00.000Z',
      'last_etag': '"etag-value"',
      'check_interval_minutes': 60,
      'auto_download': true,
      'consecutive_failures': 0,
      'next_check_at': '2024-05-10T07:00:00.000Z',
      'created_at': '2024-01-01T00:00:00.000Z',
    };

    test('fromJson/toJson round-trip', () {
      final feed = PodcastFeed.fromJson(json);

      expect(feed.id, 3);
      expect(feed.setId, 10);
      expect(feed.feedUrl, 'https://example.com/feed.rss');
      expect(feed.title, 'My Podcast');
      expect(feed.description, 'A great show');
      expect(feed.imageUrl, 'https://example.com/cover.jpg');
      expect(feed.lastCheckedAt?.toIso8601String(), json['last_checked_at']);
      expect(feed.lastETag, '"etag-value"');
      expect(feed.checkIntervalMinutes, 60);
      expect(feed.autoDownload, isTrue);
      expect(feed.consecutiveFailures, 0);
      expect(feed.nextCheckAt?.toIso8601String(), json['next_check_at']);
      expect(feed.createdAt?.toIso8601String(), json['created_at']);

      final out = feed.toJson();
      expect(out['id'], json['id']);
      expect(out['set_id'], json['set_id']);
      expect(out['feed_url'], json['feed_url']);
      expect(out['title'], json['title']);
      expect(out['description'], json['description']);
      expect(out['image_url'], json['image_url']);
      expect(out['last_checked_at'], json['last_checked_at']);
      expect(out['last_etag'], json['last_etag']);
      expect(out['check_interval_minutes'], json['check_interval_minutes']);
      expect(out['auto_download'], json['auto_download']);
      expect(out['consecutive_failures'], json['consecutive_failures']);
      expect(out['next_check_at'], json['next_check_at']);
      expect(out['created_at'], json['created_at']);
    });

    test('fromJson applies defaults for all missing fields', () {
      final feed = PodcastFeed.fromJson({});

      expect(feed.id, 0);
      expect(feed.setId, 0);
      expect(feed.feedUrl, '');
      expect(feed.title, '');
      expect(feed.description, '');
      expect(feed.imageUrl, '');
      expect(feed.lastETag, '');
      expect(feed.checkIntervalMinutes, 0);
      expect(feed.autoDownload, isFalse);
      expect(feed.consecutiveFailures, 0);
      expect(feed.lastCheckedAt, isNull);
      expect(feed.nextCheckAt, isNull);
      expect(feed.createdAt, isNull);
    });
  });

  // ---------------------------------------------------------------------------
  // PodcastEpisode
  // ---------------------------------------------------------------------------
  group('PodcastEpisode', () {
    // Use an integer for position_seconds to cover the num-to-double cast.
    final Map<String, dynamic> json = {
      'id': 99,
      'feed_id': 3,
      'media_id': 55,
      'guid': 'ep-guid-abc',
      'title': 'Episode One',
      'description': 'First episode',
      'published_at': '2024-04-01T00:00:00.000Z',
      'episode_url': 'https://example.com/ep1.mp3',
      'duration_seconds': 3600.0,
      'file_size': 102400,
      'file_name': 'ep1.mp3',
      'is_downloaded': true,
      'is_completed': false,
      'position_seconds': 120, // int in JSON; must be cast to double
      'created_at': '2024-04-01T01:00:00.000Z',
    };

    test('fromJson/toJson round-trip with optional fields present', () {
      final ep = PodcastEpisode.fromJson(json);

      expect(ep.id, 99);
      expect(ep.feedId, 3);
      expect(ep.mediaId, 55);
      expect(ep.guid, 'ep-guid-abc');
      expect(ep.title, 'Episode One');
      expect(ep.description, 'First episode');
      expect(ep.publishedAt?.toIso8601String(), json['published_at']);
      expect(ep.episodeUrl, 'https://example.com/ep1.mp3');
      expect(ep.durationSeconds, 3600.0);
      expect(ep.fileSize, 102400);
      expect(ep.fileName, 'ep1.mp3');
      expect(ep.isDownloaded, isTrue);
      expect(ep.isCompleted, isFalse);
      expect(ep.positionSeconds, 120.0); // int input becomes double
      expect(ep.createdAt?.toIso8601String(), json['created_at']);

      final out = ep.toJson();
      expect(out['id'], json['id']);
      expect(out['feed_id'], json['feed_id']);
      expect(out['media_id'], json['media_id']);
      expect(out['guid'], json['guid']);
      expect(out['title'], json['title']);
      expect(out['description'], json['description']);
      expect(out['published_at'], json['published_at']);
      expect(out['episode_url'], json['episode_url']);
      expect(out['duration_seconds'], json['duration_seconds']);
      expect(out['file_size'], json['file_size']);
      expect(out['file_name'], json['file_name']);
      expect(out['is_downloaded'], json['is_downloaded']);
      expect(out['is_completed'], json['is_completed']);
      expect(out['position_seconds'], 120.0); // value comes back as double
      expect(out['created_at'], json['created_at']);
    });

    test('fromJson applies defaults; nullable fields stay null', () {
      final ep = PodcastEpisode.fromJson({});

      expect(ep.id, 0);
      expect(ep.feedId, 0);
      expect(ep.mediaId, isNull);
      expect(ep.guid, '');
      expect(ep.title, '');
      expect(ep.description, '');
      expect(ep.episodeUrl, '');
      expect(ep.fileName, '');
      expect(ep.durationSeconds, isNull);
      expect(ep.fileSize, isNull);
      expect(ep.isDownloaded, isFalse);
      expect(ep.isCompleted, isFalse);
      expect(ep.positionSeconds, 0.0);
      expect(ep.publishedAt, isNull);
      expect(ep.createdAt, isNull);
    });
  });

  // ---------------------------------------------------------------------------
  // PlaybackHint
  // ---------------------------------------------------------------------------
  group('PlaybackHint', () {
    final Map<String, dynamic> json = {
      'media_id': 77,
      'position_seconds': 250.5,
      'finished': true,
      'updated_at': '2024-06-01T14:30:00.000Z',
    };

    test('fromJson/toJson round-trip', () {
      final hint = PlaybackHint.fromJson(json);

      expect(hint.mediaId, 77);
      expect(hint.positionSeconds, 250.5);
      expect(hint.finished, isTrue);
      expect(hint.updatedAt?.toIso8601String(), json['updated_at']);

      final out = hint.toJson();
      expect(out['media_id'], json['media_id']);
      expect(out['position_seconds'], json['position_seconds']);
      expect(out['finished'], json['finished']);
      expect(out['updated_at'], json['updated_at']);
    });

    test('fromJson applies defaults for all missing fields', () {
      final hint = PlaybackHint.fromJson({});

      expect(hint.mediaId, 0);
      expect(hint.positionSeconds, 0.0);
      expect(hint.finished, isFalse);
      expect(hint.updatedAt, isNull);
    });
  });

  // ---------------------------------------------------------------------------
  // PlayerApiClient constructor
  // ---------------------------------------------------------------------------
  group('PlayerApiClient constructor', () {
    test('stores baseUrl and bearerToken when valid', () {
      // The client stores the Uri and token as-is; no transformation occurs.
      final client = PlayerApiClient(
        baseUrl: Uri.parse('https://player.example.com'),
        bearerToken: 'my-secret-token',
      );

      expect(client.baseUrl.toString(), 'https://player.example.com');
      expect(client.bearerToken, 'my-secret-token');
    });

    test('baseUrl with path component is stored verbatim', () {
      // Verify the full URI including trailing path is preserved unchanged.
      final client = PlayerApiClient(
        baseUrl: Uri.parse('https://player.example.com/api/v1'),
        bearerToken: 'tok',
      );

      expect(client.baseUrl.path, '/api/v1');
      expect(client.baseUrl.host, 'player.example.com');
    });

    test('stores an empty bearerToken unchanged', () {
      // A missing or empty token is allowed at construction time; the server
      // will reject unauthenticated requests at call time.
      final client = PlayerApiClient(
        baseUrl: Uri.parse('https://player.example.com'),
        bearerToken: '',
      );

      expect(client.bearerToken, '');
    });
  });
}
