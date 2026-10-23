import 'package:flutter/services.dart';

/// Opens an attributed HTTP(S) metadata page in an installed browser.
Future<void> openMetadataLink(String value) async {
  final uri = Uri.tryParse(value);
  if (uri == null ||
      !{'https', 'http'}.contains(uri.scheme) ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty) {
    throw const FormatException('Invalid metadata link.');
  }
  await const MethodChannel('zone.foo.player_android/metadata_links')
      .invokeMethod<void>('open', value);
}
