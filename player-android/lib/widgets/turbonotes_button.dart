import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/player_api_client.dart';
import '../providers/api_client_provider.dart';
import '../services/playback_request.dart';
import '../services/turbonotes_link.dart';

/// Opens notes in TurboNotes; replaced in tests.
final turboNotesLauncherProvider =
    Provider<TurboNotesLauncher>((ref) => const TurboNotesLauncher());

/// Works out what [PlaybackRequest] a note would be about; replaced in tests.
final noteSubjectResolverProvider =
    Provider<Future<NoteSubject?> Function(PlaybackRequest, PlayerApiClient)>(
  (ref) => noteSubjectFor,
);

/// The player's "Note in TurboNotes" button: shown for audiobooks and
/// podcast episodes, it opens `Book-And-Podcast-Notes/TITLE.md` in
/// TurboNotes, which creates the note on first use.
class TurboNotesButton extends ConsumerStatefulWidget {
  const TurboNotesButton({super.key, required this.request});
  final PlaybackRequest request;

  @override
  ConsumerState<TurboNotesButton> createState() => _TurboNotesButtonState();
}

class _TurboNotesButtonState extends ConsumerState<TurboNotesButton> {
  NoteSubject? _subject;
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    _resolve();
  }

  @override
  void didUpdateWidget(covariant TurboNotesButton oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.request.identity != widget.request.identity ||
        oldWidget.request.title != widget.request.title) {
      _subject = null;
      _resolve();
    }
  }

  Future<void> _resolve() async {
    final generation = ++_generation;
    NoteSubject? subject;
    try {
      subject = await ref.read(noteSubjectResolverProvider)(
          widget.request, ref.read(apiClientProvider));
    } catch (_) {
      // Lookup failed (offline, older server): no button, playback goes on.
      subject = null;
    }
    if (mounted && generation == _generation) {
      setState(() => _subject = subject);
    }
  }

  Future<void> _open(NoteSubject subject) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(turboNotesLauncherProvider).open(subject);
    } on TurboNotesMissingException {
      messenger.showSnackBar(const SnackBar(
          content: Text('Install TurboNotes to keep notes on '
              'audiobooks and podcasts.')));
    } catch (_) {
      messenger.showSnackBar(const SnackBar(
          content: Text('Could not open TurboNotes. Please try again.')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final subject = _subject;
    if (subject == null) return const SizedBox.shrink();
    return IconButton(
      key: const Key('turbonotes_button'),
      tooltip: subject.podcast
          ? 'Episode note in TurboNotes'
          : 'Book note in TurboNotes',
      icon: const Icon(Icons.edit_note),
      onPressed: () => _open(subject),
    );
  }
}
