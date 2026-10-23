import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/download_quality.dart';
import 'package:player_android/services/stream_quality.dart';

void main() {
  test('download choice is independent and scoped by server', () async {
    SharedPreferences.setMockInitialValues({});
    await StreamQuality.initialize();
    await StreamQuality.save('https://one.example', 'aac128');
    expect(await DownloadQuality.selected('https://one.example'), 'original');
    await DownloadQuality.save('https://one.example', 'opus96');
    expect(StreamQuality.selected('https://one.example'), 'aac128');
    expect(await DownloadQuality.selected('https://two.example'), 'original');
    expect(await DownloadQuality.selected('https://one.example'), 'opus96');
  });

  test('saved filename follows the encoded container and unknown profiles fail',
      () {
    expect(DownloadQuality.sourceName('album.flac/track.flac', 'opus96'),
        'album.flac/track.ogg');
    expect(DownloadQuality.sourceName('track.flac', 'aac256'), 'track.m4a');
    expect(DownloadQuality.sourceName('track.flac', 'mp3192'), 'track.mp3');
    expect(DownloadQuality.sourceName('track.flac', 'original'), 'track.flac');
    expect(() => DownloadQuality.sourceName('track.flac', 'removed'),
        throwsStateError);
  });
}
