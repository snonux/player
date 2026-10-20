import 'package:flutter/services.dart';

import '../api/player_api_client.dart';
import 'audio_visuals.dart' show isAudiobook;
import 'playback_request.dart';

/// The TurboNotes folder that collects notes on audiobooks and podcasts.
const kTurboNotesFolder = 'Book-And-Podcast-Notes';

/// What a note in TurboNotes is about: an audiobook or a podcast episode.
class NoteSubject {
  const NoteSubject({required this.title, required this.podcast});

  /// The book's or the episode's title, the new note's heading.
  final String title;
  final bool podcast;

  /// The note's path in the TurboNotes folder,
  /// `Book-And-Podcast-Notes/TITLE.md`.
  String get notePath => '$kTurboNotesFolder/${noteFileName(title)}.md';
}

/// [title] as a file name that works on Android, Linux, Windows and in
/// sync tools: no path separators or reserved characters, no leading dots
/// (TurboNotes hides dot files) and at most 120 characters.
String noteFileName(String title) {
  var name = title
      .replaceAll(RegExp(r'[\x00-\x1f\x7f/\\:*?"<>|]'), '_')
      .replaceAll(RegExp(r'\s+'), ' ')
      .trim();
  name = name.replaceFirst(RegExp(r'^[.\s]+'), '');
  if (name.length > 120) name = name.substring(0, 120).trimRight();
  name = name.replaceFirst(RegExp(r'[.\s]+$'), '');
  return name.isEmpty ? 'Untitled' : name;
}

/// The book an audiobook file belongs to: a single `.m4b` file is the book;
/// chapters such as `Dune/01.mp3` are named after their folder.
String audiobookTitle(String relPath, {String fallback = ''}) {
  final segments =
      relPath.split(RegExp(r'[/\\]')).where((s) => s.isNotEmpty).toList();
  if (segments.isEmpty) return fallback;
  final file = segments.last;
  final stem =
      file.contains('.') ? file.substring(0, file.lastIndexOf('.')) : file;
  if (file.toLowerCase().endsWith('.m4b') || segments.length < 2) {
    return stem.isEmpty ? fallback : stem;
  }
  final folder = segments[segments.length - 2];
  // Chapters right in an "Audiobooks" folder have no book folder.
  return isAudiobook(folder) ? stem : folder;
}

/// The note subject of what [request] plays, or null for anything that is
/// neither an audiobook nor a podcast episode (music, public shares).
///
/// Server media is looked up: episodes of podcast sets are podcasts, and
/// the set, folder and file names tell audiobooks apart like the visuals
/// do. Files on the device only have their name to go by.
Future<NoteSubject?> noteSubjectFor(
  PlaybackRequest request,
  PlayerApiClient client,
) async {
  switch (request) {
    case ServerPlaybackRequest():
      final media = await client.getMedia(request.mediaId);
      final set = (await client.listSets())
          .where((set) => set.id == media.setId)
          .firstOrNull;
      if (set?.isPodcast ?? false) {
        return NoteSubject(title: request.title, podcast: true);
      }
      final names = '${set?.name ?? ''} ${set?.rootPath ?? ''} '
          '${media.relPath} ${media.fileName}';
      if (!isAudiobook(names)) return null;
      final relPath = media.relPath.isEmpty ? media.fileName : media.relPath;
      return NoteSubject(
        title: audiobookTitle(relPath, fallback: request.title),
        podcast: false,
      );
    case LocalPlaybackRequest() || OfflinePlaybackRequest():
      final name = '${request.title} ${Uri.decodeFull(request.sourceUri.path)}';
      if (!isAudiobook(name)) return null;
      return NoteSubject(
        title: audiobookTitle(request.title, fallback: request.title),
        podcast: false,
      );
    case PublicSharePlaybackRequest():
      return null;
  }
}

/// Thrown when TurboNotes is not installed.
class TurboNotesMissingException implements Exception {
  const TurboNotesMissingException();
  @override
  String toString() => 'TurboNotes is not installed.';
}

/// Opens a note in the TurboNotes app (org.buetow.turbonotes), which
/// creates it when it does not exist yet. Native half: `TurboNotes.kt`.
class TurboNotesLauncher {
  const TurboNotesLauncher({MethodChannel? channel}) : _channel = channel;

  static const _defaultChannel =
      MethodChannel('zone.foo.player_android/turbonotes');
  final MethodChannel? _channel;

  Future<void> open(NoteSubject subject) async {
    try {
      await (_channel ?? _defaultChannel).invokeMethod<void>('openNote', {
        'path': subject.notePath,
        'title': subject.title,
      });
    } on PlatformException catch (e) {
      if (e.code == 'not_installed') throw const TurboNotesMissingException();
      rethrow;
    }
  }
}
