import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/media_filter.dart';

void main() {
  test('normalizes and owns collections, including explicit all-set scope', () {
    final ids = [2, 1, 2];
    final tags = [' calm ', 'keep', 'calm', ''];
    final filter =
        MediaFilter(setIds: ids, tags: tags, minDuration: 0, fileSizeMax: 0);
    ids.clear();
    tags.clear();
    expect(filter.setIds, [1, 2]);
    expect(filter.tags, ['calm', 'keep']);
    expect(() => filter.tags.add('other'), throwsUnsupportedError);
    expect(
        filter,
        MediaFilter(
            setIds: [1, 2],
            tags: ['keep', 'calm'],
            minDuration: 0,
            fileSizeMax: 0));
    expect(filter.hashCode, filter.copyWith().hashCode);
    expect(filter.copyWith(setIds: null).setIds, isNull);
    expect(filter.copyWith(setIds: []).setIds, isEmpty);
    expect(MediaFilter(setIds: []), isNot(const MediaFilter.empty()));
    expect(filter.copyWith(minDuration: null, fileSizeMax: null).minDuration,
        isNull);
    expect(filter.hasConstraints, isTrue);
    expect(const MediaFilter.empty().hasConstraints, isFalse);
  });
}
