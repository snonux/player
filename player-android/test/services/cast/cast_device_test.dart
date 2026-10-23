import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/cast/cast_device.dart';
import 'package:player_android/widgets/cast_controls.dart';

class _Discovery implements CastDiscovery {
  _Discovery(this.stream);

  final Stream<List<CastDevice>> stream;

  @override
  Stream<List<CastDevice>> devices() => stream;
}

void main() {
  test('platform events add and remove devices', () {
    final devices = <String, CastDevice>{};
    applyCastDiscoveryEvent(devices, {
      'event': 'found',
      'service': 'Chromecast-abc',
      'id': 'abc',
      'name': 'Kitchen speaker',
      'model': 'Google Home',
      'host': '192.0.2.4',
      'port': 8009,
    });
    applyCastDiscoveryEvent(devices, {
      'event': 'found',
      'service': 'Chromecast-def',
      'name': '',
      'host': '192.0.2.5',
      'port': 8009,
    });
    expect(devices['Chromecast-abc']!.name, 'Kitchen speaker');
    expect(devices['Chromecast-abc']!.model, 'Google Home');
    // Without a friendly name the service name is shown.
    expect(devices['Chromecast-def']!.name, 'Chromecast-def');
    expect(devices['Chromecast-def']!.id, 'Chromecast-def');

    applyCastDiscoveryEvent(devices, {'event': 'found', 'service': 'x'});
    applyCastDiscoveryEvent(devices, 'garbage');
    expect(devices, hasLength(2));

    applyCastDiscoveryEvent(
        devices, {'event': 'lost', 'service': 'Chromecast-abc'});
    expect(devices.keys, ['Chromecast-def']);
  });

  CastDevice? picked;
  Future<void> open(
      WidgetTester tester, Stream<List<CastDevice>> stream) async {
    await tester.pumpWidget(MaterialApp(
      home: Builder(
        builder: (context) => TextButton(
          onPressed: () async =>
              picked = await showCastDevicePicker(context, _Discovery(stream)),
          child: const Text('open'),
        ),
      ),
    ));
    await tester.tap(find.text('open'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
  }

  testWidgets('the picker explains an empty search', (tester) async {
    await open(tester, const Stream.empty());
    expect(find.text('Looking for devices…'), findsOneWidget);
    expect(find.byKey(const Key('cast_picker_note')), findsNothing);
    await tester.pump(kCastSearchHintDelay);
    expect(find.byKey(const Key('cast_picker_note')), findsOneWidget);
  });

  testWidgets('the picker returns the chosen device', (tester) async {
    const tv = CastDevice(id: 'tv', name: 'Den TV', host: 'h', port: 8009);
    await open(tester, Stream.value([tv]));
    await tester.tap(find.text('Den TV'));
    await tester.pump(const Duration(milliseconds: 500));
    expect(picked, tv);
  });
}
