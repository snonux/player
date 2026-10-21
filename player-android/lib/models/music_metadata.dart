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
  Map<String, String> get details {
    String number(String field) {
      final n = _number(field), total = _number('${field}_total');
      return n == 0 ? '' : '$n${total > 0 ? ' / $total' : ''}';
    }

    return {
      'Artist': artist,
      'Album artist': _text('album_artist'),
      'Album': album,
      'Track': number('track'),
      'Disc': number('disc'),
      'Date': _text('date'),
      'Year': _number('year') > 0 ? '${_number('year')}' : '',
      'Genres': _list('genres').join(', ')
    }..removeWhere((key, value) => value.isEmpty);
  }
}
