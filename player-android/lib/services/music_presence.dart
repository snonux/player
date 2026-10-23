import 'dart:async';
import 'dart:math';
import 'package:flutter/foundation.dart';

/// Coordinates explicit per-client sharing and rejects superseded account data.
class MusicPresence extends ChangeNotifier {
  MusicPresence(
      {required this.request, required this.snapshot, String? clientId})
      : clientId = clientId ??
            List.generate(
                16,
                (_) => Random.secure()
                    .nextInt(256)
                    .toRadixString(16)
                    .padLeft(2, '0')).join();
  final Future<List<Map<String, dynamic>>?> Function(String method, int user,
      int credentialEpoch, Map<String, dynamic>? body) request;
  final Map<String, dynamic>? Function() snapshot;
  final String clientId;
  bool sharing = false,
      open = false,
      _busy = false,
      _published = false,
      _disposed = false,
      _refreshPending = false;
  List<Map<String, dynamic>> rows = [];
  String message = '';
  int? _user;
  String? _origin;
  int _credentialEpoch = 0, _generation = 0, _sequence = 0;

  void setAccount(String origin, int? user, int credentialEpoch) {
    if (_origin == origin &&
        _user == user &&
        _credentialEpoch == credentialEpoch) {
      return;
    }
    final previous = _user, previousEpoch = _credentialEpoch;
    _generation++;
    _origin = origin;
    _user = user;
    _credentialEpoch = credentialEpoch;
    sharing = false;
    _published = false;
    rows = [];
    message = '';
    _notify();
    if (previous != null) unawaited(_remove(previous, previousEpoch));
  }

  void setSharing(bool value) {
    _generation++;
    sharing = value && _user != null;
    rows = [];
    message = '';
    _notify();
    if (!sharing && _user != null) {
      _published = false;
      unawaited(_remove(_user!, _credentialEpoch));
    }
    unawaited(refresh());
  }

  void setOpen(bool value) {
    open = value;
    rows = [];
    message = value ? 'Loading listeners…' : '';
    _notify();
    if (value) unawaited(refresh());
  }

  Future<void> refresh() async {
    if (_disposed || _user == null) return;
    if (_busy) {
      _refreshPending = true;
      return;
    }
    final user = _user!,
        credentialEpoch = _credentialEpoch,
        generation = _generation;
    _busy = true;
    bool current() => !_disposed && generation == _generation;
    try {
      final body = snapshot();
      if (sharing && body != null) {
        await request('PUT', user, credentialEpoch, {
          ...body,
          'client_id': clientId,
          'client': 'android',
          'sequence': ++_sequence
        });
        if (!current()) return;
        _published = true;
      } else if (_published) {
        _published = false;
        await _remove(user, credentialEpoch);
      }
      if (!current()) return;
      if (open) {
        final result = await request('GET', user, credentialEpoch, null);
        if (!current()) return;
        rows = result ?? [];
      }
      message = '';
    } catch (_) {
      if (current()) {
        rows = [];
        message = 'Listeners unavailable. Retry.';
        if (_published) {
          _published = false;
          unawaited(_remove(user, credentialEpoch));
        }
      }
    } finally {
      _busy = false;
      if (current()) _notify();
      if (_refreshPending) {
        _refreshPending = false;
        unawaited(refresh());
      }
    }
  }

  Future<void> _remove(int user, int epoch) async {
    try {
      await request('DELETE', user, epoch,
          {'client_id': clientId, 'sequence': ++_sequence});
    } catch (_) {/* TTL bounds disconnected cleanup. */}
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    if (_user != null) unawaited(_remove(_user!, _credentialEpoch));
    super.dispose();
  }
}
