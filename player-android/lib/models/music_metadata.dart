/// Embedded tags carried with server media and its offline snapshot.
class MusicMetadata {
  final Map<String, dynamic> _json;
  const MusicMetadata._(this._json);
  factory MusicMetadata.fromJson(Map<String, dynamic> json) =>
      MusicMetadata._(Map.unmodifiable(json));
  String _text(String key) => _json[key] is String ? _json[key] as String : '';
  List<String> _list(String key) => _json[key] is List
      ? (_json[key] as List).whereType<String>().toList()
      : const [];
  int _number(String key) =>
      _json[key] is int && (_json[key] as int) > 0 ? _json[key] as int : 0;
  String get title => _text('title');
  String get artist => _text('artist').isNotEmpty
      ? _text('artist')
      : _list('artists').join(', ');
  String get album => _text('album');
  Map<String, dynamic> toJson() => Map.of(_json);
  String get releaseDate => _text('release_date').isNotEmpty
      ? _text('release_date')
      : _text('date').isNotEmpty
          ? _text('date')
          : _number('year') > 0
              ? '${_number('year')}'
              : '';
  bool get _showYear {
    final prefix = RegExp(r'^[1-9]\d{3}(?:\D|$)')
        .firstMatch(releaseDate)
        ?.group(0)
        ?.substring(0, 4);
    return _number('year') > 0 && '${_number('year')}' != prefix;
  }

  num? _audioNumber(String key, {bool nonnegative = false}) {
    final value = _json[key];
    if (value is! num || !value.isFinite || (nonnegative && value < 0)) {
      return null;
    }
    return value;
  }

  String _audioText(num value) => value == value.roundToDouble()
      ? value.toInt().toString()
      : value.toString();

  List<MapEntry<String, String>> get audioDetails {
    final rate = _number('sample_rate'), depth = _number('bit_depth');
    final channels = _number('channels');
    return [
      if (rate > 0)
        MapEntry('Sample rate', '${_audioText(rate / 1000)} kHz ($rate Hz)'),
      if (depth > 0) MapEntry('Bit depth', '$depth bits'),
      if (channels > 0)
        MapEntry(
            'Channels',
            '$channels${channels == 1 ? ' (mono)' : channels == 2 ? ' (stereo)' : ''}'),
      for (final entry in {
        'replaygain_track_gain_db': 'Track ReplayGain',
        'replaygain_album_gain_db': 'Album ReplayGain'
      }.entries)
        if (_audioNumber(entry.key) case final num value)
          MapEntry(entry.value, '${_audioText(value)} dB'),
      for (final entry in {
        'replaygain_track_peak': 'Track peak',
        'replaygain_album_peak': 'Album peak'
      }.entries)
        if (_audioNumber(entry.key, nonnegative: true) case final num value)
          MapEntry(entry.value, '${_audioText(value)} (1 = full scale)'),
    ];
  }

  Map<String, List<MapEntry<String, String>>> get detailGroups {
    const roles = {
      'conductor': 'Conductor',
      'lyricist': 'Lyricist',
      'arranger': 'Arranger',
      'producer': 'Producer',
      'director': 'Director',
      'engineer': 'Engineer',
      'mixer': 'Mixer',
      'remixer': 'Remixer',
      'djmixer': 'DJ mixer'
    };
    final groups = <String, List<MapEntry<String, String>>>{
      'Release details': [
        MapEntry('Release date', releaseDate),
        MapEntry('Original date', _text('original_date')),
        MapEntry('Recording date', _text('recording_date')),
        MapEntry('Version', _text('release_version')),
        MapEntry('Release type', _list('release_types').join('\n')),
        MapEntry('Label', _list('labels').join('\n')),
        MapEntry('Catalog number', _list('catalog_numbers').join('\n')),
        MapEntry('Media', _list('media').join('\n')),
      ],
      'Additional music details': [
        MapEntry('Mood', _list('moods').join('\n')),
        MapEntry('Grouping', _text('grouping')),
        MapEntry('Comment', _text('comment')),
        if (_json['credits'] is List)
          for (final credit in _json['credits'] as List)
            if (credit is Map &&
                credit['role'] is String &&
                credit['names'] is List)
              MapEntry(roles[credit['role']] ?? credit['role'] as String,
                  (credit['names'] as List).whereType<String>().join('\n')),
      ],
      'Advanced audio': audioDetails,
    };
    for (final rows in groups.values) {
      rows.removeWhere((entry) => entry.value.isEmpty);
    }
    groups.removeWhere((key, rows) => rows.isEmpty);
    return groups;
  }

  Map<String, String> get details {
    String number(String field) {
      final n = _number(field), total = _number('${field}_total');
      return n == 0 ? '' : '$n${total > 0 ? ' / $total' : ''}';
    }

    return {
      'Artist': artist,
      'Album artist': _text('album_artist'),
      'Composer': _list('composers').join(', '),
      'Performer': _list('performers').join(', '),
      'Album': album,
      'Disc subtitle': _text('disc_subtitle'),
      'Compilation': _json['compilation'] == true ? 'Yes' : '',
      'Track': number('track'),
      'Disc': number('disc'),
      'Tagged date': _text('date') != releaseDate ? _text('date') : '',
      'Year': _showYear ? '${_number('year')}' : '',
      'Genres': _list('genres').join(', '),
      if (_audioNumber('bpm', nonnegative: true) case final num bpm
          when bpm > 0)
        'Tempo': '${_audioText(bpm)} BPM (beats/min)'
    }..removeWhere((key, value) => value.isEmpty);
  }
}
