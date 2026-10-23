import 'package:shared_preferences/shared_preferences.dart';

/// Download choice is independent of streaming and snapshotted before transfer.
class DownloadQuality {
  static Future<String> selected(String server) async =>
      (await SharedPreferences.getInstance())
          .getString('download-quality:$server') ??
      'original';

  static Future<void> save(String server, String profile) async {
    if (!await (await SharedPreferences.getInstance())
        .setString('download-quality:$server', profile)) {
      throw StateError('Could not save download quality');
    }
  }

  static String? contentType(String profile) => switch (profile) {
        'aac128' || 'aac256' => 'audio/mp4',
        'mp3192' => 'audio/mpeg',
        'opus96' => 'audio/ogg',
        _ => null,
      };

  static String sourceName(String source, String profile) {
    if (profile == 'original') return source;
    final extension = switch (profile) {
      'aac128' || 'aac256' => '.m4a',
      'mp3192' => '.mp3',
      'opus96' => '.ogg',
      _ => throw StateError(
          'Download quality unavailable. Choose another quality.'),
    };
    final dot = source.lastIndexOf('.');
    final slash = source.lastIndexOf('/');
    return '${dot > slash ? source.substring(0, dot) : source}$extension';
  }
}
