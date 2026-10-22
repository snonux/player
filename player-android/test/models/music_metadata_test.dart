import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/audio_visuals.dart';

void main() {
  test('tagged audiobook preserves source hint through compatibility switch',
      () {
    final request = ServerPlaybackRequest(
        mediaId: 1,
        serverOrigin: 'http://fixture',
        userId: 1,
        sourceUri: Uri.parse('http://fixture/api/media/1/stream'),
        title: 'Neutral Title',
        sourceName: 'Chapter.m4b');
    expect(isAudiobook(request.visualContext), isTrue);
    final compatibility =
        request.withSourceUri(Uri.parse('http://fixture/api/media/1/compat'));
    expect(compatibility.title, 'Neutral Title');
    expect(isAudiobook(compatibility.visualContext), isTrue);
  });
  test('music title, Unicode and punctuation survive cached JSON round trip',
      () {
    final media = Media.fromJson({
      'id': 1,
      'type': 'audio',
      'file_name': '01-name.flac',
      'music': {
        'title': '東京 Nights',
        'artist': 'A feat. B; C',
        'album': 'Café',
        'disc': 2,
        'disc_total': 3,
        'disc_subtitle': 'Encore 東京',
        'compilation': true,
        'genres': ['Jazz', 'Blues'],
        'composers': ['Composer; 東京 / A', 'Other'],
        'performers': ['Session Ensemble']
      }
    });
    final cached = Media.fromJson(media.toJson());
    expect(cached.displayTitle, '東京 Nights');
    expect(cached.fileName, '01-name.flac');
    expect(cached.musicSubtitle, 'A feat. B; C · Café');
    expect(cached.music!.details['Disc'], '2 / 3');
    expect(cached.music!.details['Disc subtitle'], 'Encore 東京');
    expect(cached.music!.details['Compilation'], 'Yes');
    expect(cached.music!.details['Genres'], 'Jazz, Blues');
    expect(cached.music!.details['Composer'], 'Composer; 東京 / A, Other');
    expect(cached.music!.details['Performer'], 'Session Ensemble');
  });
  test('old snapshots and blank titles fall back to unchanged filename stem',
      () {
    for (final music in [
      null,
      {'title': ' \t '}
    ]) {
      final media = Media.fromJson(
          {'type': 'audio', 'file_name': '01-name.mp3', 'music': music});
      expect(media.displayTitle, '01-name');
      expect(media.fileName, '01-name.mp3');
    }
    expect(
        Media.fromJson({
          'type': 'image',
          'file_name': 'a.png',
          'music': {'title': 'Wrong'}
        }).displayTitle,
        'a.png');
  });
}
