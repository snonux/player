import 'package:flutter/material.dart';

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
                child: const Text('Rated only')),
            CheckedPopupMenuItem(
                value: 'highest',
                checked: highest,
                child: const Text('Highest rated first')),
          ]);
}

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
