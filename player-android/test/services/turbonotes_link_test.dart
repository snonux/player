import 'package:dio/dio.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/models/media_set.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/turbonotes_link.dart';

Media _media(int setId, String relPath) => Media(
    id: 7,
    setId: setId,
    relPath: relPath,
    fileName: relPath.split('/').last,
    absPath: '/media/$relPath',
    type: 'audio',
    duration: 60,
    codec: 'mp3',
    resolution: '',
    bitrate: 0,
    fileSizeBytes: 0,
    width: 0,
    height: 0,
    thumbnailPath: '',
    playCount: 0);

class _Client extends PlayerApiClient {
  _Client(this.media) : super(dio: Dio());
  final Media media;

  @override
  Future<Media> getMedia(int mediaId, {CancelToken? cancelToken}) async =>
      media;

  @override
  Future<List<MediaSet>> listSets() async => const [
        MediaSet(
            id: 1,
            name: 'Audiobooks',
            rootPath: 'audiobooks',
            coverThumbnailPath: '',
            isPodcast: false),
        MediaSet(
            id: 2,
            name: 'Go Time',
            rootPath: 'podcasts/go-time',
            coverThumbnailPath: '',
            isPodcast: true),
        MediaSet(
            id: 3,
            name: 'Music',
            rootPath: 'music',
            coverThumbnailPath: '',
            isPodcast: false),
      ];
}

ServerPlaybackRequest _server(String title) => ServerPlaybackRequest(
    mediaId: 7,
    serverOrigin: 'http://player',
    userId: 1,
    sourceUri: Uri.parse('http://player/api/v1/media/7/stream'),
    title: title);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('titles become safe file names', () {
    expect(noteFileName('Episode 12: Rust/Go?'), 'Episode 12_ Rust_Go_');
    expect(noteFileName('  ..Hidden  title. '), 'Hidden title');
    expect(noteFileName('a\nb\tc'), 'a_b_c');
    expect(noteFileName('...'), 'Untitled');
    expect(noteFileName('x' * 200).length, 120);
  });

  test('the note path is in Book-And-Podcast-Notes', () {
    expect(const NoteSubject(title: 'Dune: Part 1', podcast: false).notePath,
        'Book-And-Podcast-Notes/Dune_ Part 1.md');
  });

  test('audiobook titles come from the folder or the m4b file', () {
    expect(audiobookTitle('aesops-fables/10-chapter.mp3'), 'aesops-fables');
    expect(audiobookTitle('Frank Herbert/Dune/01.mp3'), 'Dune');
    expect(audiobookTitle('Frank Herbert/Dune.m4b'), 'Dune');
    expect(audiobookTitle('Audiobooks/Dune.mp3'), 'Dune');
    expect(audiobookTitle('Dune.mp3'), 'Dune');
    expect(audiobookTitle('', fallback: 'x'), 'x');
  });

  test('server media: podcasts, audiobooks, and nothing for music', () async {
    final episode = await noteSubjectFor(
        _server('Go Time 300: Generics'), _Client(_media(2, 'ep300.mp3')));
    expect(episode!.podcast, isTrue);
    expect(episode.title, 'Go Time 300: Generics');

    final book = await noteSubjectFor(_server('10-chapter.mp3'),
        _Client(_media(1, 'aesops-fables/10-chapter.mp3')));
    expect(book!.podcast, isFalse);
    expect(book.title, 'aesops-fables');

    final m4b = await noteSubjectFor(
        _server('Dune.m4b'), _Client(_media(3, 'misc/Dune.m4b')));
    expect(m4b!.title, 'Dune');

    expect(
        await noteSubjectFor(
            _server('Song'), _Client(_media(3, 'album/song.mp3'))),
        isNull);
  });

  test('device files go by name; public shares get no note', () async {
    final client = _Client(_media(3, 'x.mp3'));
    final book = await noteSubjectFor(
        LocalPlaybackRequest(
            localMediaId: 1,
            sourceUri: Uri.parse('content://docs/Audiobooks%2FDune.m4b'),
            title: 'Dune.m4b'),
        client);
    expect(book!.title, 'Dune');
    expect(
        await noteSubjectFor(
            LocalPlaybackRequest(
                localMediaId: 2,
                sourceUri: Uri.parse('content://docs/song.mp3'),
                title: 'song.mp3'),
            client),
        isNull);
    expect(
        await noteSubjectFor(
            PublicSharePlaybackRequest(
                shareToken: 't',
                sourceUri: Uri.parse('http://player/s/t/stream'),
                title: 'Dune.m4b'),
            client),
        isNull);
  });

  test('the launcher sends path and title; a missing app is reported',
      () async {
    const channel = MethodChannel('test/turbonotes');
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    final calls = <MethodCall>[];
    var installed = true;
    messenger.setMockMethodCallHandler(channel, (call) async {
      calls.add(call);
      if (!installed) throw PlatformException(code: 'not_installed');
      return null;
    });
    addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
    const launcher = TurboNotesLauncher(channel: channel);
    const subject = NoteSubject(title: 'Dune', podcast: false);
    await launcher.open(subject);
    expect(calls.single.method, 'openNote');
    expect(calls.single.arguments,
        {'path': 'Book-And-Podcast-Notes/Dune.md', 'title': 'Dune'});
    installed = false;
    await expectLater(
        launcher.open(subject), throwsA(isA<TurboNotesMissingException>()));
  });
}
