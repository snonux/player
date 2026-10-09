import 'package:xml/xml.dart' show XmlException;
import 'package:xml/xml_events.dart';

import 'svg_limits.dart';

// The gate in front of the SVG compiler.
//
// vector_graphics_compiler was written for trusted build-time assets: a few
// bytes of input can make it allocate gigabytes while parsing (a dash array
// on a long line, a path copied by hundreds of `<use>` or `clip-path`
// references) or make the renderer allocate screen-sized buffers (nested
// opacity groups). A time limit does not bound memory, so the compiler
// must never see such a document. This gate reads the raw XML with a
// streaming parser, whose memory use is proportional to one element, and
// fails closed.
//
// The gate does not try to predict what the compiler does with unusual
// input; the compiler has quirks (it matches attributes by local name,
// drops the value `inherit`, cuts a style value at its second colon, and
// loses track of its element stack after a self-closing `<clipPath/>`).
// Instead the gate defines a small grammar of its own that is stricter
// than the compiler's: which element may contain which, where character
// data may appear, which properties a `style` may set, and that every
// property of an element has exactly one plain value. The aim is that a
// document inside this grammar leaves the compiler no room for a second
// interpretation.
//
// What the grammar and the budgets are is spelled out by the constants
// below and by `SvgLimits`.

const String _svgNamespace = 'http://www.w3.org/2000/svg';

const Set<String> _shapes = {
  'path', 'rect', 'circle', 'ellipse', 'line', 'polyline', 'polygon', //
};
const Set<String> _notes = {'title', 'desc'};
const Set<String> _gradients = {'linearGradient', 'radialGradient'};

/// What `svg` (the root only), `g` and `defs` may contain.
const Set<String> _groupChildren = {
  'g', 'defs', 'use', 'clipPath', 'text', 'metadata', //
  ..._shapes, ..._gradients, ..._notes,
};

/// The grammar: every allowed element and the children it may have. One
/// exception is applied in [_SvgGate._allowedChildren]: a shape inside a
/// clip path may have no children at all.
final Map<String, Set<String>> kSvgGrammar = {
  'svg': _groupChildren,
  'g': _groupChildren,
  'defs': _groupChildren,
  'text': {'tspan'},
  'tspan': {'tspan'},
  'clipPath': _shapes,
  'linearGradient': {'stop'},
  'radialGradient': {'stop'},
  'stop': {},
  'use': {},
  'title': {},
  'desc': {},
  'metadata': {},
  for (final shape in _shapes) shape: _notes,
};

/// Elements whose content is not drawn and therefore not inspected.
const Set<String> _skippedContent = {..._notes, 'metadata'};

/// Attributes and style properties that are refused wherever they appear:
/// dashes are expanded into one path segment per dash while parsing, and
/// the others make the renderer allocate offscreen buffers.
const Set<String> kSvgForbiddenProperties = {
  'stroke-dasharray',
  'stroke-dashoffset',
  'mix-blend-mode',
  'filter',
  'mask',
};

/// The only properties a `style` attribute may set: presentation only.
/// Anything structural (`id`, `href`, `d`, `points`, `transform`, ...)
/// must be a real attribute, so that references and geometry have exactly
/// one spelling.
const Set<String> kSvgStyleProperties = {
  'fill', 'fill-opacity', 'fill-rule', 'stroke', 'stroke-width', //
  'stroke-opacity', 'stroke-linecap', 'stroke-linejoin', 'stroke-miterlimit',
  'opacity', 'stop-color', 'stop-opacity', 'font-size', 'font-family',
  'font-weight', 'font-style', 'text-anchor', 'display', 'visibility',
  'clip-path', 'clip-rule', 'color',
};

/// Properties that must be plain numbers without a unit.
const Set<String> _opacityProperties = {
  'opacity',
  'fill-opacity',
  'stroke-opacity',
  'stop-opacity',
};

/// Elements the compiler turns into an offscreen layer when they carry an
/// opacity below 1.
const Set<String> _layerElements = {'svg', 'g', 'use', 'text', 'tspan'};

/// Attributes that hold path data and may therefore be long.
const Set<String> _pathData = {'d', 'points'};

final RegExp _plainNumber = RegExp(r'^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$');

/// Throws [SvgException] unless [xml] is inside the grammar and budgets.
void checkSvgAllowed(String xml, {SvgLimits limits = const SvgLimits()}) {
  if (xml.length > kMaxSvgBytes) throw const SvgException('SVG too large');
  final gate = _SvgGate(limits);
  try {
    parseEvents(xml, validateNesting: true).forEach(gate.onEvent);
  } on XmlException catch (error) {
    throw SvgException('Invalid SVG: ${error.message}');
  }
  gate.finish();
}

/// One element that is currently open.
class _Open {
  _Open(this.name, {this.skipped = false, this.layer = false, this.id});

  final String name;

  /// `title`, `desc`, `metadata` or anything inside them.
  final bool skipped;
  final bool layer;
  final String? id;
  bool hasChild = false;

  bool get isText => name == 'text' || name == 'tspan';
}

/// Walks the XML events of one document, enforcing the grammar and
/// counting against [SvgLimits].
class _SvgGate {
  _SvgGate(this._limits);

  final SvgLimits _limits;
  final List<_Open> _open = [];
  bool _rootSeen = false;
  int _elements = 0;
  int _uses = 0;
  int _clipReferences = 0;
  int _textElements = 0;
  int _textChars = 0;
  int _pathChars = 0;
  int _stops = 0;

  /// Ids of elements that are, or contain, a `<use>`, and the ids that
  /// `<use>` elements point at. A match means a chain of references.
  final Set<String> _idsWithUse = {};
  final Set<String> _useTargets = {};

  Never _reject(String message) => throw SvgException(message);

  void onEvent(XmlEvent event) {
    if (event is XmlStartElementEvent) {
      _start(event);
      if (event.isSelfClosing) _end();
    } else if (event is XmlEndElementEvent) {
      if (_open.isEmpty) _reject('Invalid SVG: unbalanced tags');
      _end();
    } else if (event is XmlTextEvent) {
      _text(event.value);
    } else if (event is XmlCDATAEvent) {
      _text(event.value);
    } else if (event is XmlDoctypeEvent) {
      // A bare `<!DOCTYPE svg PUBLIC ...>` as older editors write it is
      // harmless (nothing is fetched). An internal subset is where entities
      // are declared, and those are refused.
      if ((event.internalSubset ?? '').trim().isNotEmpty) {
        _reject('SVG entity declarations are not supported');
      }
    } else if (event is XmlProcessingEvent) {
      _reject('SVG processing instructions are not supported');
    }
  }

  /// Checks that apply to the document as a whole.
  void finish() {
    if (_open.isNotEmpty || !_rootSeen) _reject('Invalid SVG: no root element');
    if (_useTargets.any(_idsWithUse.contains)) {
      _reject('SVG <use> may not reference content that contains <use>');
    }
    // Every `<use>` copies what it references and every `clip-path`
    // reference copies the clip path, and a used group may itself contain
    // clipped elements. Without resolving references, the product below is
    // an upper bound for how often any part of the document is copied,
    // provided the check above caught every chain of `<use>`. That check
    // relies on this gate and the compiler agreeing on the `id` and `href`
    // of every element, which is why both may only be plain attributes.
    final copies = (1 + _uses) * (1 + _clipReferences);
    if (copies * _pathChars > _limits.maxExpandedPathChars ||
        copies * _elements > _limits.maxExpandedElements ||
        copies * _textChars > _limits.maxExpandedTextChars) {
      _reject('SVG references multiply its content too often');
    }
  }

  void _start(XmlStartElementEvent event) {
    if (++_elements > _limits.maxElements) {
      _reject('SVG has too many elements');
    }
    if (_open.length >= _limits.maxElementDepth) {
      _reject('SVG is nested too deeply');
    }
    final name = event.name;
    final parent = _open.isEmpty ? null : _open.last;
    parent?.hasChild = true;
    if (parent != null && parent.skipped) {
      _open.add(_Open(name, skipped: true));
      return;
    }
    _checkPlacement(name, parent);
    if (_skippedContent.contains(name)) {
      _open.add(_Open(name, skipped: true));
      return;
    }
    final properties = _properties(event, isRoot: parent == null);
    _checkProperties(name, properties);
    _count(name, properties);
    final layer = _createsLayer(name, properties);
    final layerDepth = _open.where((e) => e.layer).length + (layer ? 1 : 0);
    if (layerDepth > _limits.maxLayerDepth) {
      _reject('SVG nests too many translucent groups');
    }
    _open.add(_Open(name, layer: layer, id: properties['id']));
  }

  /// The grammar: the root is one `svg`, and [name] must be a child that
  /// its parent allows.
  void _checkPlacement(String name, _Open? parent) {
    final shown = name.length > 40 ? name.substring(0, 40) : name;
    if (!kSvgGrammar.containsKey(name)) {
      _reject('SVG element <$shown> is not supported');
    }
    if (parent == null) {
      if (_rootSeen || name != 'svg') {
        _reject('Invalid SVG: root must be <svg>');
      }
      _rootSeen = true;
      return;
    }
    if (!_allowedChildren(parent).contains(name)) {
      _reject('SVG element <$shown> is not allowed inside <${parent.name}>');
    }
  }

  /// A shape may carry `title` and `desc`, except inside a clip path, where
  /// the compiler reads every child as geometry.
  Set<String> _allowedChildren(_Open parent) {
    final inClipPath = _open.any((e) => e.name == 'clipPath');
    if (inClipPath && _shapes.contains(parent.name)) return const {};
    return kSvgGrammar[parent.name]!;
  }

  void _end() {
    final closed = _open.removeLast();
    // The compiler loses track of its element stack after a clip path
    // without children; an empty one would clip everything away anyway.
    if (closed.name == 'clipPath' && !closed.hasChild) {
      _reject('SVG has an empty clip path');
    }
  }

  /// Character data may only appear in text. Elsewhere the compiler would
  /// ignore it or, with its stack confused, lay it out as text.
  void _text(String value) {
    final parent = _open.isEmpty ? null : _open.last;
    if (parent != null && parent.skipped) return;
    if (parent != null && parent.isText) {
      _textChars += value.length;
      if (_textChars > _limits.maxTextChars) _reject('SVG has too much text');
    } else if (value.trim().isNotEmpty) {
      _reject('SVG has character data outside a text element');
    }
  }

  /// The properties of one element: its attributes by lower-case local name
  /// and the declarations of `style`. Each property must end up with
  /// exactly one value, so that it cannot matter which one a reader picks.
  Map<String, String> _properties(
    XmlStartElementEvent event, {
    required bool isRoot,
  }) {
    final properties = <String, String>{};
    void put(String name, String value) {
      final previous = properties[name];
      if (previous != null && previous != value) {
        _reject('SVG gives $name more than one value');
      }
      // The compiler treats `inherit` as if the attribute were absent.
      if (value == 'inherit') _reject('SVG value inherit is not supported');
      properties[name] = value;
    }

    for (final attribute in event.attributes) {
      final name = attribute.localName.toLowerCase();
      final value = attribute.value.trim();
      if (!_pathData.contains(name) &&
          value.length > _limits.maxAttributeChars) {
        _reject('SVG has an oversized attribute');
      }
      if (attribute.name == 'xmlns' || attribute.name.startsWith('xmlns:')) {
        _checkNamespace(attribute.name, value, isRoot: isRoot);
      } else if (name == 'style') {
        _styleDeclarations(value).forEach(put);
      } else {
        put(name, value);
      }
    }
    return properties;
  }

  /// Namespaces are declared once, on the root, and the default one is SVG.
  void _checkNamespace(String name, String value, {required bool isRoot}) {
    if (!isRoot || (name == 'xmlns' && value != _svgNamespace)) {
      _reject('SVG namespace declarations are not supported here');
    }
  }

  Map<String, String> _styleDeclarations(String style) {
    // Escapes and comments could spell a property name in a way a plain
    // comparison misses.
    if (style.contains(r'\') || style.contains('/*')) {
      _reject('SVG style is not supported');
    }
    final declarations = <String, String>{};
    for (final declaration in style.split(';')) {
      if (declaration.trim().isEmpty) continue;
      final colon = declaration.indexOf(':');
      if (colon < 0) _reject('Invalid SVG: malformed style');
      final name = declaration.substring(0, colon).trim().toLowerCase();
      final value = declaration.substring(colon + 1).trim();
      if (!kSvgStyleProperties.contains(name)) {
        final shown = name.length > 40 ? name.substring(0, 40) : name;
        _reject('SVG style property $shown is not supported');
      }
      // The compiler splits a declaration at every colon and keeps only
      // the second part, so it would read a shorter value than this gate.
      if (value.contains(':')) _reject('SVG style is not supported');
      if (declarations.containsKey(name)) {
        _reject('SVG gives $name more than one value');
      }
      declarations[name] = value;
    }
    return declarations;
  }

  void _checkProperties(String element, Map<String, String> properties) {
    for (final name in kSvgForbiddenProperties) {
      if (properties.containsKey(name)) {
        _reject('SVG attribute $name is not supported');
      }
    }
    final href = properties['href'];
    if (href != null && !(href.startsWith('#') && href.length > 1)) {
      _reject('SVG may only reference its own elements');
    }
    for (final value in properties.values) {
      final url = value.indexOf('url(');
      if (url >= 0 && !value.startsWith('#', url + 4)) {
        _reject('SVG may only reference its own elements');
      }
    }
    _checkNumbers(properties);
    if (properties.containsKey('clip-path') &&
        (element == 'clipPath' || _open.any((e) => e.name == 'clipPath'))) {
      _reject('SVG clip paths may not be clipped themselves');
    }
  }

  /// Stroke width and opacities must be plain numbers. Keywords are
  /// refused, and so are units on a stroke width other than `px`: the
  /// compiler converts `1000em` to 14000, far above the cap checked here.
  void _checkNumbers(Map<String, String> properties) {
    final strokeWidth = properties['stroke-width'];
    if (strokeWidth != null) {
      final number = strokeWidth.endsWith('px')
          ? strokeWidth.substring(0, strokeWidth.length - 2)
          : strokeWidth;
      final width = _parseNumber(number);
      if (width == null || width < 0 || width > _limits.maxStrokeWidth) {
        _reject('SVG has an unusable stroke width');
      }
    }
    for (final name in _opacityProperties) {
      final value = properties[name];
      if (value != null && _parseNumber(value) == null) {
        _reject('SVG has an unusable $name');
      }
    }
  }

  double? _parseNumber(String value) {
    if (!_plainNumber.hasMatch(value)) return null;
    final number = double.tryParse(value);
    return number != null && number.isFinite ? number : null;
  }

  void _count(String name, Map<String, String> properties) {
    if (name == 'use') _countUse(properties);
    if (name == 'text' || name == 'tspan') _textElements++;
    if (name == 'stop') _stops++;
    if (properties.containsKey('clip-path')) _clipReferences++;
    final pathChars =
        (properties['d']?.length ?? 0) + (properties['points']?.length ?? 0);
    if (pathChars > _limits.maxPathChars) _reject('SVG has an oversized path');
    _pathChars += pathChars;
    if (_uses > _limits.maxUses) _reject('SVG has too many <use> elements');
    if (_textElements > _limits.maxTextElements) {
      _reject('SVG has too many text elements');
    }
    if (_stops > _limits.maxGradientStops) {
      _reject('SVG has too many gradient stops');
    }
    if (_pathChars > _limits.maxPathDataChars) {
      _reject('SVG has too much path data');
    }
  }

  /// Notes what this `<use>` points at and which identified elements
  /// contain it (itself included), for the chain check in [finish].
  void _countUse(Map<String, String> properties) {
    _uses++;
    final href = properties['href'];
    if (href != null) _useTargets.add(href.substring(1));
    final ownId = properties['id'];
    if (ownId != null) _idsWithUse.add(ownId);
    for (final ancestor in _open) {
      if (ancestor.id != null) _idsWithUse.add(ancestor.id!);
    }
  }

  /// An opacity below 1 on a group makes it an offscreen layer.
  bool _createsLayer(String name, Map<String, String> properties) {
    if (!_layerElements.contains(name)) return false;
    return ['opacity', 'fill-opacity', 'stroke-opacity'].any((property) {
      final value = properties[property];
      return value != null && !(double.parse(value) >= 1);
    });
  }
}
