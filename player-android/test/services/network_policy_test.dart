import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/network_policy.dart';
import 'package:player_android/services/stream_quality.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    SharedPreferences.setMockInitialValues(
        {'stream-quality:https://music.test': 'original'});
    await StreamQuality.initialize();
  });
  test('Unknown uses Mobile and restrictions cannot become cache permission',
      () async {
    await MusicNetworkPolicy.save('mode', 'auto');
    MusicNetworkPolicy.detected = 'unknown';
    await MusicNetworkPolicy.save('stream-wifi', true);
    await MusicNetworkPolicy.save('sync-wifi', true);
    expect(MusicNetworkPolicy.prefetchAllowed, false);
    expect(MusicNetworkPolicy.syncAllowed, false);
    expect(MusicNetworkPolicy.assertStreamingAllowed,
        throwsA(isA<MusicNetworkRestricted>()));
    await MusicNetworkPolicy.save('mode', 'wifi');
    expect(MusicNetworkPolicy.prefetchAllowed, true);
    expect(MusicNetworkPolicy.syncAllowed, true);
    expect(MusicNetworkPolicy.assertStreamingAllowed, returnsNormally);
  });
  test(
      'Network profiles inherit legacy quality and preserve captured selection',
      () async {
    const server = 'https://music.test';
    expect(StreamQuality.forNetwork(server, 'mobile'), 'original');
    await StreamQuality.save(server, 'opus96', network: 'mobile');
    await MusicNetworkPolicy.save('mode', 'mobile');
    final captured = StreamQuality.selected(server);
    await MusicNetworkPolicy.save('mode', 'wifi');
    expect(captured, 'opus96');
    expect(StreamQuality.selected(server), 'original');
    expect(StreamQuality.forNetwork('https://another.test', 'mobile'), 'auto');
  });
}
