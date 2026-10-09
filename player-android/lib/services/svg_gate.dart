import 'package:xml/xml.dart' show XmlException;
import 'package:xml/xml_events.dart';

import 'svg_limits.dart';

// The gate in front of the SVG compiler.
//
// vector_graphics_compiler was written for trusted build-time assets: some
// of its features expand a few bytes of input into gigabytes while parsing
// (a dash array on a long line) or into screen-sized buffers while painting
// (nested opacity groups). A time limit does not bound memory, so the
// compiler must never see such a document. This gate therefore reads the
// raw XML with a streaming parser, which costs memory proportional to one
// element, and fails closed: only the elements listed here are accepted,
// with the attribute rules and budgets below. Everything else ends in the
// labelled error state.
//
// The gate mirrors how the compiler reads a document: it matches elements
// by their full name (so `svg:rect` is unknown), attributes by their local
// name (so `xlink:href` and `x:stroke-dasharray` count), and treats the
// declarations of a `style` attribute like attributes. The content of
// `title`, `desc` and `metadata` is skipped here because the compiler
// discards the subtree of every element it does not know.

/// The only elements an accepted SVG may contain.
const Set<String> kSvgAllowedElements = {
  'svg', 'g', 'defs', 'title', 'desc', 'metadata', //
  'path', 'rect', 'circle', 'ellipse', 'line', 'polyline', 'polygon',
  'linearGradient', 'radialGradient', 'stop', 'clipPath', 'use',
  'text', 'tspan',
};

/// Elements whose content is not drawn and therefore not inspected.
const Set<String> _skippedContent = {'title', 'desc', 'metadata'};

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

/// Elements the compiler turns into an offscreen layer when they carry an
/// opacity below 1.
const Set<String> _layerElements = {'svg', 'g', 'use', 'text', 'tspan'};
const List<String> _opacityProperties = [
  'opacity',
  'fill-opacity',
  'stroke-opacity',
];

final RegExp _number = RegExp(
    r'^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?(px|pt|pc|mm|cm|in|em|ex|%)?$');
final RegExp _unit = RegExp(r'[a-z%]+$');

/// Throws [SvgException] unless [xml] only uses what the gate allows.
void checkSvgAllowed(String xml, {SvgLimits limits = const SvgLimits()}) {
  final gate = _SvgGate(limits);
  try {
    parseEvents(xml, validateNesting: true).forEach(gate.onEvent);
  } on XmlException catch (error) {
    throw SvgException('Invalid SVG: ${error.message}');
  }
}

/// One element that is currently open.
class _Open {
  const _Open({this.skipped = false, this.layer = false, this.text = false});

  /// Inside `title`, `desc` or `metadata`.
  final bool skipped;
  final bool layer;
  final bool text;
}

/// Walks the XML events of one document and counts against [SvgLimits].
class _SvgGate {
  _SvgGate(this._limits);

  final SvgLimits _limits;
  final List<_Open> _open = [];
  int _elements = 0;
  int _uses = 0;
  int _textElements = 0;
  int _textChars = 0;
  int _pathChars = 0;

  void onEvent(XmlEvent event) {
    if (event is XmlStartElementEvent) {
      _start(event);
      if (event.isSelfClosing) _open.removeLast();
    } else if (event is XmlEndElementEvent) {
      if (_open.isEmpty) _reject('Invalid SVG: unbalanced tags');
      _open.removeLast();
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

  Never _reject(String message) => throw SvgException(message);

  void _start(XmlStartElementEvent event) {
    if (++_elements > _limits.maxElements ||
        _open.length >= _limits.maxElementDepth) {
      _reject('SVG is too complex');
    }
    final name = event.name;
    final insideSkipped = _open.isNotEmpty && _open.last.skipped;
    if (insideSkipped || _skippedContent.contains(name)) {
      _open.add(const _Open(skipped: true));
      return;
    }
    if (!kSvgAllowedElements.contains(name)) {
      final shown = name.length > 40 ? name.substring(0, 40) : name;
      _reject('SVG element <$shown> is not supported');
    }
    final properties = _properties(event);
    _checkProperties(properties);
    _count(name, properties);
    final layer = _createsLayer(name, properties);
    final layerDepth = _open.where((e) => e.layer).length + (layer ? 1 : 0);
    if (layerDepth > _limits.maxLayerDepth) _reject('SVG is too complex');
    final text = name == 'text' || name == 'tspan';
    _open.add(_Open(layer: layer, text: text));
  }

  /// Attributes by local name plus the declarations of `style`, all with
  /// lower-case keys.
  Map<String, String> _properties(XmlStartElementEvent event) {
    final properties = <String, String>{};
    for (final attribute in event.attributes) {
      final value = attribute.value.trim();
      if (attribute.localName != 'style') {
        properties[attribute.localName.toLowerCase()] = value;
        continue;
      }
      // Escapes and comments could spell a forbidden property in a way a
      // plain comparison misses.
      if (value.contains(r'\') || value.contains('/*')) {
        _reject('SVG style is not supported');
      }
      for (final declaration in value.split(';')) {
        if (declaration.trim().isEmpty) continue;
        final colon = declaration.indexOf(':');
        if (colon < 0) _reject('Invalid SVG: malformed style');
        properties[declaration.substring(0, colon).trim().toLowerCase()] =
            declaration.substring(colon + 1).trim();
      }
    }
    return properties;
  }

  void _checkProperties(Map<String, String> properties) {
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
    final strokeWidth = properties['stroke-width'];
    if (strokeWidth != null && strokeWidth != 'inherit') {
      final width = _parseNumber(strokeWidth);
      if (width == null || width < 0 || width > _limits.maxStrokeWidth) {
        _reject('SVG has an unusable stroke width');
      }
    }
  }

  /// The numeric part of a CSS length, or null when it is not one.
  double? _parseNumber(String value) {
    final text = value.toLowerCase();
    if (!_number.hasMatch(text)) return null;
    final number = double.tryParse(text.replaceFirst(_unit, ''));
    return number != null && number.isFinite ? number : null;
  }

  void _count(String name, Map<String, String> properties) {
    if (name == 'use') _uses++;
    if (name == 'text' || name == 'tspan') _textElements++;
    _pathChars +=
        (properties['d']?.length ?? 0) + (properties['points']?.length ?? 0);
    if (_uses > _limits.maxUses ||
        _textElements > _limits.maxTextElements ||
        _pathChars > _limits.maxPathDataChars) {
      _reject('SVG is too complex');
    }
  }

  /// Fails closed: an opacity that is not plainly a number of at least 1
  /// counts as a layer.
  bool _createsLayer(String name, Map<String, String> properties) {
    if (!_layerElements.contains(name)) return false;
    return _opacityProperties.any((property) {
      final value = properties[property];
      if (value == null) return false;
      final opacity = double.tryParse(value);
      return opacity == null || !(opacity >= 1);
    });
  }

  void _text(String value) {
    if (_open.isEmpty || !_open.last.text) return;
    _textChars += value.length;
    if (_textChars > _limits.maxTextChars) _reject('SVG is too complex');
  }
}
