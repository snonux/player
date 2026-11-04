import 'package:flutter/material.dart';
import '../services/podcast_metadata.dart';

/// Parse long publisher HTML only when the listener expands its notes.
class PodcastShowNotes extends StatefulWidget {
  const PodcastShowNotes(
      {super.key, required this.description, this.initiallyExpanded = false});
  final String description;
  final bool initiallyExpanded;
  @override
  State<PodcastShowNotes> createState() => _PodcastShowNotesState();
}

class _PodcastShowNotesState extends State<PodcastShowNotes> {
  late bool expanded = widget.initiallyExpanded;
  String? text;
  @override
  void didUpdateWidget(covariant PodcastShowNotes oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.description != widget.description) text = null;
  }

  @override
  Widget build(BuildContext context) {
    if (widget.description.trim().isEmpty) return const SizedBox.shrink();
    if (expanded) text ??= podcastShowNotesText(widget.description);
    return ExpansionTile(
        initiallyExpanded: widget.initiallyExpanded,
        tilePadding: EdgeInsets.zero,
        childrenPadding: const EdgeInsets.only(bottom: 12),
        title: const Text('Show notes'),
        onExpansionChanged: (value) => setState(() => expanded = value),
        children: [
          if (expanded)
            Align(
                alignment: Alignment.centerLeft,
                child: SelectableText(
                    text!.isEmpty ? 'Show notes unavailable.' : text!))
        ]);
  }
}
