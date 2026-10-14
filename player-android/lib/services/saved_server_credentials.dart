import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:local_auth/local_auth.dart';

import '../models/user.dart';
import '../providers/settings_provider.dart';

class SavedServerCredential {
  const SavedServerCredential({
    required this.origin,
    required this.token,
    required this.tokenId,
    required this.user,
    required this.expiresAt,
  });

  final String origin;
  final String token;
  final int? tokenId;
  final User user;
  final int expiresAt;
  bool get isValid =>
      token.isNotEmpty &&
      (tokenId == null || tokenId! > 0) &&
      user.id > 0 &&
      user.username.isNotEmpty &&
      DateTime.now().millisecondsSinceEpoch < expiresAt;

  Map<String, dynamic> toJson() => {
        'origin': origin,
        'token': token,
        'tokenId': tokenId,
        'user': user.toJson(),
        'expiresAt': expiresAt,
      };
}

abstract interface class SavedServerCredentials {
  Future<SavedServerCredential?> read(String origin);
  Future<void> write(SavedServerCredential credential);
  Future<void> delete(String origin);
}

class SecureSavedServerCredentials implements SavedServerCredentials {
  SecureSavedServerCredentials({FlutterSecureStorage? storage})
      : _storage = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _storage;
  String _key(String origin) => 'server_credential_'
      '${sha256.convert(utf8.encode(parseServerBaseUrl(origin).origin))}';

  @override
  Future<SavedServerCredential?> read(String origin) async {
    final canonical = parseServerBaseUrl(origin).origin;
    final raw = await _storage.read(key: _key(canonical));
    if (raw == null) return null;
    try {
      final json = jsonDecode(raw) as Map<String, dynamic>;
      if (json['origin'] != canonical) return null;
      final credential = SavedServerCredential(
        origin: canonical,
        token: json['token'] as String,
        tokenId: json['tokenId'] as int?,
        user: User.fromJson(json['user'] as Map<String, dynamic>),
        expiresAt: json['expiresAt'] as int,
      );
      return credential.isValid ? credential : null;
    } on FormatException {
      return null;
    } on TypeError {
      return null;
    }
  }

  @override
  Future<void> write(SavedServerCredential credential) => _storage.write(
        key: _key(credential.origin),
        value: jsonEncode({
          ...credential.toJson(),
          'origin': parseServerBaseUrl(credential.origin).origin,
        }),
      );
  @override
  Future<void> delete(String origin) => _storage.delete(key: _key(origin));
}

abstract interface class ServerBiometricUnlock {
  Future<bool> authenticate(String origin);
}

class DeviceServerBiometricUnlock implements ServerBiometricUnlock {
  final _auth = LocalAuthentication();
  @override
  Future<bool> authenticate(String origin) => _auth.authenticate(
        localizedReason: 'Unlock your saved Player login for $origin',
        options: const AuthenticationOptions(
            biometricOnly: true, useErrorDialogs: false),
      );
}

final savedServerCredentialsProvider = Provider<SavedServerCredentials>(
  (ref) => SecureSavedServerCredentials(),
);
final serverBiometricUnlockProvider = Provider<ServerBiometricUnlock>(
  (ref) => DeviceServerBiometricUnlock(),
);
final selectedServerCredentialProvider = FutureProvider<SavedServerCredential?>(
  (ref) {
    final origin = ref.watch(settingsProvider).valueOrNull?.serverBaseUrl;
    if (origin == null) return Future.value(null);
    return ref.watch(savedServerCredentialsProvider).read(origin);
  },
);
