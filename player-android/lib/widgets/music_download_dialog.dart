import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/media.dart';
import 'download_quality_picker.dart';
import '../providers/offline_downloads_provider.dart';
import '../providers/api_client_provider.dart';

/// A single readable progress surface shared by collection and song selections.
Future<void> showMusicDownloadDialog(
    BuildContext context, WidgetRef ref, List<Media> songs) async {
  final downloads = ref.read(offlineDownloadsProvider);
  final mutations = ref.read(credentialMutationQueueProvider);
  final generation = mutations.generation;
  String? selectedProfile;
  Future<void> Function(Media)? save;
  final client = ref.read(apiClientProvider);
  bool authorized() =>
      mutations.generation == generation && mutations.credentialsEnabled;
  var started = false;
  String? error;
  await showDialog<void>(
      context: context,
      builder: (context) => StatefulBuilder(
          builder: (context, update) => ListenableBuilder(
              listenable: downloads,
              builder: (context, _) {
                final batch = started ? downloads.batch : null;
                final active = started ? downloads.active : null;
                final busy = batch?.running == true;
                final count = songs.map((s) => s.id).toSet().length;
                Future<void> run(List<Media> pending) async {
                  update(() {
                    started = true;
                    error = null;
                  });
                  try {
                    save ??= mediaOfflineSaver(ref,
                        batchMember: true, downloadProfile: selectedProfile);
                    await downloads.downloadBatch(pending, save!, authorized);
                  } catch (_) {
                    if (context.mounted) {
                      update(() => error =
                          'Another download is running. Cancel it in Downloads or try again later.');
                    }
                  }
                }

                final remaining = batch == null
                    ? songs
                    : batch.songs
                        .where((s) => batch.failed.contains(s))
                        .toList();
                // Cancellation retains completed files; retrying the selection skips those
                // already present through the normal repository existence check.
                final retry = batch?.cancelled == true ? songs : remaining;
                return AlertDialog(
                  title: const Text('Keep music offline'),
                  content: SizedBox(
                      width: 420,
                      child: SingleChildScrollView(
                          child: Column(
                              mainAxisSize: MainAxisSize.min,
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                            Text('$count unique songs · Saved to Downloads'),
                            const SizedBox(height: 12),
                            DownloadQualityPicker(
                                client: client,
                                enabled: !started,
                                onSelectionReady: (value) {
                                  update(() => selectedProfile = value);
                                }),
                            const Text(
                                'Keep Player open while downloading. Each batch keeps the quality selected when it starts.'),
                            if (batch != null) ...[
                              const SizedBox(height: 12),
                              Semantics(
                                  liveRegion: true,
                                  child: Text(
                                      '${batch.completed} of ${batch.songs.length} saved · ${batch.failed.length} failed${batch.cancelled ? ' · Canceled' : busy ? '' : ' · Finished'}')),
                              LinearProgressIndicator(
                                  value: batch.songs.isEmpty
                                      ? 0
                                      : (batch.completed +
                                              batch.failed.length) /
                                          batch.songs.length),
                            ],
                            if (active != null) ...[
                              Text(active.title,
                                  maxLines: 2, overflow: TextOverflow.ellipsis),
                              Text(
                                  '${(active.received / 1048576).toStringAsFixed(1)} MiB received'),
                            ],
                            if (batch?.failed.isNotEmpty == true)
                              const Text(
                                  'Some songs could not be saved. Check your connection, access and storage, then retry failed songs. Completed files remain available.'),
                            if (error != null) Text(error!),
                          ]))),
                  actions: [
                    TextButton(
                        onPressed: () => Navigator.pop(context),
                        child: Text(busy ? 'Hide progress' : 'Close')),
                    if (busy)
                      TextButton(
                          onPressed: downloads.cancel,
                          child: const Text('Cancel downloads'))
                    else
                      FilledButton(
                          onPressed: !authorized() ||
                                  (!started && selectedProfile == null) ||
                                  (started && retry.isEmpty)
                              ? null
                              : () => run(started ? retry : songs),
                          child: Text(started ? 'Retry' : 'Keep offline')),
                  ],
                );
              })));
}
