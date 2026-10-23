import 'dart:async';
import 'dart:typed_data';
import 'package:audio_service/audio_service.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';
import '../services/external_metadata_links.dart';
import '../api/dio_client.dart';
import '../api/radio_api.dart';
import '../providers/api_client_provider.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/auth_state_provider.dart';
import '../providers/playback_session_provider.dart';
import '../providers/server_endpoint_provider.dart';
import '../services/playback_request.dart';
import '../services/playback_session_coordinator.dart';

/// Shared station browsing and live controls, with no finite-song timeline.
class RadioScreen extends ConsumerStatefulWidget {
  const RadioScreen({super.key});
  @override
  ConsumerState<RadioScreen> createState() => _RadioScreenState();
}

class _RadioScreenState extends ConsumerState<RadioScreen> {
  List<Map<String, dynamic>>? _stations;
  String _query = '';
  String? _error, _playError;
  bool _loading = false;
  int _attempt = 0, _loadGeneration = 0;
  RadioApi? _stationsApi, _loadingApi;
  int? _opening;
  PlaybackSessionLease? _pendingLease;
  final _art = <int, Future<Uint8List>>{};
  @override
  void initState() {
    super.initState();
    Future.microtask(_load);
  }

  @override
  void dispose() {
    _attempt++;
    if (_opening != null) unawaited(_pendingLease?.stop());
    super.dispose();
  }

  RadioApi _captureApi() {
    final client = ref.read(apiClientProvider);
    final mutations = ref.read(credentialMutationQueueProvider);
    final epoch = mutations.generation;
    final base = ref.read(playerBaseUrlProvider);
    final user = ref.read(authStateProvider).valueOrNull?.user?.id;
    return RadioApi(client.rawDio,
        credentialEpoch: epoch,
        isCurrent: () =>
            mounted &&
            mutations.generation == epoch &&
            identical(ref.read(apiClientProvider), client) &&
            ref.read(playerBaseUrlProvider) == base &&
            ref.read(authStateProvider).valueOrNull?.user?.id == user);
  }

  Future<void> _load() async {
    final api = _captureApi(), generation = ++_loadGeneration;
    bool current() =>
        mounted && generation == _loadGeneration && api.isCurrent();
    setState(() {
      _loading = true;
      _loadingApi = api;
      _error = null;
    });
    try {
      final rows = await api.list();
      if (current()) {
        setState(() {
          _stations = rows;
          _stationsApi = api;
          _art.clear();
        });
      }
    } catch (_) {
      if (current()) {
        setState(() => _error =
            'Could not load stations. Check your connection, then retry.');
      }
    } finally {
      if (current()) setState(() => _loading = false);
    }
  }

  Future<void> _play(Map<String, dynamic> station) async {
    final scope = _stationsApi;
    if (scope == null || !scope.isCurrent()) {
      await _load();
      return;
    }
    final attempt = ++_attempt;
    final handler = ref.read(audioHandlerProvider);
    final coordinator = ref.read(playbackSessionCoordinatorProvider);
    final base = ref.read(playerBaseUrlProvider);
    final user = ref.read(authStateProvider).valueOrNull?.user;
    if (user == null) return;
    final mutations = ref.read(credentialMutationQueueProvider);
    final accountGeneration = mutations.generation;
    final api = ref.read(apiClientProvider);
    final uri =
        Uri.parse('${api.baseUrl}/api/v1/radio/${station['id']}/stream');
    setState(() {
      _opening = station['id'] as int;
      _playError = null;
    });
    PlaybackSessionLease? lease;
    bool current() =>
        mounted &&
        attempt == _attempt &&
        lease?.isCurrent == true &&
        mutations.generation == accountGeneration &&
        scope.isCurrent();
    try {
      lease = await coordinator.claim(
          kind: PlaybackSourceKind.server,
          identity: 'radio:${base.origin}:${user.id}:${station['id']}',
          stop: handler.stop,
          sourceUri: uri.toString());
      _pendingLease = lease;
      if (!current()) {
        await lease?.stop();
        return;
      }
      final generation = handler.beginSourceSession(
          ownsSource: () => lease?.isCurrent == true);
      final transport = handler.transportRevision;
      final request = LiveRadioPlaybackRequest(
          stationId: station['id'] as int,
          serverOrigin: base.origin,
          userId: user.id,
          sourceUri: uri,
          title: station['name'] as String,
          artworkUri: (station['artwork_url'] as String).isEmpty
              ? null
              : Uri.parse(
                  '${api.baseUrl}/api/v1/radio/${station['id']}/artwork'));
      handler.setMediaRequest(request);
      final headers = await accountRequestHeaders(
          uri: uri,
          baseUrl: base,
          storage: ref.read(tokenStorageProvider),
          cookieJar: ref.read(cookieJarProvider),
          mutations: mutations);
      final routed = await ref.read(serverEndpointRouterProvider).resolve(uri);
      if (!current()) return;
      if (headers.isEmpty) throw StateError('Sign in to play stations.');
      await handler.loadLiveSourceForSession(
          AudioSource.uri(routed, headers: headers), generation, transport);
      if (!current()) return;
    } catch (_) {
      if (current()) {
        await handler.player.stop();
        if (mounted) {
          setState(() => _playError =
              'Station unavailable or offline. Retry to reconnect.');
        }
      }
    } finally {
      if (mounted && attempt == _attempt) setState(() => _opening = null);
    }
  }

  Future<Uint8List> _artwork(int id) =>
      _art.putIfAbsent(id, () => _stationsApi!.artwork(id));
  Widget _cover(Map<String, dynamic> station) => SizedBox(
      width: 56,
      height: 56,
      child: (station['artwork_url'] as String).isEmpty
          ? const Icon(Icons.radio)
          : FutureBuilder<Uint8List>(
              future: _artwork(station['id'] as int),
              builder: (_, snapshot) => snapshot.hasData
                  ? Image.memory(snapshot.data!,
                      fit: BoxFit.cover,
                      errorBuilder: (_, __, ___) => const Icon(Icons.radio))
                  : const Icon(Icons.radio)));
  Future<void> _edit([Map<String, dynamic>? station]) async {
    final api = station == null ? _captureApi() : _stationsApi;
    if (api == null || !api.isCurrent()) {
      await _load();
      return;
    }
    final saved = await showDialog<bool>(
        context: context,
        builder: (_) => RadioStationEditor(station: station, api: api));
    if (saved == true && mounted && api.isCurrent()) await _load();
  }

  Future<void> _delete(Map<String, dynamic> station) async {
    final api = _stationsApi;
    if (api == null || !api.isCurrent()) {
      await _load();
      return;
    }
    final confirmed = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
                title: const Text('Remove station?'),
                content: Text('Remove ${station['name']} for everyone?'),
                actions: [
                  TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: const Text('Cancel')),
                  FilledButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: const Text('Remove'))
                ]));
    if (confirmed != true || !mounted || !api.isCurrent()) return;
    try {
      await api.remove(station['id'] as int);
      if (!mounted || !api.isCurrent()) return;
      final handler = ref.read(audioHandlerProvider);
      if (handler.currentMetadataRequest case LiveRadioPlaybackRequest request
          when request.stationId == station['id']) {
        await ref.read(playbackSessionCoordinatorProvider).stopCurrent();
      }
      if (mounted) await _load();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content: Text('Could not remove station. Retry when connected.')));
      }
    }
  }

  Future<void> _website(String address) async {
    try {
      await openMetadataLink(address);
      return;
    } catch (_) {
      /* Show the same actionable message for unavailable handlers. */
    }
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Could not open station website.')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final admin =
        ref.watch(authStateProvider).valueOrNull?.user?.isAdmin == true;
    final handler = ref.watch(audioHandlerProvider);
    ref.watch(apiClientProvider);
    final currentStations = _stationsApi?.isCurrent() == true;
    final stations = (currentStations
            ? (_stations ?? [])
            : <Map<String, dynamic>>[])
        .where((s) =>
            (s['name'] as String).toLowerCase().contains(_query.toLowerCase()))
        .toList();
    return Scaffold(
        appBar: AppBar(title: const Text('Internet radio'), actions: [
          IconButton(
              tooltip: 'Refresh stations',
              onPressed: _loading ? null : _load,
              icon: const Icon(Icons.refresh)),
          if (admin)
            IconButton(
                tooltip: 'Add station',
                onPressed: () => _edit(),
                icon: const Icon(Icons.add))
        ]),
        body: Column(children: [
          Padding(
              padding: const EdgeInsets.all(16),
              child: TextField(
                  decoration: const InputDecoration(
                      labelText: 'Search stations',
                      prefixIcon: Icon(Icons.search)),
                  onChanged: (value) => setState(() => _query = value))),
          if (_loading && _loadingApi?.isCurrent() == true)
            const LinearProgressIndicator(),
          if (_stationsApi != null && !currentStations ||
              _loadingApi != null && !_loadingApi!.isCurrent())
            TextButton(
                onPressed: _load,
                child: const Text('Account changed. Refresh stations.')),
          if (_error != null)
            Padding(
                padding: const EdgeInsets.all(16),
                child: Column(children: [
                  Text(_error!),
                  TextButton(onPressed: _load, child: const Text('Retry'))
                ])),
          if (_stations != null && stations.isEmpty)
            Padding(
                padding: const EdgeInsets.all(16),
                child: Text(_query.isEmpty
                    ? 'No stations yet. Ask an administrator to add one.'
                    : 'No matching stations.')),
          Expanded(
              child: StreamBuilder<PlaybackState>(
                  stream: handler.playbackState,
                  builder: (context, snapshot) {
                    final request = handler.currentMetadataRequest;
                    final state = snapshot.data;
                    return ListView.builder(
                        itemCount: stations.length,
                        itemBuilder: (context, index) {
                          final station = stations[index];
                          final active = request is LiveRadioPlaybackRequest &&
                              request.stationId == station['id'];
                          final busy = _opening == station['id'] ||
                              active &&
                                  (state?.processingState ==
                                          AudioProcessingState.loading ||
                                      state?.processingState ==
                                          AudioProcessingState.buffering);
                          final failed = active &&
                              (_playError != null ||
                                  state?.processingState ==
                                      AudioProcessingState.error ||
                                  state?.processingState ==
                                      AudioProcessingState.completed);
                          final status = busy
                              ? 'Buffering…'
                              : failed
                                  ? (_playError ??
                                      'Station disconnected. Retry to reconnect.')
                                  : active && state?.playing == true
                                      ? 'Live'
                                      : active
                                          ? 'Stopped · Play reconnects live'
                                          : 'Live radio';
                          return Card(
                              child: Padding(
                                  padding: const EdgeInsets.all(8),
                                  child: Column(
                                      crossAxisAlignment:
                                          CrossAxisAlignment.stretch,
                                      children: [
                                        ListTile(
                                            leading: _cover(station),
                                            title:
                                                Text(station['name'] as String),
                                            subtitle: Text(status),
                                            trailing: admin
                                                ? PopupMenuButton<String>(
                                                    tooltip: 'Manage station',
                                                    onSelected: (action) =>
                                                        action == 'edit'
                                                            ? _edit(station)
                                                            : _delete(station),
                                                    itemBuilder: (_) => [
                                                          const PopupMenuItem(
                                                              value: 'edit',
                                                              child:
                                                                  Text('Edit')),
                                                          const PopupMenuItem(
                                                              value: 'delete',
                                                              child: Text(
                                                                  'Remove'))
                                                        ])
                                                : null),
                                        Wrap(spacing: 8, children: [
                                          FilledButton.icon(
                                              onPressed: busy
                                                  ? null
                                                  : active &&
                                                          state?.playing == true
                                                      ? () => handler.pause()
                                                      : () => _play(station),
                                              icon: Icon(active &&
                                                      state?.playing == true
                                                  ? Icons.stop
                                                  : Icons.play_arrow),
                                              label: Text(busy
                                                  ? 'Buffering'
                                                  : active &&
                                                          state?.playing == true
                                                      ? 'Stop'
                                                      : failed
                                                          ? 'Retry'
                                                          : 'Play')),
                                          if ((station['homepage_url']
                                                  as String)
                                              .isNotEmpty)
                                            TextButton(
                                                onPressed: () => _website(
                                                    station['homepage_url']
                                                        as String),
                                                child: const Text(
                                                    'Station website'))
                                        ])
                                      ])));
                        });
                  }))
        ]));
  }
}

class RadioStationEditor extends StatefulWidget {
  const RadioStationEditor({super.key, this.station, required this.api});
  final RadioApi api;
  final Map<String, dynamic>? station;
  @override
  State<RadioStationEditor> createState() => _StationEditorState();
}

class _StationEditorState extends State<RadioStationEditor> {
  late final _fields = {
    for (final key in ['name', 'stream_url', 'homepage_url', 'artwork_url'])
      key: TextEditingController(text: widget.station?[key] as String? ?? '')
  };
  late bool _local = widget.station?['local_network'] == true;
  bool _saving = false;
  String? _error;
  @override
  void dispose() {
    for (final field in _fields.values) {
      field.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final data = {
        for (final field in _fields.entries) field.key: field.value.text.trim(),
        'local_network': _local
      };
      await widget.api.save(widget.station?['id'] as int?, data);
      if (mounted) Navigator.pop(context, true);
    } on DioException catch (error) {
      if (mounted) {
        setState(() => _error = !widget.api.isCurrent()
            ? 'Account changed. Close this dialog and refresh stations.'
            : error.response?.data is Map
                ? '${(error.response!.data as Map)['error']}'
                : 'Could not save station. Check connection and retry.');
      }
    } catch (_) {
      if (mounted) {
        setState(() => _error = widget.api.isCurrent()
            ? 'Could not save station. Check connection and retry.'
            : 'Account changed. Close this dialog and refresh stations.');
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
          title: Text(widget.station == null ? 'Add station' : 'Edit station'),
          content: SingleChildScrollView(
              child: Column(mainAxisSize: MainAxisSize.min, children: [
            for (final field in _fields.entries)
              Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: TextField(
                      controller: field.value,
                      keyboardType: field.key == 'name'
                          ? TextInputType.text
                          : TextInputType.url,
                      decoration: InputDecoration(
                          labelText: switch (field.key) {
                        'name' => 'Station name',
                        'stream_url' => 'Stream URL',
                        'homepage_url' => 'Website URL (optional)',
                        _ => 'Artwork URL (optional)'
                      }))),
            CheckboxListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Allow local-network station'),
                subtitle: const Text(
                    'Permit private or loopback stream and artwork hosts.'),
                value: _local,
                onChanged: _saving
                    ? null
                    : (value) => setState(() => _local = value!)),
            if (_error != null)
              Text(_error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error))
          ])),
          actions: [
            TextButton(
                onPressed: _saving ? null : () => Navigator.pop(context),
                child: const Text('Cancel')),
            FilledButton(
                onPressed: _saving ? null : _save,
                child: Text(_saving ? 'Saving…' : 'Save'))
          ]);
}
