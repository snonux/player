// Table-driven tests for the SVG gate (svg_gate.dart): the grammar and the
// budgets, with one sample per rule. The names of forbidden properties and
// allowed elements are written out here rather than taken from the
// production constants, so that removing one from the gate fails a test.

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
      svgDocument(body: '<g opacity="0.5">$_rect</g>' * 20),
  'opacity of exactly 1 is no layer':
      svgDocument(body: '${'<g opacity="1">' * 10}$_rect${'</g>' * 10}'),
  'anything inside title, desc and metadata': svgDocument(
      body: '<title>stroke-dasharray</title><desc><image/>text</desc>'
          '<metadata><script>x</script><![CDATA[y]]></metadata>$_rect'),
  'a title and a description on a shape': svgDocument(
      body: '<rect width="10" height="10"><title>Box</title>'
          '<desc>A box</desc></rect>'),
  'the same value given as attribute and as style': svgDocument(
      body: '<rect width="10" height="10" fill="#f00" style="fill:#f00"/>'),
  'href and xlink:href with the same target': _xlinkDocument(
      '<defs><g id="a">$_rect</g></defs><use href="#a" xlink:href="#a"/>'),
  'self-closing text, groups, defs and gradients':
      svgDocument(body: '<defs/><g/><text/><linearGradient id="g"/>$_rect'),
  'every style property of the allowlist': svgDocument(
      body: '<defs><clipPath id="c">$_rect</clipPath><linearGradient id="g">'
          '<stop offset="0" style="stop-color:#f00;stop-opacity:0.5"/>'
          '</linearGradient></defs>'
          '<rect width="9" height="9" style="fill:url(#g);fill-opacity:1;'
          'fill-rule:evenodd;stroke:#000;stroke-width:2px;stroke-opacity:1;'
          'stroke-linecap:round;stroke-linejoin:round;stroke-miterlimit:4;'
          'opacity:1;display:inline;visibility:visible;clip-path:url(#c);'
          'clip-rule:nonzero;color:#00f"/>'
          '<text y="8" style="font-size:4px;font-family:serif;'
          'font-weight:bold;font-style:italic;text-anchor:middle">t</text>'),
  'a stroke width in px': svgDocument(
      body: '<line x2="1" y2="1" stroke="#000" stroke-width="1000px"/>'),
  'white space and comments between elements':
      svgDocument(body: '\n  <!-- note -->\n  <g>\n $_rect \n</g>\n'),
  'a same-document reference':
      svgDocument(body: '<defs><g id="a">$_rect</g></defs><use href="#a"/>'),
};

/// Written out on purpose; see the comment at the top of the file.
const _forbiddenProperties = [
  'stroke-dasharray',
  'stroke-dashoffset',
  'mix-blend-mode',
  'filter',
  'mask',
];

/// A document whose root declares the xlink prefix.
String _xlinkDocument(String body) => '<svg xmlns="http://www.w3.org/2000/svg" '
    'xmlns:xlink="http://www.w3.org/1999/xlink" width="9" height="9">'
    '$body</svg>';

/// [content] as the child of a [parent] element that is itself placed
/// where the grammar allows it.
String _inside(String parent, String content) => switch (parent) {
      'tspan' => '<text><tspan>$content</tspan></text>',
      'stop' => '<linearGradient id="g"><stop>$content</stop>'
          '</linearGradient>',
      _ => '<$parent>$content</$parent>',
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
  for (final property in _forbiddenProperties) ...{
    'attribute $property': (
      _withAttribute('$property="1"'),
      'attribute $property is not supported',
    ),
    'style property $property': (
      _withAttribute('style="fill:#f00; $property : 1"'),
      'style property $property is not supported',
    ),
    'upper-case style property $property': (
      _withAttribute('style="${property.toUpperCase()}:1"'),
      'style property $property is not supported',
    ),
    'upper-case attribute $property': (
      _withAttribute('${property.toUpperCase()}="1"'),
      'attribute $property is not supported',
    ),
    'namespaced attribute $property': (
      '<svg xmlns="http://www.w3.org/2000/svg" xmlns:x="urn:x" width="9" '
          'height="9"><rect x:$property="1" width="1" height="1"/></svg>',
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
    _xlinkDocument('<use xlink:href="http://evil.example/a.svg#x"/>'),
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
  for (final keyword in ['initial', 'unset', 'auto', '']) ...{
    'stroke-width "$keyword"': (
      _withAttribute('stroke-width="$keyword"'),
      'stroke width',
    ),
    'opacity "$keyword"': (
      svgDocument(body: '<g opacity="$keyword">$_rect</g>'),
      'unusable opacity',
    ),
  },
  // The compiler treats `inherit` as "attribute absent", whatever it is on.
  for (final attribute in ['stroke-width', 'opacity', 'id', 'd', 'fill'])
    '$attribute="inherit"': (
      svgDocument(body: '<path d="M0 0h9v9z" $attribute="inherit"/>'),
      attribute == 'd' ? 'more than one value' : 'inherit is not supported',
    ),
  'inherit in a style': (
    _withAttribute('style="fill:inherit"'),
    'inherit is not supported',
  ),
  for (final unit in ['em', 'ex', 'pt', 'pc', 'mm', 'cm', 'in', '%'])
    'a stroke width in $unit': (
      _withAttribute('stroke-width="2$unit"'),
      'unusable stroke width',
    ),
  // Only presentation properties may be set through style.
  for (final property in [
    'id', 'href', 'd', 'points', 'transform', 'x', 'width', 'class', //
    'style', 'font', 'marker', 'clip', 'stroke-dasharray2',
  ])
    'style property $property': (
      _withAttribute('style="$property:a"'),
      'style property $property is not supported',
    ),
  'a style value with a second colon': (
    _withAttribute('style="fill:#f00:x"'),
    'style is not supported',
  ),
  'a style value with a colon in a url': (
    _withAttribute('style="clip-path:url(#a:b)"'),
    'style is not supported',
  ),
  // The compiler reads `id:a:x` as id "a"; the gate used to read "a:x" and
  // so missed that this group, which contains a use, is what the use copies.
  'a use chain behind a style id with a second colon': (
    svgDocument(body: '<g style="id:a:x">$_rect<use href="#a"/></g>'),
    'style property id is not supported',
  ),
  'the 625-fold use chain behind style ids': (
    svgDocument(
        body: '<defs><g style="id:g0:x"><path d="${'M0 0h9' * 9000}"/></g>'
            '${[
      for (var level = 1; level <= 3; level++)
        '<g style="id:g$level:x">${'<use href="#g${level - 1}"/>' * 5}</g>',
    ].join()}</defs>${'<use href="#g3"/>' * 5}'),
    'style property id is not supported',
  ),
  'the same chain with real id attributes': (
    svgDocument(
        body: '<defs><g id="g0">$_rect</g><g id="g1"><use href="#g0"/></g>'
            '</defs><use href="#g1"/>'),
    '<use> may not reference content that contains <use>',
  ),
  'a single path over the per-path cap': (
    svgDocument(body: '<path d="M0 0${' h1' * (22 * 1024)}"/>'),
    'oversized path',
  ),
  'a points list over the per-path cap': (
    svgDocument(body: '<polygon points="${'1,1 ' * (17 * 1024)}"/>'),
    'oversized path',
  ),
  'an opacity with a unit': (
    svgDocument(body: '<g style="fill-opacity:50%">$_rect</g>'),
    'unusable fill-opacity',
  ),
  // Decoys: a second spelling or a second source for the same property.
  'path data given twice in different case': (
    svgDocument(body: '<path d="M0 0h9v9z" D=""/>'),
    'gives d more than one value',
  ),
  'stroke width given twice in different case': (
    _withAttribute('stroke-width="1e9" STROKE-WIDTH="1"'),
    'gives stroke-width more than one value',
  ),
  'opacity given twice in different case': (
    svgDocument(body: '<g opacity="0.5" OPACITY="1">$_rect</g>'),
    'gives opacity more than one value',
  ),
  'a property as attribute and, differently, in style': (
    _withAttribute('fill="#f00" style="fill:#00f"'),
    'gives fill more than one value',
  ),
  'stroke width as attribute and inherit in style': (
    _withAttribute('stroke-width="1e9" style="stroke-width:inherit"'),
    'gives stroke-width more than one value',
  ),
  'a property twice in one style': (
    _withAttribute('style="fill:#f00;fill:#f00"'),
    'gives fill more than one value',
  ),
  'href and xlink:href with different targets': (
    _xlinkDocument('<g id="a">$_rect</g><use href="#a" xlink:href="#b"/>'),
    'gives href more than one value',
  ),
  'an attribute value over 4 KB': (
    _withAttribute('transform="${'translate(1) ' * 400}"'),
    'oversized attribute',
  ),
  'a class attribute over 4 KB': (
    _withAttribute('class="${'a' * 4097}"'),
    'oversized attribute',
  ),
  'a document over 1 MB': (
    svgDocument(body: '<!--${'x' * (1024 * 1024)}-->$_rect'),
    'SVG too large',
  ),
  'a negative stroke width': (_withAttribute('stroke-width="-1"'), 'stroke'),
  'mismatched tags': ('<svg><g></svg>', 'Invalid SVG'),
  ..._misplaced,
};

/// Samples for the grammar: an allowed element in a place, or with content,
/// that the grammar does not give it.
final _misplaced = <String, (String, String)>{
  for (final (child, parent) in [
    ('rect', 'text'), ('g', 'text'), ('clipPath', 'text'), ('use', 'text'), //
    ('title', 'text'), ('desc', 'tspan'), ('text', 'tspan'), ('rect', 'tspan'),
    ('g', 'clipPath'), ('text', 'clipPath'), ('use', 'clipPath'),
    ('title', 'clipPath'), ('clipPath', 'clipPath'), ('stop', 'clipPath'),
    ('rect', 'linearGradient'), ('title', 'radialGradient'),
    ('rect', 'rect'), ('g', 'path'), ('tspan', 'circle'), ('metadata', 'rect'),
    ('rect', 'use'), ('title', 'use'), ('title', 'stop'), ('stop', 'g'),
    ('tspan', 'g'), ('svg', 'g'), ('svg', 'defs'),
  ])
    '$child inside $parent': (
      svgDocument(body: _inside(parent, '<$child></$child>')),
      '<$child> is not allowed inside <$parent>',
    ),
  'a title on a shape inside a clip path': (
    svgDocument(
        body: '<clipPath id="c"><rect width="1" height="1"><title>t</title>'
            '</rect></clipPath>$_rect'),
    '<title> is not allowed inside <rect>',
  ),
  'a nested svg': (svgDocument(body: '<svg>$_rect</svg>'), '<svg> is not'),
  'a root that is not svg': ('<g xmlns="http://www.w3.org/2000/svg"/>', 'root'),
  'a self-closing clip path': (
    svgDocument(body: '<clipPath id="c"/>$_rect'),
    'empty clip path',
  ),
  'an empty clip path': (
    svgDocument(body: '<defs><clipPath id="c"></clipPath></defs>$_rect'),
    'empty clip path',
  ),
  'a clip path that is itself clipped': (
    svgDocument(
        body: '<clipPath id="a"><rect width="1" height="1"/></clipPath>'
            '<clipPath id="b" clip-path="url(#a)"><rect width="1" '
            'height="1"/></clipPath>'),
    'may not be clipped themselves',
  ),
  'a clipped shape inside a clip path': (
    svgDocument(
        body: '<clipPath id="b"><rect width="1" height="1" '
            'style="clip-path:url(#a)"/></clipPath>'),
    'may not be clipped themselves',
  ),
  'character data in the root': (
    svgDocument(body: '${_rect}stray'),
    'character data outside a text element',
  ),
  'character data in a group': (
    svgDocument(body: '<g>stray$_rect</g>'),
    'character data outside a text element',
  ),
  'CDATA in a shape': (
    svgDocument(body: '<rect width="1" height="1"><![CDATA[x]]></rect>'),
    'character data outside a text element',
  ),
  'character data in a gradient': (
    svgDocument(body: '<linearGradient id="g">x</linearGradient>$_rect'),
    'character data outside a text element',
  ),
  'a use of content that contains a use': (
    svgDocument(
        body: '<g id="a">$_rect</g><g id="b"><use href="#a"/></g>'
            '<use href="#b"/>'),
    '<use> may not reference content that contains <use>',
  ),
  'a use of another use': (
    svgDocument(
        body: '<g id="a">$_rect</g><use id="u" href="#a"/><use href="#u"/>'),
    '<use> may not reference content that contains <use>',
  ),
  'a namespace declaration below the root': (
    svgDocument(body: '<g xmlns:x="urn:x">$_rect</g>'),
    'namespace declarations',
  ),
  'a default namespace other than SVG': (
    '<svg xmlns="http://www.w3.org/1999/xhtml"/>',
    'namespace declarations',
  ),
};

/// Documents over one budget: description -> (document, limits, reason).
final _overBudget = <String, (String, SvgLimits, String)>{
  'too many elements': (
    svgDocument(body: _rect * 5),
    const SvgLimits(maxElements: 5),
    'too many elements',
  ),
  'elements nested too deep': (
    svgDocument(body: '${'<g>' * 4}$_rect${'</g>' * 4}'),
    const SvgLimits(maxElementDepth: 5),
    'nested too deeply',
  ),
  'five nested opacity groups': (
    svgDocument(body: '${'<g opacity="0.99">' * 5}$_rect${'</g>' * 5}'),
    const SvgLimits(),
    'nests too many translucent groups',
  ),
  'nested layers through style, fill-opacity and stroke-opacity': (
    svgDocument(
        body: '<g style="opacity:.5"><g fill-opacity="0.5">'
            '<g opacity="0.5"><g opacity="0"><g stroke-opacity="0.1">'
            '$_rect</g></g></g></g></g>'),
    const SvgLimits(),
    'nests too many translucent groups',
  ),
  'too many use elements': (
    svgDocument(body: '<g id="a">$_rect</g>${'<use href="#a"/>' * 3}'),
    const SvgLimits(maxUses: 2),
    'too many <use> elements',
  ),
  'too much text': (
    svgDocument(body: '<text>${'x' * 11}</text>'),
    const SvgLimits(maxTextChars: 10),
    'too much text',
  ),
  'too much text in CDATA, tspans and white space': (
    svgDocument(
        body: '<text>ab <tspan><![CDATA[defg]]><tspan>h</tspan></tspan>'
            ' jk</text>'),
    const SvgLimits(maxTextChars: 10),
    'too much text',
  ),
  'too many text elements': (
    svgDocument(body: '<text>a<tspan>b</tspan><tspan>c</tspan></text>'),
    const SvgLimits(maxTextElements: 2),
    'too many text elements',
  ),
  'too much path data': (
    svgDocument(body: '<path d="M0 0h1"/><polygon points="0,0 1,1 2,2"/>'),
    const SvgLimits(maxPathDataChars: 15),
    'too much path data',
  ),
  'too many gradient stops': (
    svgDocument(
        body: '<linearGradient id="g">${'<stop offset="0"/>' * 3}'
            '</linearGradient>$_rect'),
    const SvgLimits(maxGradientStops: 2),
    'too many gradient stops',
  ),
  'path data multiplied by use': (
    svgDocument(
        body: '<path id="p" d="${'M0 0h9' * 10}"/>${'<use href="#p"/>' * 9}'),
    const SvgLimits(maxExpandedPathChars: 599),
    'references multiply its content too often',
  ),
  'path data multiplied by clip-path references': (
    svgDocument(
        body: '<clipPath id="c"><path d="${'M0 0h9' * 10}"/></clipPath>'
            '${'<rect width="1" height="1" clip-path="url(#c)"/>' * 9}'),
    const SvgLimits(maxExpandedPathChars: 599),
    'references multiply its content too often',
  ),
  'path data multiplied by use and clip-path together': (
    // 3 uses x 3 clip references: (1+3) x (1+3) x 60 = 960 characters.
    svgDocument(
        body: '<clipPath id="c"><path d="${'M0 0h9' * 10}"/></clipPath>'
            '<g id="g">${'<rect width="1" height="1" '
                'style="clip-path:url(#c)"/>' * 3}</g>'
            '${'<use href="#g"/>' * 3}'),
    const SvgLimits(maxExpandedPathChars: 959),
    'references multiply its content too often',
  ),
  'elements multiplied by use': (
    svgDocument(body: '<g id="a">${_rect * 8}</g>${'<use href="#a"/>' * 9}'),
    const SvgLimits(maxExpandedElements: 189),
    'references multiply its content too often',
  ),
  'text multiplied by use': (
    svgDocument(
        body: '<text id="t">${'x' * 10}</text>${'<use href="#t"/>' * 9}'),
    const SvgLimits(maxExpandedTextChars: 99),
    'references multiply its content too often',
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
        expect((compileSvg(svgBytes(document))).data, isNotEmpty);
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
  for (final MapEntry(key: name, value: (document, limits, reason))
      in _overBudget.entries) {
    test(name, () {
      expect(() => checkSvgAllowed(document, limits: limits), _rejects(reason));
    });
  }

  test('the defaults are the documented budgets', () {
    const limits = SvgLimits();
    expect(limits.maxElements, 2000);
    expect(limits.maxElementDepth, 32);
    expect(limits.maxLayerDepth, 4);
    expect(limits.maxUses, 100);
    expect(limits.maxTextChars, 2000);
    expect(limits.maxTextElements, 100);
    expect(limits.maxPathChars, 64 * 1024);
    expect(limits.maxPathDataChars, 256 * 1024);
    expect(limits.maxStrokeWidth, 1000);
    expect(limits.maxGradientStops, 256);
    expect(limits.maxAttributeChars, 4096);
    expect(limits.maxExpandedPathChars, 1024 * 1024);
    expect(limits.maxExpandedElements, 20000);
    expect(limits.maxDrawOperations, closeTo(4577.6, 0.1));
    expect(kSvgRasterBudget, 3e8);
    expect(kMinSvgRasterSide, 256);
    expect(limits.maxExpandedTextChars, 10000);
    expect(kMaxSvgBytes, 1024 * 1024);
  });

  test('the grammar allows exactly the documented elements', () {
    expect(kSvgGrammar.keys.toSet(), {
      'svg', 'g', 'defs', 'title', 'desc', 'metadata', 'path', 'rect', //
      'circle', 'ellipse', 'line', 'polyline', 'polygon', 'linearGradient',
      'radialGradient', 'stop', 'clipPath', 'use', 'text', 'tspan',
    });
    expect(kSvgForbiddenProperties, _forbiddenProperties.toSet());
    expect(kSvgStyleProperties, {
      'fill', 'fill-opacity', 'fill-rule', 'stroke', 'stroke-width', //
      'stroke-opacity', 'stroke-linecap', 'stroke-linejoin',
      'stroke-miterlimit', 'opacity', 'stop-color', 'stop-opacity',
      'font-size', 'font-family', 'font-weight', 'font-style', 'text-anchor',
      'display', 'visibility', 'clip-path', 'clip-rule', 'color',
    });
  });

  test('each expansion budget is exact', () {
    // The samples above are one over; with that one unit they pass.
    final path = _overBudget['path data multiplied by use']!.$1;
    checkSvgAllowed(path, limits: const SvgLimits(maxExpandedPathChars: 600));
    final elements = _overBudget['elements multiplied by use']!.$1;
    checkSvgAllowed(elements,
        limits: const SvgLimits(maxExpandedElements: 190));
    final text = _overBudget['text multiplied by use']!.$1;
    checkSvgAllowed(text, limits: const SvgLimits(maxExpandedTextChars: 100));
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
