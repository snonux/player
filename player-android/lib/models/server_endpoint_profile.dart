import '../services/server_address.dart';

/// Addresses explicitly trusted by the user for one canonical server profile.
class ServerEndpointProfile {
  const ServerEndpointProfile(
      {required this.primary,
      required this.local,
      required this.serverId,
      this.preferLocal = true,
      this.wifiName = ''});
  final String primary;
  final String local;
  final String serverId;
  final bool preferLocal;
  final String wifiName;
  Map<String, dynamic> toJson() => {
        'primary': primary,
        'local': local,
        'serverId': serverId,
        'preferLocal': preferLocal,
        'wifiName': wifiName
      };
  factory ServerEndpointProfile.fromJson(Map<String, dynamic> json) {
    final primary = parseServerBaseUrl(json['primary'] as String).origin;
    final local = parseServerBaseUrl(json['local'] as String).origin;
    final id = json['serverId'] as String;
    if (primary == local || !RegExp(r'^[a-f0-9]{64}$').hasMatch(id)) {
      throw const FormatException('Invalid paired addresses');
    }
    return ServerEndpointProfile(
        primary: primary,
        local: local,
        serverId: id,
        preferLocal: json['preferLocal'] == true,
        wifiName: (json['wifiName'] as String? ?? '').trim());
  }
}
