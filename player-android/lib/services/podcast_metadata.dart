import 'package:html/dom.dart';
import 'package:html/parser.dart' as html;

/// Publisher images use their own public client/cache, never Player credentials.
String podcastArtworkUrl(Object? value) {
  if (value is! String) return '';
  final uri = Uri.tryParse(value.trim());
  if (uri == null ||
      !['http', 'https'].contains(uri.scheme) ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty) {
    return '';
  }
  return uri.toString();
}

DateTime? podcastPublicationDate(Object? value) {
  if (value is! num || !value.isFinite || value <= 0) return null;
  try {
    return DateTime.fromMillisecondsSinceEpoch(value.toInt(), isUtc: true);
  } on ArgumentError {
    return null;
  }
}

/// Show publisher content as text, preserving paragraphs and copyable URLs.
/// No publisher HTML, JavaScript, embedded content or styling is executed.
String podcastShowNotesText(String source) {
  final fragment = html.parseFragment(source);
  final out = StringBuffer();
  const ignored = {
    'script',
    'style',
    'iframe',
    'object',
    'template',
    'noscript',
    'svg'
  };
  const blocks = {
    'p',
    'div',
    'section',
    'article',
    'blockquote',
    'h1',
    'h2',
    'h3',
    'h4',
    'h5',
    'h6',
    'ul',
    'ol',
    'li'
  };
  void visit(Node node) {
    if (node is Text) {
      out.write(node.data);
    } else if (node is Element) {
      final tag = node.localName;
      if (ignored.contains(tag)) return;
      if (tag == 'br') out.write('\n');
      if (blocks.contains(tag)) out.write('\n');
      if (tag == 'li') out.write('• ');
      for (final child in node.nodes) {
        visit(child);
      }
      if (tag == 'a') {
        final href = podcastArtworkUrl(node.attributes['href']);
        if (href.isNotEmpty && node.text.trim() != href) out.write(' ($href)');
      }
      if (blocks.contains(tag)) out.write('\n');
    }
  }

  for (final node in fragment.nodes) {
    visit(node);
  }
  return out
      .toString()
      .replaceAll('\u00a0', ' ')
      .replaceAll(RegExp(r'[\t ]+'), ' ')
      .replaceAll(RegExp(r' *\n *'), '\n')
      .replaceAll(RegExp(r'\n{3,}'), '\n\n')
      .trim();
}
