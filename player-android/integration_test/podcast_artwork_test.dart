import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/screens/standalone_podcasts_screen.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/widgets/podcast_artwork.dart';
import 'package:sqflite/sqflite.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
      'overview hydrates public cover, aliases reuse it and errors fall back',
      (tester) async {
    final publisher = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final origin = 'http://127.0.0.1:${publisher.port}';
    final requests = <HttpRequest>[];
    final png = base64Decode(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg==');
    publisher.listen((request) async {
      requests.add(request);
      if (request.uri.path == '/feed') {
        request.response.headers.contentType =
            ContentType('application', 'rss+xml');
        request.response.write('<rss><channel><title>Publisher podcast</title>'
            '<image><url>$origin/cover.png</url></image>'
            '<item><guid>one</guid><title>Episode</title>'
            '<enclosure url="$origin/audio.mp3"/></item></channel></rss>');
      } else if (request.uri.path == '/cover.png') {
        request.response.headers.contentType = ContentType('image', 'png');
        request.response.add(png);
      } else {
        request.response.statusCode = 404;
      }
      await request.response.close();
    });
    final repository = PodcastRepository(databasePath: inMemoryDatabasePath);
    try {
      final db = await repository.database;
      const accountKey = 'feed:account/canonical';
      await db.insert('podcast_subscriptions', {
        'scope': 'local',
        'feed_key': accountKey,
        'title': 'Publisher podcast',
        'fetch_url': '$origin/feed',
        'subscribed': 1,
        'subscription_ms': 123,
      });
      await tester.pumpWidget(ProviderScope(overrides: [
        podcastRepositoryProvider.overrideWithValue(repository),
        podcastScopeProvider.overrideWith((ref) async => 'local'),
        podcastServerSubscriptionsProvider.overrideWith((ref) async => null),
        podcastServerMetadataProvider.overrideWithValue(null),
      ], child: const MaterialApp(home: StandalonePodcastsScreen())));
      Future<void> expectBitmap() async {
        for (var i = 0; i < 100; i++) {
          await tester.pump(const Duration(milliseconds: 100));
          final images = tester.widgetList<RawImage>(find.byType(RawImage));
          if (images.any((image) => image.image?.width == 1)) return;
          await Future<void>.delayed(const Duration(milliseconds: 50));
        }
        fail('Publisher cover bitmap did not decode');
      }

      await expectBitmap();
      expect(requests.any((request) => request.uri.path == '/feed'), isTrue);
      expect(
          requests.any((request) => request.uri.path == '/cover.png'), isTrue);
      expect(
          (await repository.subscriptions('local')).single['subscription_ms'],
          123);
      // Canonical account identity changes without losing the downloaded image.
      await db
          .update('podcast_subscriptions', {'feed_key': 'feed:new/canonical'});
      await db.insert('podcast_account_aliases', {
        'scope': 'local',
        'kind': 'feed',
        'source': accountKey,
        'target': 'feed:new/canonical'
      });
      expect((await repository.subscriptions('local')).single['image_url'],
          '$origin/cover.png');
      // An unavailable episode-specific image falls back to the publisher cover.
      await tester.pumpWidget(MaterialApp(
          home: Scaffold(
              body: PodcastArtwork(
        imageUrl: '$origin/missing.png',
        fallbackUrl: '$origin/cover.png',
        title: 'Episode',
      ))));
      await expectBitmap();
      expect(requests.any((request) => request.uri.path == '/missing.png'),
          isTrue);
      for (final request in requests) {
        expect(request.headers.value(HttpHeaders.authorizationHeader), isNull);
        expect(request.headers.value(HttpHeaders.cookieHeader), isNull);
      }
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    } finally {
      await repository.close();
      await publisher.close(force: true);
    }
  });
}
