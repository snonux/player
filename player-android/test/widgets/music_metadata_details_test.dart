import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/music_metadata.dart';
import 'package:player_android/widgets/music_metadata_details.dart';

void main() {
  testWidgets(
      'release groups preserve labels, multiline values and empty snapshots',
      (tester) async {
    final music = MusicMetadata.fromJson({
      'date': '2024',
      'year': 2024,
      'release_date': '2025-04',
      'original_date': '1979',
      'recording_date': '1978-11',
      'labels': ['A; B', 'C / D'],
      'comment': '<b>Text</b>\n第二行',
      'credits': [
        {
          'role': 'tape op',
          'names': ['A/B', 'C, D']
        }
      ]
    });
    await tester.pumpWidget(MaterialApp(
        home: Scaffold(
            body: SingleChildScrollView(
                child: MusicMetadataDetails(music: music)))));
    expect(find.text('Release details'), findsOneWidget);
    expect(find.text('Release date: 2025-04'), findsOneWidget);
    expect(find.text('Original date: 1979'), findsOneWidget);
    expect(find.text('Recording date: 1978-11'), findsOneWidget);
    expect(find.text('Label: A; B\nC / D'), findsOneWidget);
    expect(find.text('Comment: <b>Text</b>\n第二行'), findsOneWidget);
    expect(find.text('tape op: A/B\nC, D'), findsOneWidget);
    expect(music.details['Tagged date'], '2024');
    expect(music.details['Year'], '2024');
    final cached = MusicMetadata.fromJson(music.toJson());
    expect(cached.toJson(), music.toJson());
    expect(cached.detailGroups['Release details']!.last.value, 'A; B\nC / D');
    await tester.pumpWidget(const MaterialApp(
        home: Scaffold(body: MusicMetadataDetails(music: null))));
    expect(find.text('No embedded music metadata saved.'), findsOneWidget);
    expect(find.text('Release details'), findsNothing);
  });

  test('date label omissions retain imprecise tag and independent year', () {
    final tagged = MusicMetadata.fromJson({'date': '2024-02', 'year': 2024});
    expect(tagged.details.containsKey('Tagged date'), false);
    expect(tagged.details.containsKey('Year'), false);
    final human =
        MusicMetadata.fromJson({'date': 'Spring release', 'year': 2024});
    expect(human.releaseDate, 'Spring release');
    expect(human.details['Year'], '2024');
  });
}
