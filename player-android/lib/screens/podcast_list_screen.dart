import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/podcast_library_provider.dart';
import 'standalone_podcasts_screen.dart';

/// Episodes are the default; the overview keeps subscription management close.
class PodcastListScreen extends ConsumerStatefulWidget {
  const PodcastListScreen({super.key, this.serverSetId, this.serverSetName});
  final int? serverSetId;
  final String? serverSetName;
  @override
  ConsumerState<PodcastListScreen> createState() => _PodcastListScreenState();
}

class _PodcastListScreenState extends ConsumerState<PodcastListScreen> {
  bool overview = false;
  @override
  Widget build(BuildContext context) {
    final sync = ref.watch(podcastServerSubscriptionsProvider);
    final syncMessage = sync.valueOrNull?.errorKind == null
        ? null
        : 'Could not sync all podcasts. Showing saved items; reload to retry.';
    final toggle = Padding(
      padding: const EdgeInsets.fromLTRB(8, 0, 8, 8),
      child: SegmentedButton<bool>(
        segments: const [
          ButtonSegment(value: false, label: Text('Episodes')),
          ButtonSegment(value: true, label: Text('Podcasts')),
        ],
        selected: {overview},
        style: const ButtonStyle(
            minimumSize: WidgetStatePropertyAll(Size(48, 48))),
        onSelectionChanged: (value) => setState(() => overview = value.single),
      ),
    );
    return overview
        ? StandalonePodcastsScreen(
            viewSwitcher: toggle, syncMessage: syncMessage)
        : LocalPodcastEpisodesScreen(
            serverSetId: widget.serverSetId,
            serverSetName: widget.serverSetName,
            viewSwitcher: toggle,
            syncMessage: syncMessage);
  }
}
