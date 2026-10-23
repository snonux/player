import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/music_home.dart';
import 'package:player_android/services/music_discovery.dart';

void main() {
  test('saved order and pins recover safely without persisting private names',
      () {
    final home = MusicHome({
      'order': ['pinned', 'gone', 'pinned'],
      'hidden': ['gone', 'favorites'],
      'pins': ['pl_1', 'pl_1', 4, ''],
      'name': 'Private title'
    });
    expect(home.order.first, 'pinned');
    expect(home.order.length, albumCollections.length + 1);
    expect(home.hidden, {'favorites'});
    expect(home.pins, ['pl_1']);
    expect(home.toJson().containsKey('name'), isFalse);
    expect(MusicHome(home.toJson()).toJson(), home.toJson());
    expect(MusicHome().hidden, isEmpty);
    expect(MusicHome({'order': 'broken', 'hidden': {}, 'pins': {}}).toJson(),
        MusicHome().toJson());
  });
}
