import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/screens/standalone_podcasts_screen.dart';
import 'package:player_android/widgets/podcast_artwork.dart';
import 'podcast_browsing_fakes.dart';

void main() {
  testWidgets(
      'episode publication date and safe show notes are readable on a narrow enlarged screen',
      (tester) async {
    tester.view.physicalSize = const Size(360, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final repository = BrowsingRepository()
      ..metadata = {
        'published_at': 1793023200000,
        'description':
            '<p>First &amp; second paragraph.</p><p><a href="https://example.com">Publisher notes</a></p><script>bad()</script>'
      };
    await tester.pumpWidget(browsingApp(
        Builder(
            builder: (context) => MediaQuery(
                data: MediaQuery.of(context)
                    .copyWith(textScaler: const TextScaler.linear(1.5)),
                child: const LocalPodcastEpisodesScreen())),
        repository));
    await tester.pumpAndSettle();
    expect(find.text('Published Oct 26, 2026'), findsOneWidget);
    expect(find.text('Show notes'), findsOneWidget);
    expect(find.byType(SelectableText), findsNothing);
    await tester.tap(find.text('Show notes'));
    await tester.pumpAndSettle();
    final text =
        tester.widget<SelectableText>(find.byType(SelectableText)).data!;
    expect(text, contains('First & second paragraph.'));
    expect(text, contains('Publisher notes (https://example.com)'));
    expect(text, isNot(contains('bad()')));
    expect(text, isNot(contains('<p>')));
    expect(tester.takeException(), isNull);
  });
  testWidgets('missing notes stay absent and missing publication is explicit',
      (tester) async {
    final repository = BrowsingRepository()..metadata = {'description': ''};
    await tester.pumpWidget(
        browsingApp(const LocalPodcastEpisodesScreen(), repository));
    await tester.pumpAndSettle();
    expect(find.text('Publication date unavailable'), findsOneWidget);
    expect(find.text('Show notes'), findsNothing);
  });
  testWidgets(
      'item image has no auth headers and failures fall back to feed cover',
      (tester) async {
    await tester.pumpWidget(const MaterialApp(
        home: Scaffold(
            body: PodcastArtwork(
                imageUrl: 'https://publisher.example/item.jpg',
                fallbackUrl: 'https://publisher.example/feed.jpg',
                title: 'Episode'))));
    final image =
        tester.widget<CachedNetworkImage>(find.byType(CachedNetworkImage));
    expect(image.imageUrl, 'https://publisher.example/item.jpg');
    expect(image.httpHeaders, isNull);
    final context = tester.element(find.byType(CachedNetworkImage));
    final fallback =
        image.errorWidget!(context, image.imageUrl, Exception('missing'));
    expect(fallback, isA<CachedNetworkImage>());
    expect((fallback as CachedNetworkImage).imageUrl,
        'https://publisher.example/feed.jpg');
    expect(fallback.httpHeaders, isNull);
  });
  testWidgets(
      'blank or failed publisher covers retain supplied embedded/private fallback',
      (tester) async {
    const embedded = Icon(Icons.album, key: Key('embedded-cover'));
    await tester.pumpWidget(const MaterialApp(
        home: Scaffold(
            body: PodcastArtwork(
                imageUrl: '',
                fallbackUrl: '',
                title: 'Episode',
                unavailable: embedded))));
    expect(find.byKey(const Key('embedded-cover')), findsOneWidget);
    await tester.pumpWidget(const MaterialApp(
        home: Scaffold(
            body: PodcastArtwork(
                imageUrl: 'https://publisher.example/item.jpg',
                fallbackUrl: 'https://publisher.example/feed.jpg',
                title: 'Episode',
                unavailable: embedded))));
    final image =
        tester.widget<CachedNetworkImage>(find.byType(CachedNetworkImage));
    final context = tester.element(find.byType(CachedNetworkImage));
    final feed =
        image.errorWidget!(context, image.imageUrl, Exception('missing'))
            as CachedNetworkImage;
    final last =
        feed.errorWidget!(context, feed.imageUrl, Exception('missing'));
    expect(last, same(embedded));
  });
}
