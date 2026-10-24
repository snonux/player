import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/api_client_provider.dart';

final serverPluginsApiProvider = Provider<ServerPluginsApi>((ref) {
  final credentials = ref.watch(credentialMutationQueueProvider);
  return ServerPluginsApi(ref.watch(apiClientProvider).rawDio,
      currentEpoch: () => credentials.generation);
});

/// A small optional admin API, separate from the ordinary playback client.
class ServerPluginsApi {
  ServerPluginsApi(this.dio, {required this.currentEpoch});
  final Dio dio;
  final int Function() currentEpoch;

  Options _options(int epoch) {
    if (epoch != currentEpoch()) {
      throw StateError('The signed-in account changed.');
    }
    return Options(
        sendTimeout: const Duration(seconds: 10),
        receiveTimeout: const Duration(seconds: 10),
        headers: {'Cache-Control': 'no-store'},
        extra: {'credentialEpoch': epoch});
  }

  Future<List<ServerPlugin>> list(int epoch) async {
    final response = await dio.get<List<dynamic>>('/api/v1/admin/plugins',
        options: _options(epoch));
    return (response.data ?? [])
        .map((item) => ServerPlugin(Map<String, dynamic>.from(item as Map)))
        .toList();
  }

  Future<void> setEnabled(String id, bool enabled, int epoch) async {
    await dio.post<void>(
        '/api/v1/admin/plugins/${Uri.encodeComponent(id)}/${enabled ? 'enable' : 'disable'}',
        options: _options(epoch));
  }

  Future<int> check(String id, int epoch) async {
    final response = await dio.post<Map<String, dynamic>>(
        '/api/v1/admin/plugins/${Uri.encodeComponent(id)}/invoke',
        data: {'capability': 'probe'},
        options: _options(epoch));
    return (response.data!['result'] as num).toInt();
  }
}

class ServerPlugin {
  ServerPlugin(this.data);
  final Map<String, dynamic> data;
  String get id => data['id'] as String;
  String get name => (data['name'] as String?)?.isNotEmpty == true
      ? data['name'] as String
      : id;
  String get version => data['version'] as String? ?? '';
  String get description => data['description'] as String? ?? '';
  String get error => data['error'] as String? ?? '';
  String get status => data['status'] as String? ?? 'error';
  bool get enabled => data['enabled'] == true;
  bool get canCheck =>
      enabled && (data['capabilities'] as List?)?.contains('probe') == true;
  bool get hasProbe =>
      (data['capabilities'] as List?)?.contains('probe') == true;
}
