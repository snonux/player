import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/music_presence.dart';

void main() {
  test('viewing is separate from sharing; errors clear private rows', () async {
    final calls = <String>[];
    bool offline = false;
    Map<String, dynamic>? snapshot = {
      'media_id': 1,
      'state': 'playing',
      'position_seconds': 4
    };
    final controller = MusicPresence(
        clientId: 'client-1234567890',
        snapshot: () => snapshot,
        request: (method, user, epoch, body) async {
          calls.add(method);
          if (offline) throw StateError('offline');
          return method == 'GET'
              ? [
                  {'title': 'Private'}
                ]
              : null;
        });
    controller.setAccount('https://one', 1, 2);
    await controller.refresh();
    expect(calls, isEmpty);
    controller.setOpen(true);
    await Future<void>.delayed(Duration.zero);
    expect(controller.rows.single['title'], 'Private');
    expect(calls, ['GET']);
    controller.setSharing(true);
    await Future<void>.delayed(Duration.zero);
    expect(calls, contains('PUT'));
    snapshot = null;
    await controller.refresh();
    expect(calls, contains('DELETE'));
    offline = true;
    await controller.refresh();
    expect(controller.rows, isEmpty);
    expect(controller.message, contains('unavailable'));
    controller.dispose();
  });

  test('account changes disable sharing and reject delayed private response',
      () async {
    final response = Completer<List<Map<String, dynamic>>?>();
    final calls = <Map<String, dynamic>>[];
    final controller = MusicPresence(
        clientId: 'client-1234567890',
        snapshot: () =>
            {'media_id': 1, 'state': 'playing', 'position_seconds': 1},
        request: (method, user, epoch, body) async {
          calls.add(
              {'method': method, 'user': user, 'epoch': epoch, 'body': body});
          return method == 'GET' ? response.future : null;
        });
    controller.setAccount('https://one', 1, 2);
    controller.setOpen(true);
    await Future<void>.delayed(Duration.zero);
    controller.setAccount('https://two', 2, 3);
    expect(controller.sharing, isFalse);
    expect(controller.rows, isEmpty);
    response.complete([
      {'title': 'Old account'}
    ]);
    await Future<void>.delayed(Duration.zero);
    expect(controller.rows, isEmpty);
    final removal = calls.lastWhere((call) => call['method'] == 'DELETE');
    expect(removal['user'], 1);
    expect(removal['epoch'], 2);
    controller.dispose();
  });
}
