import 'dart:convert';
import 'package:crypto/crypto.dart';

String podcastTrim(String value) =>
    value.replaceAll(RegExp(r'^[ \t\r\n\f\v]+|[ \t\r\n\f\v]+$'), '');
String _asciiLower(String value) =>
    value.replaceAllMapped(RegExp('[A-Z]'), (m) => m[0]!.toLowerCase());

/// Byte-compatible identity rules shared with internal/podcast/identity.go.
String podcastLocation(String raw) {
  var value = podcastTrim(raw);
  final bytes = utf8.encode(value);
  if (bytes.isEmpty ||
      bytes.length > 2048 ||
      bytes.any((b) => b <= 32 || b == 127)) {
    throw const FormatException('Invalid feed URL');
  }
  String schemeOf(String text) {
    final m = RegExp(r'^([a-zA-Z][a-zA-Z0-9+.-]*):').firstMatch(text);
    if (m == null) throw const FormatException('Feed URL needs http or https');
    return m[1]!.toLowerCase();
  }

  var scheme = schemeOf(value);
  var rest = value.substring(value.indexOf(':') + 1);
  const pseudo = {'feed', 'itpc', 'pcast', 'podcast'};
  if (pseudo.contains(scheme)) {
    final inner = rest.startsWith('//') ? rest.substring(2) : rest;
    if (RegExp(r'^[a-zA-Z][a-zA-Z0-9+.-]*://').hasMatch(inner)) {
      scheme = schemeOf(inner);
      rest = inner.substring(inner.indexOf(':') + 1);
    } else {
      scheme = 'http';
    }
  }
  if ((scheme != 'http' && scheme != 'https') || !rest.startsWith('//')) {
    throw const FormatException('Invalid feed scheme');
  }
  rest = rest.substring(2);
  final end = rest.indexOf(RegExp(r'[/\?#]'));
  var host = end < 0 ? rest : rest.substring(0, end);
  var tail = end < 0 ? '' : rest.substring(end);
  if (host.contains('@') || host.contains(r'\')) {
    throw const FormatException('Invalid feed host');
  }
  String port = '';
  if (host.startsWith('[')) {
    final close = host.indexOf(']');
    if (close < 0) throw const FormatException('Invalid IPv6 host');
    final suffix = host.substring(close + 1);
    if (suffix.isNotEmpty && !suffix.startsWith(':')) {
      throw const FormatException('Invalid host');
    }
    port = suffix.isEmpty ? '' : suffix.substring(1);
    host = host.substring(0, close + 1);
  } else if (host.contains(':')) {
    port = host.substring(host.indexOf(':') + 1);
    host = host.substring(0, host.indexOf(':'));
  }
  host = _asciiLower(host);
  if (host.endsWith('.')) host = host.substring(0, host.length - 1);
  if (host.isEmpty || host == '[]') {
    throw const FormatException('Missing feed host');
  }
  if (port.isNotEmpty) {
    final n = int.tryParse(port);
    if (!RegExp(r'^\d+$').hasMatch(port) || n == null || n < 1 || n > 65535) {
      throw const FormatException('Invalid port');
    }
    if (!((scheme == 'http' && n == 80) || (scheme == 'https' && n == 443))) {
      host += ':$n';
    }
  }
  tail = tail.split('#').first;
  final q = tail.indexOf('?');
  var path = q < 0 ? tail : tail.substring(0, q);
  final query = q < 0 ? '' : tail.substring(q + 1);
  if (path.isEmpty) path = '/';
  String escape(String s) {
    final out = StringBuffer();
    for (final b in utf8.encode(s)) {
      out.write(b >= 128
          ? '%${b.toRadixString(16).toUpperCase().padLeft(2, '0')}'
          : String.fromCharCode(b));
    }
    return out.toString().replaceAllMapped(
        RegExp(r'%[0-9a-fA-F]{2}'), (m) => m[0]!.toUpperCase());
  }

  return host + escape(path) + (query.isEmpty ? '' : '?${escape(query)}');
}

String podcastFeedKey(String url) => 'feed:${podcastLocation(url)}';
String podcastEnclosureKey(String url) => 'enc:${podcastLocation(url)}';
String _enc(String url) {
  try {
    return podcastEnclosureKey(url);
  } on FormatException {
    return '';
  }
}

class PodcastIdentityItem {
  const PodcastIdentityItem(this.guid, this.enclosureUrl);
  final String guid, enclosureUrl;
}

List<String> podcastEpisodeKeys(List<PodcastIdentityItem> items) {
  final encs = items.map((i) => _enc(i.enclosureUrl)).toList();
  final counts = <String, int>{};
  for (var i = 0; i < items.length; i++) {
    if (encs[i].isNotEmpty) {
      final g = podcastTrim(items[i].guid);
      counts[g] = (counts[g] ?? 0) + 1;
    }
  }
  final keys = List.filled(items.length, '');
  final taken = <String>{};
  for (var i = 0; i < items.length; i++) {
    final g = podcastTrim(items[i].guid);
    if (encs[i].isNotEmpty && g.isNotEmpty && counts[g] == 1) {
      keys[i] =
          'guid:${utf8.encode(g).length > 1024 ? 'sha256:${sha256.convert(utf8.encode(g))}' : g}';
      taken.add(encs[i]);
    }
  }
  for (var i = 0; i < items.length; i++) {
    if (encs[i].isNotEmpty && keys[i].isEmpty && taken.add(encs[i])) {
      keys[i] = encs[i];
    }
  }
  return keys;
}

class PodcastKnownEpisode {
  const PodcastKnownEpisode(this.key, this.enclosureKey, this.aliases);
  final String key, enclosureKey;
  final List<String> aliases;
}

class PodcastIngestMatch {
  const PodcastIngestMatch(this.key, {this.isNew = false, this.alias = ''});
  final String key, alias;
  final bool isNew;
}

List<PodcastIngestMatch> podcastMatchEpisodes(
    List<PodcastKnownEpisode> known, List<PodcastIdentityItem> items) {
  final keys = podcastEpisodeKeys(items),
      encs = items.map((i) => _enc(i.enclosureUrl)).toList();
  final byKey = <String, int>{}, byEnc = <String, Set<int>>{};
  for (var i = 0; i < known.length; i++) {
    final e = known[i];
    for (final key in [e.key, ...e.aliases]) {
      byKey[key] = i;
    }
    for (final key in [e.key, e.enclosureKey, ...e.aliases]) {
      if (key.startsWith('enc:')) (byEnc[key] ??= {}).add(i);
    }
  }
  int encMatch(String enc) => byEnc[enc]?.length == 1 ? byEnc[enc]!.first : -1;
  final claimed = <int>{}, docKeys = keys.toSet();
  final aliases = known.map((e) => e.aliases.length).toList();
  final results = List.filled(items.length, const PodcastIngestMatch(''));
  void claim(int i, int x) {
    claimed.add(x);
    var alias = '';
    if (!byKey.containsKey(keys[i]) && aliases[x] < 16) {
      alias = keys[i];
      byKey[alias] = x;
      aliases[x]++;
    }
    results[i] = PodcastIngestMatch(known[x].key, alias: alias);
  }

  for (var i = 0; i < keys.length; i++) {
    if (keys[i].isEmpty) continue;
    var x = byKey[keys[i]] ?? -1;
    final y = encMatch(encs[i]);
    if (y >= 0 &&
        y != x &&
        known[y].key.startsWith('enc:') &&
        (x < 0 || known[x].enclosureKey != encs[i])) {
      x = y;
    }
    if (x >= 0 && !claimed.contains(x)) claim(i, x);
  }
  for (var i = 0; i < keys.length; i++) {
    if (keys[i].isEmpty || results[i].key.isNotEmpty) continue;
    final x = encMatch(encs[i]);
    if (x >= 0 && !claimed.contains(x) && !docKeys.contains(known[x].key)) {
      claim(i, x);
    }
  }
  var created = 0;
  for (var i = 0; i < keys.length; i++) {
    if (keys[i].isEmpty || results[i].key.isNotEmpty || created >= 1000) {
      continue;
    }
    final key = byKey.containsKey(keys[i]) ? encs[i] : keys[i];
    if (byKey.containsKey(key)) continue;
    byKey[key] = -1;
    results[i] = PodcastIngestMatch(key, isNew: true);
    created++;
  }
  return results;
}

String? podcastResolveEpisode(
    List<PodcastKnownEpisode> known, String key, String enclosureUrl) {
  int encMatch(String enc) {
    final found = <int>{};
    for (var i = 0; i < known.length; i++) {
      if ([known[i].key, known[i].enclosureKey, ...known[i].aliases]
          .contains(enc)) {
        found.add(i);
      }
    }
    return found.length == 1 ? found.first : -1;
  }

  var x = known.indexWhere((e) => e.key == key || e.aliases.contains(key));
  final enc = _enc(enclosureUrl);
  if (x < 0 && key.startsWith('enc:')) {
    x = encMatch(key);
    if (x < 0 && enc.isNotEmpty) x = encMatch(enc);
  }
  if (x < 0) return null;
  final y = enc.isEmpty ? -1 : encMatch(enc);
  if (y >= 0 &&
      y != x &&
      known[y].key.startsWith('enc:') &&
      known[x].enclosureKey != enc) {
    x = y;
  }
  return known[x].key;
}
