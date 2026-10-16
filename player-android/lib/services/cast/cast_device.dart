import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// A Google Cast receiver found on the local network.
@immutable
class CastDevice {
  const CastDevice({
    required this.id,
    required this.name,
    required this.host,
    required this.port,
    this.model,
  });

  final String id;

  /// The friendly name the owner gave it, e.g. "Living Room TV".
  final String name;
  final String host;
  final int port;
  final String? model;

  @override
  bool operator ==(Object other) =>
      other is CastDevice &&
      other.id == id &&
      other.host == host &&
      other.port == port &&
      other.name == name;

  @override
  int get hashCode => Object.hash(id, host, port, name);
}

/// Lists cast devices while someone listens.
abstract interface class CastDiscovery {
  /// The devices seen so far, re-emitted on every change. Discovery runs
  /// while the stream has a listener.
  Stream<List<CastDevice>> devices();
}

/// Discovery through the Android side (NsdManager, see CastDiscovery.kt).
class PlatformCastDiscovery implements CastDiscovery {
  PlatformCastDiscovery({EventChannel? channel})
      : _channel = channel ??
            const EventChannel('zone.foo.player_android/cast_discovery');

  final EventChannel _channel;

  @override
  Stream<List<CastDevice>> devices() {
    // Keyed by mDNS service name, which "lost" events report.
    final byService = <String, CastDevice>{};
    return _channel.receiveBroadcastStream().map((event) {
      applyCastDiscoveryEvent(byService, event);
      return List<CastDevice>.unmodifiable(
        byService.values.toList()..sort((a, b) => a.name.compareTo(b.name)),
      );
    });
  }
}

/// Applies one platform event to [byService]. Malformed events are ignored.
@visibleForTesting
void applyCastDiscoveryEvent(Map<String, CastDevice> byService, Object? event) {
  if (event is! Map) return;
  final service = event['service'];
  if (service is! String) return;
  switch (event['event']) {
    case 'found':
      final host = event['host'];
      final port = event['port'];
      if (host is! String || port is! int) return;
      final name = event['name'];
      final id = event['id'];
      final model = event['model'];
      byService[service] = CastDevice(
        id: id is String ? id : service,
        name: name is String && name.isNotEmpty ? name : service,
        host: host,
        port: port,
        model: model is String ? model : null,
      );
    case 'lost':
      byService.remove(service);
  }
}
