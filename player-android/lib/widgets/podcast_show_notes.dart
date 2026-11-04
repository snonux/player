import 'package:flutter/material.dart';
import '../services/podcast_metadata.dart';
import '../services/external_metadata_links.dart';

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

  Future<void> openLink(String url) async {
    try {
      await openMetadataLink(url);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content: Text(
                'Could not open link. Check that a browser is installed.')));
      }
    }
  }

  Widget notes() {
    final content = text!.isEmpty ? 'Show notes unavailable.' : text!;
    final parts = podcastShowNotesSegments(content);
    if (!parts.any((part) => part.url != null)) return SelectableText(content);
    return LayoutBuilder(
        builder: (context, constraints) => SelectionArea(
              child: Text.rich(TextSpan(children: [
                for (final part in parts)
                  if (part.url == null)
                    TextSpan(text: part.text)
                  else
                    WidgetSpan(
                        alignment: PlaceholderAlignment.middle,
                        child: ConstrainedBox(
                            constraints:
                                BoxConstraints(maxWidth: constraints.maxWidth),
                            child: TextButton(
                              onPressed: () => openLink(part.url!),
                              style: TextButton.styleFrom(
                                  minimumSize: const Size(48, 48),
                                  padding:
                                      const EdgeInsets.symmetric(horizontal: 4),
                                  textStyle: const TextStyle(
                                      decoration: TextDecoration.underline)),
                              child: Text(part.text, softWrap: true),
                            )))
              ])),
            ));
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
          if (expanded) Align(alignment: Alignment.centerLeft, child: notes())
        ]);
  }
}
