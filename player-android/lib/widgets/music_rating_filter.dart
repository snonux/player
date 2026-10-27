import '../l10n/music_localizations.dart';
import 'package:flutter/material.dart';
import 'overflow_menu.dart';

/// Secondary rating filters keep hearts and metadata filters independent.
class MusicRatingFilter extends StatelessWidget {
  const MusicRatingFilter(
      {super.key,
      required this.rated,
      required this.highest,
      required this.onChanged});
  final bool rated, highest;
  final void Function(bool rated, bool highest) onChanged;
  @override
  Widget build(BuildContext context) => PopupMenuButton<String>(
      tooltip: 'Rating filters and order',
      icon: const Icon(Icons.star_half),
      onSelected: (value) => onChanged(value == 'rated' ? !rated : rated,
          value == 'highest' ? !highest : highest),
      itemBuilder: (_) => [
            CheckedPopupMenuItem(
                value: 'rated',
                checked: rated,
                child: Text(context.tr('Rated only'))),
            CheckedPopupMenuItem(
                value: 'highest',
                checked: highest,
                child: const Text('Highest rated first')),
          ]);
}

/// The same rating filters as rows of a screen's [OverflowMenuButton].
List<Widget> musicRatingMenuItems(BuildContext context,
        {required bool rated,
        required bool highest,
        required void Function(bool rated, bool highest) onChanged}) =>
    [
      OverflowMenuItem(
          key: const Key('music_rated_only'),
          icon: Icons.star_half,
          label: context.tr('Rated only'),
          selected: rated,
          onSelected: () => onChanged(!rated, highest)),
      OverflowMenuItem(
          key: const Key('music_highest_rated'),
          icon: Icons.star,
          label: 'Highest rated first',
          selected: highest,
          onSelected: () => onChanged(rated, !highest)),
    ];

List<Map<String, dynamic>> ratingFiltered(
    List<Map<String, dynamic>> items, bool rated, bool highest) {
  final result = items
      .where((item) => !rated || (item['rating'] as int? ?? 0) > 0)
      .toList();
  if (highest) {
    result.sort((a, b) =>
        (b['rating'] as int? ?? 0).compareTo(a['rating'] as int? ?? 0));
  }
  return result;
}
