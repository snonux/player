// Standalone entry point for validating Android SAF grants and the resolved
// just_audio/video_player plugins. Launch with:
// flutter run --debug -t lib/content_uri_compatibility_harness.dart
import 'dart:async';

import 'package:audio_service/audio_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:video_player/video_player.dart';

import 'services/audio_handler.dart';

const _documents = MethodChannel(
  'zone.foo.player_android/document_compatibility',
);

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final handler = await AudioService.init<PlayerAudioHandler>(
    builder: () => PlayerAudioHandler(AudioPlayer()),
    config: const AudioServiceConfig(
      androidNotificationChannelName: 'Player Compatibility Test',
      androidNotificationIcon: 'drawable/ic_launcher',
      androidStopForegroundOnPause: false,
    ),
  );
  runApp(_CompatibilityApp(handler: handler));
}

class _CompatibilityApp extends StatelessWidget {
  const _CompatibilityApp({required this.handler});

  final PlayerAudioHandler handler;

  @override
  Widget build(BuildContext context) => MaterialApp(
        title: 'Content URI Compatibility',
        home: _CompatibilityScreen(handler: handler),
      );
}

class _CompatibilityScreen extends StatefulWidget {
  const _CompatibilityScreen({required this.handler});

  final PlayerAudioHandler handler;

  @override
  State<_CompatibilityScreen> createState() => _CompatibilityScreenState();
}

class _CompatibilityScreenState extends State<_CompatibilityScreen> {
  static const _uriKey = 'compatibility_uri';
  static const _kindKey = 'compatibility_kind';

  String? _uri;
  String? _kind;
  String _status = 'Pick an audio or video document.';
  VideoPlayerController? _video;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _restoreSelection();
  }

  Future<void> _restoreSelection() async {
    final prefs = await SharedPreferences.getInstance();
    final uri = prefs.getString(_uriKey);
    final kind = prefs.getString(_kindKey);
    if (!mounted || uri == null || (kind != 'audio' && kind != 'video')) return;
    setState(() {
      _uri = uri;
      _kind = kind;
      _status = 'Restored persisted selection. Tap Play to verify access.';
    });
  }

  Future<void> _pick(String kind) async {
    await _disposeVideo();
    await widget.handler.stop();
    setState(() => _busy = true);
    try {
      final selection = await _documents.invokeMapMethod<String, dynamic>(
        'pick',
        {'kind': kind},
      );
      if (selection == null) {
        _show('Picker cancelled.');
        return;
      }
      final uri = selection['uri'] as String;
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_uriKey, uri);
      await prefs.setString(_kindKey, kind);
      _show('Persisted ${selection['name'] ?? uri} (${selection['mimeType']}).');
      setState(() {
        _uri = uri;
        _kind = kind;
      });
    } on PlatformException catch (error) {
      _show('Picker/grant failed: ${error.code}: ${error.message}');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _play() async {
    final uri = _uri;
    final kind = _kind;
    if (uri == null || kind == null || _busy) return;
    setState(() => _busy = true);
    try {
      if (kind == 'audio') {
        await _disposeVideo();
        final player = widget.handler.player;
        await player.setAudioSource(AudioSource.uri(Uri.parse(uri)));
        widget.handler.setMediaItem(id: uri, title: 'Local URI compatibility');
        _startAudioPlayback();
        _show('Audio source loaded: $uri');
      } else {
        await widget.handler.stop();
        await _disposeVideo();
        final controller = VideoPlayerController.contentUri(Uri.parse(uri));
        _video = controller;
        await controller.initialize();
        await controller.play();
        if (mounted) setState(() {});
        _show('Video source initialized: ${controller.value.duration}');
      }
    } catch (error) {
      _show('Playback failed: $error');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _seek() async {
    if (_kind == 'audio') {
      final player = widget.handler.player;
      final duration = player.duration;
      if (duration == null || duration <= const Duration(seconds: 10)) {
        _show('Audio is too short to test a 10-second seek.');
        return;
      }
      await player.seek(const Duration(seconds: 10));
      _show('Audio seeked to ${player.position}.');
      return;
    }
    final video = _video;
    if (video == null || video.value.duration <= const Duration(seconds: 10)) {
      _show('Video is too short to test a 10-second seek.');
      return;
    }
    await video.seekTo(const Duration(seconds: 10));
    _show('Video seeked to ${video.value.position}.');
  }

  void _startAudioPlayback() {
    unawaited(widget.handler.play().catchError((Object error) {
      _show('Playback failed: $error');
    }));
  }

  Future<void> _release() async {
    final uri = _uri;
    if (uri == null) return;
    await widget.handler.stop();
    await _disposeVideo();
    try {
      await _documents.invokeMethod<void>('release', {'uri': uri});
      _show('Persisted permission released; force-stop and relaunch, then Play should fail.');
    } on PlatformException catch (error) {
      _show('Permission release failed: ${error.code}: ${error.message}');
    }
  }

  Future<void> _disposeVideo() async {
    final video = _video;
    _video = null;
    await video?.dispose();
  }

  void _show(String message) {
    if (!mounted) return;
    setState(() => _status = message);
  }

  @override
  void dispose() {
    _video?.dispose();
    widget.handler.stop();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar: AppBar(title: const Text('Content URI Compatibility')),
        body: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            Text(_status, key: const Key('compatibility_status')),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: _busy ? null : () => _pick('audio'),
              child: const Text('Pick audio'),
            ),
            FilledButton(
              onPressed: _busy ? null : () => _pick('video'),
              child: const Text('Pick video'),
            ),
            FilledButton(
              onPressed: _uri == null || _busy ? null : _play,
              child: const Text('Play'),
            ),
            OutlinedButton(
              onPressed: _uri == null || _busy ? null : _seek,
              child: const Text('Seek to 10 seconds'),
            ),
            OutlinedButton(
              onPressed: _busy ? null : widget.handler.pause,
              child: const Text('Pause audio'),
            ),
            OutlinedButton(
              onPressed: _busy ? null : _startAudioPlayback,
              child: const Text('Resume audio'),
            ),
            OutlinedButton(
              onPressed: _uri == null || _busy ? null : _release,
              child: const Text('Revoke persisted access'),
            ),
            if (_video?.value.isInitialized == true)
              AspectRatio(
                aspectRatio: _video!.value.aspectRatio,
                child: VideoPlayer(_video!),
              ),
          ],
        ),
      );
}
