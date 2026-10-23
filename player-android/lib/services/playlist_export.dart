import 'package:flutter/services.dart';

/// Saves UTF-8 M3U8 content using Android's system document destination chooser.
/// A null result means the user cancelled; no broad storage permission is needed.
class PlaylistExport {
  static const _channel =
      MethodChannel('zone.foo.player_android/playlist_export');

  static Future<bool> save(String filename, String content) async =>
      await _channel.invokeMethod<bool>('save', {
        'filename': filename,
        'content': content,
      }) ??
      false;
}
