import 'package:flutter/services.dart';

enum LocalDocumentKind { audio, video }

class PickedLocalDocument {
  const PickedLocalDocument({
    required this.uri,
    required this.kind,
    required this.newPersistedReadGrant,
    this.name,
    this.mimeType,
    this.sizeBytes,
  });

  final String uri;
  final LocalDocumentKind kind;
  final bool newPersistedReadGrant;
  final String? name;
  final String? mimeType;
  final int? sizeBytes;
}

abstract interface class LocalDocumentPicker {
  /// Returns null when the user cancels the system picker.
  Future<PickedLocalDocument?> pick(LocalDocumentKind kind);

  /// Probes provider access when the user explicitly chooses to replace a file.
  Future<bool> isReadable(String uri);

  Future<void> releasePersistedReadGrant(String uri);
}

class LocalDocumentPickerException implements Exception {
  const LocalDocumentPickerException(this.message, {this.code});

  final String message;
  final String? code;

  @override
  String toString() => message;
}

/// SAF bridge used by the Android app. Tests inject an in-memory picker.
class MethodChannelLocalDocumentPicker implements LocalDocumentPicker {
  MethodChannelLocalDocumentPicker({MethodChannel? channel})
      : _channel = channel ?? const MethodChannel(_channelName);

  static const _channelName = 'zone.foo.player_android/document_compatibility';
  final MethodChannel _channel;

  @override
  Future<PickedLocalDocument?> pick(LocalDocumentKind kind) async {
    try {
      final result = await _channel.invokeMapMethod<String, dynamic>(
        'pick',
        {'kind': kind.name},
      );
      if (result == null) return null;
      final uri = result['uri'];
      if (uri is! String || !uri.startsWith('content://')) {
        throw const LocalDocumentPickerException(
          'The selected item did not provide a persistent Android document URI.',
          code: 'invalid_uri',
        );
      }
      final size = result['sizeBytes'];
      return PickedLocalDocument(
        uri: uri,
        kind: kind,
        newPersistedReadGrant: result['newPersistedReadGrant'] == true,
        name: result['name'] is String ? result['name'] as String : null,
        mimeType:
            result['mimeType'] is String ? result['mimeType'] as String : null,
        sizeBytes: size is int && size >= 0 ? size : null,
      );
    } on PlatformException catch (error) {
      throw LocalDocumentPickerException(
        _pickerErrorMessage(error),
        code: error.code,
      );
    }
  }

  @override
  Future<bool> isReadable(String uri) async {
    try {
      return await _channel.invokeMethod<bool>(
            'checkReadable',
            {'uri': uri},
          ) ??
          false;
    } on PlatformException {
      // Missing providers and revoked grants should leave the recovery choices
      // available, rather than preventing a replacement or removal.
      return false;
    }
  }

  @override
  Future<void> releasePersistedReadGrant(String uri) async {
    try {
      await _channel.invokeMethod<void>('release', {'uri': uri});
    } on PlatformException catch (error) {
      throw LocalDocumentPickerException(
        'The library item was removed, but Android could not release its file permission.',
        code: error.code,
      );
    }
  }

  String _pickerErrorMessage(PlatformException error) => switch (error.code) {
        'grant_failed' =>
          'Android could not keep access to this file. Choose a file from a provider that supports persistent access.',
        'picker_unavailable' =>
          'Android could not open the document picker. Check that a file provider is available.',
        _ => 'Could not select this file: ${error.message ?? error.code}',
      };
}
