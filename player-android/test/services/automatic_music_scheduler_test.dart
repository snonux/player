import 'dart:async';
import 'dart:convert';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/automatic_music_provider.dart';
import 'package:player_android/services/automatic_music.dart';
import 'package:player_android/services/podcast_sync.dart';
import 'package:player_android/services/offline_download_repository.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const codec = StandardMethodCodec();
  Future<void> invoke(String method, String token) async {
    final done = Completer<void>();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .handlePlatformMessage('zone.foo.player_android/automatic_music',
            codec.encodeMethodCall(MethodCall(method, token)),
            (ByteData? reply) {
      try {
        codec.decodeEnvelope(reply!);
        done.complete();
      } catch (e) {
        done.completeError(e);
      }
    });
    await done.future;
  }

  test(
      'OS stop before automatic domain starts prevents late reads and preserves independent manual invocation',
      () async {
    final schedules = <MethodCall>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(AutomaticMusicScheduler.channel,
            (call) async {
      schedules.add(call);
      return null;
    });
    final service =
        AutomaticMusic(AutomaticMusicLibrary(OfflineDownloadRepository()),
            load: (k) async => jsonEncode({
                  'policy': {'enabled': true, 'recent': true}
                }),
            persist: (k, v) async {},
            network: () => 'wifi');
    final account = AutomaticMusicAccount(
        origin: 'http://p',
        userId: 1,
        current: () => true,
        read: (path) async {
          fail('Stopped wake must not read');
        },
        save: (s, c, a) async {});
    await service.bind(account.key);
    final scheduler = AutomaticMusicScheduler(service);
    addTearDown(() {
      scheduler.dispose();
      service.dispose();
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(AutomaticMusicScheduler.channel, null);
    });
    final entered = Completer<void>(), gate = Completer<void>();
    String? canceledKind;
    scheduler.onWake = () async {
      entered.complete();
      await gate.future;
      try {
        await scheduler.sync(account);
      } catch (error) {
        canceledKind = syncErrorKind(error);
      }
    };
    final pending = invoke('wake', 'job1');
    await entered.future;
    await invoke('stop', 'job1');
    expect(scheduler.wakeStopped, true);
    gate.complete();
    await pending;
    expect(canceledKind, "canceled");
    expect(service.active, false);
    expect(service.status['lastAttempt'], null);
    expect(scheduler.invocation, null);
    expect(scheduler.wakeStopped, false);
    expect((schedules.first.arguments as Map)['hours'], 6);
    await invoke('stop', 'old-job');
    expect(scheduler.wakeStopped, false);
  });
  test(
      'OS scheduling failure is visible without an unbounded retry notification loop',
      () async {
    var calls = 0;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(AutomaticMusicScheduler.channel,
            (call) async {
      calls++;
      throw PlatformException(code: 'unavailable');
    });
    final service = AutomaticMusic(
        AutomaticMusicLibrary(OfflineDownloadRepository()),
        load: (k) async => null,
        persist: (k, v) async {},
        network: () => 'wifi');
    await service.bind('test');
    final scheduler = AutomaticMusicScheduler(service);
    addTearDown(() {
      scheduler.dispose();
      service.dispose();
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(AutomaticMusicScheduler.channel, null);
    });
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);
    expect(calls, 1);
    expect(scheduler.error, contains('unavailable'));
  });
}
