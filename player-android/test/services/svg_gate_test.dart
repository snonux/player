// Table-driven tests for the SVG allowlist gate (svg_gate.dart): what it
// lets through to the compiler and what it refuses, one sample per rule.

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/svg_document.dart';
import 'package:player_android/services/svg_gate.dart';

import '../support/svg_test_support.dart';

const _rect = '<rect width="10" height="10"/>';

/// A typical icon: one path with a view box.
const _icon = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" '
    'width="24" height="24"><title>Home</title>'
    '<path fill="currentColor" fill-rule="evenodd" '
    'd="M10 20v-6h4v6h5v-8h3L12 3 2 12h3v8z"/></svg>';

/// What Inkscape writes with "Plain SVG": declaration, namespaces, RDF
/// metadata, a gradient in defs, groups with transforms and style attributes.
const _inkscapePlain = '''<?xml version="1.0" encoding="UTF-8" standalone="no"?>
<!-- Created with Inkscape (http://www.inkscape.org/) -->
<svg xmlns:dc="http://purl.org/dc/elements/1.1/"
   xmlns:cc="http://creativecommons.org/ns#"
   xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
   xmlns:svg="http://www.w3.org/2000/svg"
   xmlns="http://www.w3.org/2000/svg"
   xmlns:xlink="http://www.w3.org/1999/xlink"
   width="210mm" height="297mm" viewBox="0 0 210 297" version="1.1" id="svg8">
  <defs id="defs2">
    <linearGradient id="lg1">
      <stop style="stop-color:#ff0000;stop-opacity:1" offset="0" id="s1"/>
      <stop style="stop-color:#0000ff;stop-opacity:1" offset="1" id="s2"/>
    </linearGradient>
    <linearGradient xlink:href="#lg1" id="lg2" x1="10" y1="10" x2="90" y2="90"
       gradientUnits="userSpaceOnUse"/>
    <clipPath id="clip1"><circle cx="50" cy="50" r="40"/></clipPath>
  </defs>
  <metadata id="metadata5">
    <rdf:RDF>
      <cc:Work rdf:about="">
        <dc:format>image/svg+xml</dc:format>
        <dc:type rdf:resource="http://purl.org/dc/dcmitype/StillImage"/>
      </cc:Work>
    </rdf:RDF>
  </metadata>
  <g id="layer1" transform="translate(5,5)" style="opacity:0.8">
    <rect style="fill:url(#lg2);stroke:#000000;stroke-width:0.26458332px;stroke-linecap:round"
       id="rect10" width="80" height="60" x="10" y="10" ry="4"/>
    <ellipse cx="50" cy="50" rx="20" ry="10" clip-path="url(#clip1)"
       style="fill:#00ff00;fill-opacity:0.5"/>
    <use xlink:href="#rect10" x="0" y="80" width="100%" height="100%"/>
    <text x="12" y="150" style="font-size:8px;font-family:sans-serif">
      <tspan x="12" y="150">Hello</tspan></text>
  </g>
</svg>''';

/// Documents the gate must accept, by description.
final _accepted = <String, String>{
  'a typical icon': _icon,
  'an Inkscape plain export': _inkscapePlain,
  'every basic shape': svgDocument(
      body: '<path d="M0 0h5v5z"/><rect width="1" height="1"/>'
          '<circle r="1"/><ellipse rx="1" ry="2"/><line x2="5" y2="5" '
          'stroke="#000" stroke-width="2"/><polyline points="0,0 1,1"/>'
          '<polygon points="0,0 1,1 2,0"/>'),
  'a bare public doctype': '<?xml version="1.0"?>\n<!DOCTYPE svg PUBLIC '
      '"-//W3C//DTD SVG 1.1//EN" '
      '"http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">\n$kValidSvg',
  'four nested opacity groups':
      svgDocument(body: '${'<g opacity="0.5">' * 4}$_rect${'</g>' * 4}'),
  'many sibling opacity groups':
      svgDocument(body: '<g opacity="0.5">$_rect</g>' * 50),
  'opacity of exactly 1 is no layer':
      svgDocument(body: '${'<g opacity="1">' * 10}$_rect${'</g>' * 10}'),
  'forbidden names inside title, desc and metadata': svgDocument(
      body: '<title>stroke-dasharray</title><desc><image/></desc>'
          '<metadata><script>x</script></metadata>$_rect'),
  'a same-document reference':
      svgDocument(body: '<defs><g id="a">$_rect</g></defs><use href="#a"/>'),
};

String _withAttribute(String attribute) =>
    svgDocument(body: '<rect width="10" height="10" $attribute/>');

/// Documents the gate must refuse, by description, each with a fragment of
/// the expected message.
final _rejected = <String, (String, String)>{
  for (final element in [
    'pattern', 'image', 'mask', 'filter', 'foreignObject', 'style', //
    'script', 'symbol', 'marker', 'switch', 'a', 'animate', 'set',
    'animateTransform', 'feGaussianBlur', 'textPath', 'view', 'svg:rect',
  ])
    'element $element': (
      svgDocument(body: '<$element>$_rect</$element>'),
      'element <$element> is not supported',
    ),
  'an unknown element inside a group': (
    svgDocument(body: '<g><g><iframe/></g></g>'),
    'element <iframe>',
  ),
  for (final property in kSvgForbiddenProperties) ...{
    'attribute $property': (
      _withAttribute('$property="1"'),
      'attribute $property is not supported',
    ),
    'style property $property': (
      _withAttribute('style="fill:#f00; $property : 1"'),
      'attribute $property is not supported',
    ),
    'upper-case style property $property': (
      _withAttribute('style="${property.toUpperCase()}:1"'),
      'attribute $property is not supported',
    ),
    'namespaced attribute $property': (
      svgDocument(body: '<rect xmlns:x="urn:x" x:$property="1" width="1"/>'),
      'attribute $property is not supported',
    ),
  },
  'a style with an escape': (
    _withAttribute(r'style="stroke-dash\61rray:1"'),
    'style is not supported',
  ),
  'a style with a comment': (
    _withAttribute('style="stroke-/**/dasharray:1"'),
    'style is not supported',
  ),
  'a style without a colon': (_withAttribute('style="fill"'), 'malformed'),
  'href to a URL': (
    svgDocument(body: '<use href="https://evil.example/a.svg#x"/>'),
    'only reference its own elements',
  ),
  'xlink:href to a URL': (
    svgDocument(
        body: '<use xmlns:xlink="http://www.w3.org/1999/xlink" '
            'xlink:href="http://evil.example/a.svg#x"/>'),
    'only reference its own elements',
  ),
  'href with a data URI': (
    svgDocument(body: '<use href="data:image/svg+xml,x"/>'),
    'only reference its own elements',
  ),
  'an empty fragment href': (
    svgDocument(body: '<use href="#"/>'),
    'only reference its own elements',
  ),
  'a paint server on another host': (
    _withAttribute('fill="url(http://evil.example/p.svg#g)"'),
    'only reference its own elements',
  ),
  'a doctype declaring entities': (
    '<!DOCTYPE svg [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;">]>'
        '$kValidSvg',
    'entity declarations',
  ),
  'a processing instruction': (
    '<?xml-stylesheet href="evil.css"?>$kValidSvg',
    'processing instructions',
  ),
  'a stroke width above the limit': (
    _withAttribute('stroke-width="1001"'),
    'stroke width',
  ),
  'a stroke width in a style above the limit': (
    _withAttribute('style="stroke-width:1e9px"'),
    'stroke width',
  ),
  'a stroke width that is not a number': (
    _withAttribute('stroke-width="calc(1e9)"'),
    'stroke width',
  ),
  'a negative stroke width': (_withAttribute('stroke-width="-1"'), 'stroke'),
  'mismatched tags': ('<svg><g></svg>', 'Invalid SVG'),
};

/// Documents over one budget, by description, with the limits used.
final _overBudget = <String, (String, SvgLimits)>{
  'too many elements': (
    svgDocument(body: _rect * 5),
    const SvgLimits(maxElements: 5),
  ),
  'elements nested too deep': (
    svgDocument(body: '${'<g>' * 4}$_rect${'</g>' * 4}'),
    const SvgLimits(maxElementDepth: 5),
  ),
  'five nested opacity groups': (
    svgDocument(body: '${'<g opacity="0.99">' * 5}$_rect${'</g>' * 5}'),
    const SvgLimits(),
  ),
  'nested layers through style, fill-opacity and odd values': (
    svgDocument(
        body: '<g style="opacity:.5"><g fill-opacity="0.5">'
            '<g opacity="50%"><g opacity="0"><g stroke-opacity="0.1">'
            '$_rect</g></g></g></g></g>'),
    const SvgLimits(),
  ),
  'too many use elements': (
    svgDocument(body: '<g id="a">$_rect</g>${'<use href="#a"/>' * 3}'),
    const SvgLimits(maxUses: 2),
  ),
  'too much text': (
    svgDocument(body: '<text>${'x' * 11}</text>'),
    const SvgLimits(maxTextChars: 10),
  ),
  'too much text in CDATA and tspans': (
    svgDocument(body: '<text>abc<tspan><![CDATA[defgh]]></tspan>ijk</text>'),
    const SvgLimits(maxTextChars: 10),
  ),
  'too many text elements': (
    svgDocument(body: '<text>a<tspan>b</tspan><tspan>c</tspan></text>'),
    const SvgLimits(maxTextElements: 2),
  ),
  'too much path data': (
    svgDocument(body: '<path d="M0 0h1"/><polygon points="0,0 1,1 2,2"/>'),
    const SvgLimits(maxPathDataChars: 15),
  ),
};

Matcher _rejects(String message) => throwsA(
    isA<SvgException>().having((e) => e.message, 'message', contains(message)));

void main() {
  group('accepts', () {
    for (final MapEntry(key: name, value: document) in _accepted.entries) {
      test(name, () {
        checkSvgAllowed(document);
        // What the gate accepts must also satisfy the compiler.
        expect(compileSvg(svgBytes(document)), isNotEmpty);
      });
    }
  });

  group('rejects', () {
    for (final MapEntry(key: name, value: (document, message))
        in _rejected.entries) {
      test(name, () {
        expect(() => checkSvgAllowed(document), _rejects(message));
        expect(
            () => compileSvg(svgBytes(document)), throwsA(isA<SvgException>()));
      });
    }
  });

  group('budgets', _budgetTests);
}

void _budgetTests() {
  for (final MapEntry(key: name, value: (document, limits))
      in _overBudget.entries) {
    test(name, () {
      expect(() => checkSvgAllowed(document, limits: limits),
          _rejects('too complex'));
    });
  }

  test('the defaults are the documented budgets', () {
    const limits = SvgLimits();
    expect(limits.maxElements, 5000);
    expect(limits.maxElementDepth, 32);
    expect(limits.maxLayerDepth, 4);
    expect(limits.maxUses, 200);
    expect(limits.maxTextChars, 2000);
    expect(limits.maxTextElements, 100);
    expect(limits.maxPathDataChars, 512 * 1024);
    expect(limits.maxStrokeWidth, 1000);
  });

  test('white space between elements is not counted as text', () {
    final document = svgDocument(body: '${' ' * 5000}<g>\n\n$_rect\n</g>');
    checkSvgAllowed(document, limits: const SvgLimits(maxTextChars: 10));
  });

  test('a document exactly at a budget passes', () {
    checkSvgAllowed(svgDocument(body: _rect * 4),
        limits: const SvgLimits(maxElements: 5));
    checkSvgAllowed(svgDocument(body: '<text>${'x' * 10}</text>'),
        limits: const SvgLimits(maxTextChars: 10));
  });
}
