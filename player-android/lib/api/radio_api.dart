import 'dart:typed_data';
import 'package:dio/dio.dart';

/// Radio requests keep the account/client captured with the station list/dialog.
/// The shared auth interceptor also checks [credentialEpoch] after credential I/O.
class RadioApi {
  RadioApi(this.dio, {required this.credentialEpoch, required this.isCurrent});
  final Dio dio;
  final int credentialEpoch;
  final bool Function() isCurrent;
  void _check() {
    if (!isCurrent()) {
      throw StateError('Account changed. Refresh stations and try again.');
    }
  }

  Options _options({String? method, ResponseType? responseType}) => Options(
      method: method,
      responseType: responseType,
      extra: {'credentialEpoch': credentialEpoch});
  Future<T> _owned<T>(Future<T> Function() call) async {
    _check();
    final result = await call();
    _check();
    return result;
  }

  Future<List<Map<String, dynamic>>> list() => _owned(() async {
        final response =
            await dio.get<List<dynamic>>('/api/v1/radio', options: _options());
        return (response.data ?? []).cast<Map<String, dynamic>>();
      });
  Future<void> save(int? id, Map<String, dynamic> body) => _owned(() async {
        await dio.request<void>(
            id == null ? '/api/v1/radio' : '/api/v1/radio/$id',
            data: body,
            options: _options(method: id == null ? 'POST' : 'PUT'));
      });
  Future<void> remove(int id) => _owned(() async {
        await dio.delete<void>('/api/v1/radio/$id', options: _options());
      });
  Future<Uint8List> artwork(int id) => _owned(() async {
        final response = await dio.get<List<int>>('/api/v1/radio/$id/artwork',
            options: _options(responseType: ResponseType.bytes));
        return Uint8List.fromList(response.data ?? []);
      });
}
