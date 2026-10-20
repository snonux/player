import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/radio_station.dart';
import '../providers/api_client_provider.dart';
import '../providers/radio_provider.dart';
import '../services/radio_controller.dart';
import '../utils/error_mappers.dart';
import '../widgets/authenticated_network_image.dart';

/// Stations requested per explorer page.
const radioPageSize = 50;

/// Web radio: an explorer of the station directory (search, genre, country
/// and order), auto explore, and the user's bookmarked stations.
///
/// Playback is owned by [radioControllerProvider], not by this screen, so a
/// station keeps playing in the background after the screen is closed.
class RadioScreen extends ConsumerWidget {
  const RadioScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return DefaultTabController(
      length: 2,
      child: Scaffold(
        appBar: AppBar(
          title: const Text('Web radio'),
          bottom: const TabBar(
            tabs: [
              Tab(
                  key: Key('radio_tab_explore'),
                  icon: Icon(Icons.travel_explore),
                  text: 'Explore'),
              Tab(
                  key: Key('radio_tab_bookmarks'),
                  icon: Icon(Icons.bookmarks_outlined),
                  text: 'Bookmarks'),
            ],
          ),
        ),
        body: const TabBarView(
          children: [RadioExploreTab(), RadioBookmarksTab()],
        ),
        bottomNavigationBar: const RadioNowPlayingBar(),
      ),
    );
  }
}

// -----------------------------------------------------------------------------
// Explore
// -----------------------------------------------------------------------------

class RadioExploreTab extends ConsumerStatefulWidget {
  const RadioExploreTab({super.key});

  @override
  ConsumerState<RadioExploreTab> createState() => _RadioExploreTabState();
}

class _RadioExploreTabState extends ConsumerState<RadioExploreTab>
    with AutomaticKeepAliveClientMixin {
  final _search = TextEditingController();
  RadioQuery _query = const RadioQuery();
  String? _countryName;
  final List<RadioStation> _stations = [];
  bool _loading = false;
  bool _hasMore = false;
  String? _error;
  int _generation = 0;

  @override
  bool get wantKeepAlive => true;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  Future<void> _load({bool more = false}) async {
    if (!mounted) return;
    final generation = ++_generation;
    setState(() {
      _loading = true;
      _error = null;
      if (!more) {
        _stations.clear();
        _hasMore = false;
      }
    });
    try {
      final page = await ref.read(apiClientProvider).searchRadioStations(
            search: _query.search,
            tag: _query.tag,
            country: _query.country,
            order: _query.order,
            limit: radioPageSize,
            offset: more ? _stations.length : 0,
          );
      if (!mounted || generation != _generation) return;
      ref.read(radioControllerProvider).rememberBookmarks(page);
      setState(() {
        _stations.addAll(page);
        _hasMore = page.length >= radioPageSize;
        _loading = false;
      });
    } catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _error = radioErrorMessage(error);
        _loading = false;
      });
    }
  }

  void _apply(RadioQuery query, {String? countryName}) {
    setState(() {
      _query = query;
      if (query.country == null) {
        _countryName = null;
      } else if (countryName != null) {
        _countryName = countryName;
      }
    });
    unawaited(_load());
  }

  Future<void> _pickTag() async {
    final picked = await showModalBottomSheet<_Pick>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _PickerSheet(
        title: 'Genre',
        searchHint: 'Filter genres',
        load: () async => (await ref.read(apiClientProvider).listRadioTags())
            .map((t) => _Option(t.name, t.name, t.stationCount))
            .toList(),
      ),
    );
    if (picked == null) return;
    _apply(_query.copyWith(tag: () => picked.value));
  }

  Future<void> _pickCountry() async {
    final picked = await showModalBottomSheet<_Pick>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _PickerSheet(
        title: 'Country',
        searchHint: 'Filter countries',
        load: () async =>
            (await ref.read(apiClientProvider).listRadioCountries())
                .map((c) => _Option(c.code, c.name, c.stationCount))
                .toList(),
      ),
    );
    if (picked == null) return;
    _apply(_query.copyWith(country: () => picked.value),
        countryName: picked.label);
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);
    final radio = ref.watch(radioControllerProvider);
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        key: const Key('radio_explore_list'),
        padding: const EdgeInsets.only(bottom: 16),
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
            child: TextField(
              key: const Key('radio_search_field'),
              controller: _search,
              textInputAction: TextInputAction.search,
              decoration: InputDecoration(
                labelText: 'Search stations',
                prefixIcon: const Icon(Icons.search),
                border: const OutlineInputBorder(),
                suffixIcon: _search.text.isEmpty
                    ? null
                    : IconButton(
                        tooltip: 'Clear search',
                        icon: const Icon(Icons.clear),
                        onPressed: () {
                          _search.clear();
                          _apply(_query.copyWith(search: ''));
                        },
                      ),
              ),
              onChanged: (_) => setState(() {}),
              onSubmitted: (text) => _apply(_query.copyWith(search: text)),
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12),
            child: Wrap(
              spacing: 8,
              runSpacing: 4,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                _FilterChip(
                  key: const Key('radio_filter_genre'),
                  icon: Icons.label_outline,
                  label: _query.tag == null ? 'Genre' : 'Genre: ${_query.tag}',
                  active: _query.tag != null,
                  onPressed: _pickTag,
                  onClear: () => _apply(_query.copyWith(tag: () => null)),
                ),
                _FilterChip(
                  key: const Key('radio_filter_country'),
                  icon: Icons.public,
                  label: _query.country == null
                      ? 'Country'
                      : 'Country: ${_countryName ?? _query.country}',
                  active: _query.country != null,
                  onPressed: _pickCountry,
                  onClear: () => _apply(_query.copyWith(country: () => null)),
                ),
                PopupMenuButton<RadioOrder>(
                  key: const Key('radio_order_menu'),
                  tooltip: 'Sort order',
                  initialValue: _query.order,
                  onSelected: (order) => _apply(_query.copyWith(order: order)),
                  itemBuilder: (_) => [
                    for (final order in RadioOrder.values)
                      PopupMenuItem(
                        key: Key('radio_order_${order.apiValue}'),
                        value: order,
                        child: Text(order.label),
                      ),
                  ],
                  child: Chip(
                    avatar: const Icon(Icons.sort, size: 18),
                    label: Text('Sort: ${_query.order.label}'),
                  ),
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
            child: FilledButton.tonalIcon(
              key: const Key('radio_auto_explore_button'),
              style: FilledButton.styleFrom(
                  minimumSize: const Size.fromHeight(48)),
              icon: const Icon(Icons.shuffle),
              label: Text(radio.exploring
                  ? 'Restart auto explore with these filters'
                  : 'Auto explore'),
              onPressed: () => radio.startExplore(_query),
            ),
          ),
          if (_error != null)
            _Message(
              key: const Key('radio_explore_error'),
              icon: Icons.cloud_off,
              text: _error!,
              actionLabel: 'Retry',
              onAction: _load,
            )
          else if (!_loading && _stations.isEmpty)
            const _Message(
              key: Key('radio_explore_empty'),
              icon: Icons.radio,
              text: 'No stations match these filters.',
            ),
          for (final station in _stations) StationTile(station: station),
          if (_loading)
            const Padding(
              padding: EdgeInsets.all(24),
              child: Center(child: CircularProgressIndicator()),
            )
          else if (_hasMore && _error == null)
            Padding(
              padding: const EdgeInsets.all(16),
              child: OutlinedButton(
                key: const Key('radio_load_more'),
                onPressed: () => _load(more: true),
                child: const Text('Load more stations'),
              ),
            ),
        ],
      ),
    );
  }
}

class _FilterChip extends StatelessWidget {
  const _FilterChip({
    super.key,
    required this.icon,
    required this.label,
    required this.active,
    required this.onPressed,
    required this.onClear,
  });

  final IconData icon;
  final String label;
  final bool active;
  final VoidCallback onPressed;
  final VoidCallback onClear;

  @override
  Widget build(BuildContext context) {
    if (!active) {
      return ActionChip(
        avatar: Icon(icon, size: 18),
        label: Text(label),
        onPressed: onPressed,
      );
    }
    return InputChip(
      avatar: Icon(icon, size: 18),
      label: Text(label),
      selected: true,
      onPressed: onPressed,
      onDeleted: onClear,
      deleteButtonTooltipMessage: 'Clear filter',
    );
  }
}

// -----------------------------------------------------------------------------
// Bookmarks
// -----------------------------------------------------------------------------

class RadioBookmarksTab extends ConsumerStatefulWidget {
  const RadioBookmarksTab({super.key});

  @override
  ConsumerState<RadioBookmarksTab> createState() => _RadioBookmarksTabState();
}

class _RadioBookmarksTabState extends ConsumerState<RadioBookmarksTab>
    with AutomaticKeepAliveClientMixin {
  List<RadioStation>? _stations;
  String? _error;
  int _generation = 0;
  int? _loadedRevision;

  @override
  bool get wantKeepAlive => true;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  Future<void> _load() async {
    if (!mounted) return;
    final generation = ++_generation;
    final radio = ref.read(radioControllerProvider);
    _loadedRevision = radio.bookmarkRevision;
    setState(() => _error = null);
    try {
      final stations = await ref.read(apiClientProvider).listRadioBookmarks();
      if (!mounted || generation != _generation) return;
      radio.rememberBookmarks(stations, complete: true);
      setState(() => _stations = stations);
    } catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() => _error = radioErrorMessage(error));
    }
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);
    final radio = ref.watch(radioControllerProvider);
    if (_loadedRevision != null && radio.bookmarkRevision != _loadedRevision) {
      // A station was bookmarked or removed elsewhere: show the new list.
      _loadedRevision = radio.bookmarkRevision;
      WidgetsBinding.instance.addPostFrameCallback((_) => _load());
    }
    final stations = _stations;
    final Widget content;
    if (_error != null && stations == null) {
      content = _Message(
        key: const Key('radio_bookmarks_error'),
        icon: Icons.cloud_off,
        text: _error!,
        actionLabel: 'Retry',
        onAction: _load,
      );
    } else if (stations == null) {
      content = const Padding(
        padding: EdgeInsets.all(24),
        child: Center(child: CircularProgressIndicator()),
      );
    } else if (stations.isEmpty) {
      content = const _Message(
        key: Key('radio_bookmarks_empty'),
        icon: Icons.bookmark_border,
        text: 'No bookmarked stations yet. Tap the bookmark button of a '
            'station to keep it here.',
      );
    } else {
      content = Column(
        children: [for (final s in stations) StationTile(station: s)],
      );
    }
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        key: const Key('radio_bookmarks_list'),
        children: [content],
      ),
    );
  }
}

// -----------------------------------------------------------------------------
// Station tile and now-playing bar
// -----------------------------------------------------------------------------

/// One station: tap plays it, the trailing button toggles its bookmark.
class StationTile extends ConsumerWidget {
  const StationTile({super.key, required this.station});

  final RadioStation station;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final radio = ref.watch(radioControllerProvider);
    final onAir = radio.station?.uuid == station.uuid;
    return ListTile(
      key: Key('radio_station_${station.uuid}'),
      minVerticalPadding: 10,
      selected: onAir,
      leading: StationIcon(station: station),
      title: Text(station.name, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: station.summary.isEmpty
          ? null
          : Text(station.summary, maxLines: 2, overflow: TextOverflow.ellipsis),
      trailing: BookmarkStationButton(station: station),
      onTap: () => radio.play(station),
    );
  }
}

/// Toggles the bookmark of [station]; shows a snack bar when that fails.
class BookmarkStationButton extends ConsumerWidget {
  const BookmarkStationButton({super.key, required this.station});

  final RadioStation station;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final radio = ref.watch(radioControllerProvider);
    final bookmarked = radio.isBookmarked(station);
    return IconButton(
      key: Key('radio_bookmark_${station.uuid}'),
      tooltip: bookmarked ? 'Remove bookmark' : 'Bookmark station',
      isSelected: bookmarked,
      icon: const Icon(Icons.bookmark_border),
      selectedIcon: const Icon(Icons.bookmark),
      onPressed: radio.isBookmarkPending(station)
          ? null
          : () async {
              final messenger = ScaffoldMessenger.maybeOf(context);
              try {
                await radio.toggleBookmark(station);
              } catch (error) {
                messenger?.showSnackBar(
                    SnackBar(content: Text(radioErrorMessage(error))));
              }
            },
    );
  }
}

/// The station's favicon (relayed by the server), or a radio icon.
class StationIcon extends ConsumerWidget {
  const StationIcon({super.key, required this.station, this.size = 44});

  final RadioStation station;
  final double size;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final fallback = SizedBox(
      width: size,
      height: size,
      child: Icon(Icons.radio, size: size * 0.6),
    );
    if (station.faviconUrl.isEmpty) return fallback;
    return ClipRRect(
      borderRadius: BorderRadius.circular(6),
      child: AuthenticatedNetworkImage(
        imageUrl: ref.read(apiClientProvider).radioUrl(station.faviconUrl),
        width: size,
        height: size,
        fit: BoxFit.cover,
        placeholder: (_, __) => fallback,
        errorWidget: (_, __, ___) => fallback,
      ),
    );
  }
}

/// The station on air with its controls, and the auto-explore controls
/// while exploring. Hidden when nothing plays.
class RadioNowPlayingBar extends ConsumerWidget {
  const RadioNowPlayingBar({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final radio = ref.watch(radioControllerProvider);
    final station = radio.station;
    final theme = Theme.of(context);
    if (station == null) {
      final message = radio.exploreMessage;
      if (message == null) return const SizedBox.shrink();
      return Material(
        elevation: 8,
        child: SafeArea(
          top: false,
          child: ListTile(
            key: const Key('radio_explore_message'),
            leading: const Icon(Icons.info_outline),
            title: Text(message),
          ),
        ),
      );
    }
    final statusText = switch (radio.status) {
      RadioStatus.loading => 'Connecting…',
      RadioStatus.playing => 'Live',
      RadioStatus.paused => 'Paused',
      RadioStatus.error => radio.error ?? 'Cannot play this station.',
      RadioStatus.idle => '',
    };
    return Material(
      key: const Key('radio_now_playing'),
      elevation: 8,
      color: theme.colorScheme.surfaceContainer,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(12, 8, 4, 8),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Row(
                children: [
                  StationIcon(station: station, size: 40),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(station.name,
                            key: const Key('radio_now_playing_name'),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: theme.textTheme.titleMedium),
                        Text(statusText,
                            key: const Key('radio_now_playing_status'),
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: theme.textTheme.bodySmall?.copyWith(
                                color: radio.status == RadioStatus.error
                                    ? theme.colorScheme.error
                                    : null)),
                      ],
                    ),
                  ),
                  if (radio.status == RadioStatus.loading)
                    const Padding(
                      padding: EdgeInsets.all(12),
                      child: SizedBox(
                          width: 24,
                          height: 24,
                          child: CircularProgressIndicator(strokeWidth: 2)),
                    )
                  else
                    IconButton(
                      key: const Key('radio_play_pause'),
                      iconSize: 32,
                      tooltip: switch (radio.status) {
                        RadioStatus.playing => 'Pause',
                        RadioStatus.error => 'Retry',
                        _ => 'Play',
                      },
                      icon: Icon(switch (radio.status) {
                        RadioStatus.playing => Icons.pause_circle_filled,
                        RadioStatus.error => Icons.refresh,
                        _ => Icons.play_circle_filled,
                      }),
                      onPressed: radio.togglePause,
                    ),
                  BookmarkStationButton(station: station),
                  IconButton(
                    key: const Key('radio_stop'),
                    tooltip: 'Stop radio',
                    icon: const Icon(Icons.stop),
                    onPressed: radio.stop,
                  ),
                ],
              ),
              if (radio.exploring) const _ExploreControls(),
            ],
          ),
        ),
      ),
    );
  }
}

class _ExploreControls extends ConsumerWidget {
  const _ExploreControls();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final radio = ref.watch(radioControllerProvider);
    final countdown = radio.status == RadioStatus.playing
        ? 'Next station in ${radio.secondsLeft} s'
        : 'Tuning in…';
    return Padding(
      padding: const EdgeInsets.only(top: 8, right: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              const Icon(Icons.shuffle, size: 18),
              const SizedBox(width: 8),
              Expanded(
                child: Text('Auto explore · $countdown',
                    key: const Key('radio_explore_countdown')),
              ),
              SegmentedButton<Duration>(
                key: const Key('radio_explore_dwell'),
                showSelectedIcon: false,
                style: const ButtonStyle(visualDensity: VisualDensity.compact),
                segments: [
                  for (final d in radioExploreDwellChoices)
                    ButtonSegment(
                      value: d,
                      label: Text('${d.inSeconds}s',
                          key: Key('radio_dwell_${d.inSeconds}')),
                      tooltip: 'Stay ${d.inSeconds} seconds per station',
                    ),
                ],
                selected: {radio.dwell},
                onSelectionChanged: (s) => radio.setDwell(s.first),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              Expanded(
                child: FilledButton.icon(
                  key: const Key('radio_explore_keep'),
                  icon: const Icon(Icons.check),
                  label: const Text('Keep'),
                  onPressed: radio.keep,
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: OutlinedButton.icon(
                  key: const Key('radio_explore_next'),
                  icon: const Icon(Icons.skip_next),
                  label: const Text('Next'),
                  onPressed: radio.exploreNext,
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: OutlinedButton.icon(
                  key: const Key('radio_explore_stop'),
                  icon: const Icon(Icons.stop),
                  label: const Text('Stop'),
                  onPressed: radio.stopExplore,
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

class _Message extends StatelessWidget {
  const _Message({
    super.key,
    required this.icon,
    required this.text,
    this.actionLabel,
    this.onAction,
  });

  final IconData icon;
  final String text;
  final String? actionLabel;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        children: [
          Icon(icon, size: 48),
          const SizedBox(height: 12),
          Text(text, textAlign: TextAlign.center),
          if (actionLabel != null) ...[
            const SizedBox(height: 12),
            FilledButton(onPressed: onAction, child: Text(actionLabel!)),
          ],
        ],
      ),
    );
  }
}

class _Option {
  const _Option(this.value, this.label, this.count);

  final String value;
  final String label;
  final int count;
}

/// The user's choice in a [_PickerSheet]; [value] null means "any".
class _Pick {
  const _Pick(this.value, this.label);

  final String? value;
  final String? label;
}

/// Bottom sheet listing genres or countries with a filter field.
class _PickerSheet extends StatefulWidget {
  const _PickerSheet({
    required this.title,
    required this.searchHint,
    required this.load,
  });

  final String title;
  final String searchHint;
  final Future<List<_Option>> Function() load;

  @override
  State<_PickerSheet> createState() => _PickerSheetState();
}

class _PickerSheetState extends State<_PickerSheet> {
  late Future<List<_Option>> _options = widget.load();
  String _filter = '';

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: SizedBox(
        height: MediaQuery.sizeOf(context).height * 0.75,
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
              child: TextField(
                key: const Key('radio_picker_filter'),
                autofocus: false,
                decoration: InputDecoration(
                  labelText: widget.searchHint,
                  prefixIcon: const Icon(Icons.search),
                  border: const OutlineInputBorder(),
                ),
                onChanged: (text) =>
                    setState(() => _filter = text.trim().toLowerCase()),
              ),
            ),
            Expanded(
              child: FutureBuilder<List<_Option>>(
                future: _options,
                builder: (context, snapshot) {
                  if (snapshot.hasError) {
                    return _Message(
                      icon: Icons.cloud_off,
                      text: radioErrorMessage(snapshot.error!),
                      actionLabel: 'Retry',
                      onAction: () => setState(() => _options = widget.load()),
                    );
                  }
                  final options = snapshot.data;
                  if (options == null) {
                    return const Center(child: CircularProgressIndicator());
                  }
                  final shown = options
                      .where((o) => o.label.toLowerCase().contains(_filter))
                      .toList();
                  return ListView(
                    key: const Key('radio_picker_list'),
                    children: [
                      ListTile(
                        key: const Key('radio_picker_any'),
                        leading: const Icon(Icons.all_inclusive),
                        title: Text('Any ${widget.title.toLowerCase()}'),
                        onTap: () =>
                            Navigator.of(context).pop(const _Pick(null, null)),
                      ),
                      for (final o in shown)
                        ListTile(
                          key: Key('radio_picker_${o.value}'),
                          title: Text(o.label),
                          trailing: o.count > 0 ? Text('${o.count}') : null,
                          onTap: () => Navigator.of(context)
                              .pop(_Pick(o.value, o.label)),
                        ),
                    ],
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}
