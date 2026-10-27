import 'package:flutter/foundation.dart';

import 'json_helpers.dart';

/// Ordering of the radio station explorer (`order` query parameter of
/// `GET /api/v1/radio/stations`).
enum RadioOrder {
  popular('popular', 'Popular'),
  votes('votes', 'Most votes'),
  name('name', 'Name'),
  random('random', 'Random');

  const RadioOrder(this.apiValue, this.label);

  final String apiValue;
  final String label;
}

/// An internet radio station as the server describes it.
///
/// Station data comes from the Radio Browser directory through the server.
/// [streamUrl] and [faviconUrl] are server-relative paths: the server relays
/// both, so the app never contacts station hosts directly and cleartext
/// stations work as well.
@immutable
class RadioStation {
  const RadioStation({
    required this.uuid,
    required this.name,
    required this.streamUrl,
    this.homepage = '',
    this.faviconUrl = '',
    this.country = '',
    this.countryCode = '',
    this.language = '',
    this.tags = const [],
    this.codec = '',
    this.bitrate = 0,
    this.votes = 0,
    this.bookmarked = false,
    this.bookmarkedAt,
  });

  final String uuid;
  final String name;
  final String homepage;

  /// Server-relative favicon path, empty when the station has none.
  final String faviconUrl;

  /// Server-relative path of the relayed live stream.
  final String streamUrl;
  final String country;
  final String countryCode;
  final String language;
  final List<String> tags;
  final String codec;

  /// kbit/s, 0 when unknown.
  final int bitrate;
  final int votes;
  final bool bookmarked;
  final DateTime? bookmarkedAt;

  factory RadioStation.fromJson(Map<String, dynamic> json) {
    final uuid = json['uuid'];
    if (uuid is! String || uuid.isEmpty) {
      throw const FormatException('radio station without uuid');
    }
    final tags = json['tags'];
    return RadioStation(
      uuid: uuid,
      name: (json['name'] as String?)?.trim().isNotEmpty == true
          ? (json['name'] as String).trim()
          : 'Unnamed station',
      homepage: json['homepage'] as String? ?? '',
      faviconUrl: json['favicon_url'] as String? ?? '',
      streamUrl: json['stream_url'] as String? ??
          '/api/radio/stations/${Uri.encodeComponent(uuid)}/stream',
      country: json['country'] as String? ?? '',
      countryCode: json['country_code'] as String? ?? '',
      language: json['language'] as String? ?? '',
      tags: tags is List ? tags.whereType<String>().toList() : const [],
      codec: json['codec'] as String? ?? '',
      bitrate: (json['bitrate'] as num?)?.toInt() ?? 0,
      votes: (json['votes'] as num?)?.toInt() ?? 0,
      bookmarked: json['bookmarked'] as bool? ?? false,
      bookmarkedAt: dateTimeFromJson(json['bookmarked_at']),
    );
  }

  RadioStation copyWith({bool? bookmarked}) => RadioStation(
        uuid: uuid,
        name: name,
        streamUrl: streamUrl,
        homepage: homepage,
        faviconUrl: faviconUrl,
        country: country,
        countryCode: countryCode,
        language: language,
        tags: tags,
        codec: codec,
        bitrate: bitrate,
        votes: votes,
        bookmarked: bookmarked ?? this.bookmarked,
        bookmarkedAt: bookmarkedAt,
      );

  /// One line describing the station: country, format, and genres.
  String get summary {
    final format = [
      if (codec.isNotEmpty) codec,
      if (bitrate > 0) '$bitrate kbps',
    ].join(' ');
    return [
      if (country.isNotEmpty) country,
      if (format.isNotEmpty) format,
      if (tags.isNotEmpty) tags.take(3).join(', '),
    ].join(' · ');
  }
}

/// A genre tag with the number of stations carrying it.
@immutable
class RadioTag {
  const RadioTag({required this.name, this.stationCount = 0});

  final String name;
  final int stationCount;

  factory RadioTag.fromJson(Map<String, dynamic> json) => RadioTag(
        name: json['name'] as String? ?? '',
        stationCount: (json['station_count'] as num?)?.toInt() ?? 0,
      );
}

/// A country (ISO 3166-1 alpha-2 [code]) with its number of stations.
@immutable
class RadioCountry {
  const RadioCountry({
    required this.code,
    required this.name,
    this.stationCount = 0,
  });

  final String code;
  final String name;
  final int stationCount;

  factory RadioCountry.fromJson(Map<String, dynamic> json) => RadioCountry(
        code: json['code'] as String? ?? '',
        name: json['name'] as String? ?? '',
        stationCount: (json['station_count'] as num?)?.toInt() ?? 0,
      );
}

/// Filter of the station explorer; also the pool auto explore draws from.
@immutable
class RadioQuery {
  const RadioQuery({
    this.search = '',
    this.tag,
    this.country,
    this.order = RadioOrder.popular,
  });

  final String search;
  final String? tag;

  /// ISO 3166-1 alpha-2 code.
  final String? country;
  final RadioOrder order;

  RadioQuery copyWith({
    String? search,
    String? Function()? tag,
    String? Function()? country,
    RadioOrder? order,
  }) =>
      RadioQuery(
        search: search ?? this.search,
        tag: tag == null ? this.tag : tag(),
        country: country == null ? this.country : country(),
        order: order ?? this.order,
      );

  @override
  bool operator ==(Object other) =>
      other is RadioQuery &&
      other.search == search &&
      other.tag == tag &&
      other.country == country &&
      other.order == order;

  @override
  int get hashCode => Object.hash(search, tag, country, order);
}
