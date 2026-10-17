import 'dart:ui' show SemanticsAction, Tristate;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';

const _alphabet = 'abcdefgimnopqrstuvwxyz';

/// Prefix-free mnemonics for the controls currently exposed by the UI.
List<String> controlShortcutKeys(List<String> labels) {
  if (labels.length > _alphabet.length) {
    var width = 2, capacity = _alphabet.length * _alphabet.length;
    while (capacity < labels.length) {
      width++;
      capacity *= _alphabet.length;
    }
    return List.generate(labels.length, (index) {
      var value = index, code = '';
      for (var digit = 0; digit < width; digit++) {
        code = _alphabet[value % _alphabet.length] + code;
        value ~/= _alphabet.length;
      }
      return code;
    });
  }
  final used = <String>{};
  return labels.map((label) {
    final key = (label.toLowerCase() + _alphabet).split('').firstWhere(
        (letter) => _alphabet.contains(letter) && !used.contains(letter));
    used.add(key);
    return key;
  }).toList();
}

/// Hardware-keyboard navigation uses the same actions as accessibility/touch.
/// This covers dynamically created menus, dialogs and player controls without
/// maintaining a second registry of callbacks that can drift from the UI.
/// Touch layouts keep their normal appearance until shortcuts are requested.
class KeyboardAccess extends StatefulWidget {
  const KeyboardAccess({super.key, required this.child});
  final Widget child;

  static void showHints(BuildContext context) =>
      context.findAncestorStateOfType<_KeyboardAccessState>()?._show();

  @override
  State<KeyboardAccess> createState() => _KeyboardAccessState();
}

class _Control {
  const _Control(this.node, this.rect, this.label, this.key);
  final SemanticsNode node;
  final Rect rect;
  final String label;
  final String key;
}

class _KeyboardAccessState extends State<KeyboardAccess> {
  bool _active = false, _scheduled = false;
  String _prefix = '';
  SemanticsHandle? _handle;
  SemanticsOwner? _owner;
  List<_Control> _controls = [];
  SemanticsNode? _adjustable;

  @override
  void initState() {
    super.initState();
    HardwareKeyboard.instance.addHandler(_onKey);
  }

  @override
  void dispose() {
    HardwareKeyboard.instance.removeHandler(_onKey);
    _owner?.removeListener(_schedule);
    _handle?.dispose();
    super.dispose();
  }

  bool get _typing {
    final focused = FocusManager.instance.primaryFocus?.context;
    return focused?.findAncestorStateOfType<EditableTextState>() != null ||
        focused?.widget is EditableText;
  }

  void _show() {
    _handle ??= SemanticsBinding.instance.ensureSemantics();
    setState(() {
      _active = true;
      _prefix = '';
    });
    _schedule();
  }

  void _stop() {
    setState(() {
      _active = false;
      _prefix = '';
      _controls = [];
      _adjustable = null;
    });
    _owner?.removeListener(_schedule);
    _owner = null;
    _handle?.dispose();
    _handle = null;
  }

  void _schedule() {
    if (!_active || _scheduled) return;
    _scheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _scheduled = false;
      if (!mounted || !_active) return;
      _refresh();
    });
    WidgetsBinding.instance.ensureVisualUpdate();
  }

  void _refresh() {
    final owner = context.findRenderObject()?.owner?.semanticsOwner;
    if (owner != _owner) {
      _owner?.removeListener(_schedule);
      _owner = owner;
      _owner?.addListener(_schedule);
    }
    final root = owner?.rootSemanticsNode;
    if (root == null) return;
    final scale = View.of(context).devicePixelRatio;
    final screen = Offset.zero & MediaQuery.sizeOf(context);
    final nodes = <(SemanticsNode, Rect, String)>[];
    void visit(SemanticsNode node, Matrix4 parent, String inheritedLabel) {
      final data = node.getSemanticsData();
      if (data.flagsCollection.isHidden ||
          node.isMergedIntoParent ||
          data.identifier == 'keyboard-access-overlay') {
        return;
      }
      final ownLabel =
          [data.tooltip, data.label].where((text) => text.isNotEmpty).join(' ');
      final label = ownLabel.isEmpty ? inheritedLabel : ownLabel;
      final matrix = Matrix4.copy(parent);
      if (node.transform != null) matrix.multiply(node.transform!);
      final rect =
          MatrixUtils.transformRect(matrix, node.rect).intersect(screen);
      final actions = [
        SemanticsAction.tap,
        SemanticsAction.longPress,
        SemanticsAction.increase,
        SemanticsAction.decrease,
        SemanticsAction.scrollUp,
        SemanticsAction.scrollDown,
        SemanticsAction.scrollLeft,
        SemanticsAction.scrollRight
      ];
      if (!rect.isEmpty &&
          data.flagsCollection.isEnabled != Tristate.isFalse &&
          actions.any(data.hasAction)) {
        nodes.add((node, rect, label.isEmpty ? 'Scroll or adjust' : label));
      }
      node.visitChildren((child) {
        visit(child, matrix, label);
        return true;
      });
    }

    visit(root, Matrix4.diagonal3Values(1 / scale, 1 / scale, 1), '');
    final keys = controlShortcutKeys(nodes.map((entry) => entry.$3).toList());
    final next = [
      for (var i = 0; i < nodes.length; i++)
        _Control(nodes[i].$1, nodes[i].$2, nodes[i].$3, keys[i])
    ];
    final changed = next.length != _controls.length ||
        next.asMap().entries.any((entry) {
          final old = _controls[entry.key], item = entry.value;
          return item.node.id != old.node.id ||
              item.rect != old.rect ||
              item.label != old.label ||
              item.key != old.key;
        });
    if (changed) {
      setState(() {
        final remapped = next.length != _controls.length ||
            next.asMap().entries.any((entry) =>
                entry.value.node.id != _controls[entry.key].node.id ||
                entry.value.key != _controls[entry.key].key);
        _controls = next;
        if (remapped) _prefix = '';
      });
    }
  }

  void _perform(_Control control, {bool longPress = false}) {
    final node = _owner?.getSemanticsNode(control.node.id);
    if (node == null) {
      _schedule();
      return;
    }
    final data = node.getSemanticsData();
    if (data.hasAction(SemanticsAction.focus)) {
      _owner?.performAction(node.id, SemanticsAction.focus);
    }
    final action = longPress && data.hasAction(SemanticsAction.longPress)
        ? SemanticsAction.longPress
        : data.hasAction(SemanticsAction.tap)
            ? SemanticsAction.tap
            : data.hasAction(SemanticsAction.longPress)
                ? SemanticsAction.longPress
                : null;
    if (action != null) _owner?.performAction(node.id, action);
    _adjustable = node;
    _prefix = '';
    if (data.flagsCollection.isTextField) {
      _stop();
    } else {
      setState(() {});
      _schedule();
    }
  }

  bool _onKey(KeyEvent event) {
    if (!mounted || event is! KeyDownEvent) return false;
    final keyboard = HardwareKeyboard.instance;
    if (keyboard.isControlPressed ||
        keyboard.isMetaPressed ||
        keyboard.isAltPressed) {
      return false;
    }
    final key = event.logicalKey;
    if (key == LogicalKeyboardKey.f2 ||
        (key == LogicalKeyboardKey.semicolon && (_active || !_typing))) {
      if (_active) {
        _stop();
      } else {
        _show();
      }
      return true;
    }
    if (!_active) return false;
    if (key == LogicalKeyboardKey.escape) {
      _stop();
      return true;
    }
    final adjustable = _adjustable;
    if (adjustable != null) {
      final data = adjustable.getSemanticsData();
      final action = switch (key) {
        LogicalKeyboardKey.arrowRight =>
          data.hasAction(SemanticsAction.increase)
              ? SemanticsAction.increase
              : SemanticsAction.scrollRight,
        LogicalKeyboardKey.arrowLeft => data.hasAction(SemanticsAction.decrease)
            ? SemanticsAction.decrease
            : SemanticsAction.scrollLeft,
        LogicalKeyboardKey.arrowDown ||
        LogicalKeyboardKey.pageDown =>
          SemanticsAction.scrollUp,
        LogicalKeyboardKey.arrowUp ||
        LogicalKeyboardKey.pageUp =>
          SemanticsAction.scrollDown,
        _ => null,
      };
      if (action != null && data.hasAction(action)) {
        _owner?.performAction(adjustable.id, action);
        _schedule();
        return true;
      }
    }
    if (key == LogicalKeyboardKey.backspace && _prefix.isNotEmpty) {
      setState(() => _prefix = _prefix.substring(0, _prefix.length - 1));
      return true;
    }
    final letter = (event.character ?? event.logicalKey.keyLabel).toLowerCase();
    if (!RegExp(r'^[a-z]$').hasMatch(letter)) return false;
    final next = _prefix + letter;
    final matches =
        _controls.where((control) => control.key.startsWith(next)).toList();
    if (matches.isEmpty) {
      setState(() => _prefix = '');
      return true;
    }
    if (matches.length == 1 && matches.first.key == next) {
      _perform(matches.first, longPress: keyboard.isShiftPressed);
    } else {
      setState(() => _prefix = next);
    }
    return true;
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Stack(fit: StackFit.expand, children: [
      widget.child,
      if (_active)
        ExcludeSemantics(
            child: IgnorePointer(
                child: Stack(children: [
          for (final control
              in _controls.where((control) => control.key.startsWith(_prefix)))
            Positioned(
                left: control.rect.left
                    .clamp(0, MediaQuery.sizeOf(context).width - 32),
                top: control.rect.top
                    .clamp(0, MediaQuery.sizeOf(context).height - 28),
                child: Container(
                    padding:
                        const EdgeInsets.symmetric(horizontal: 5, vertical: 2),
                    decoration: BoxDecoration(
                        color: colors.surface,
                        border: Border.all(color: colors.primary),
                        borderRadius: BorderRadius.circular(4)),
                    child: Text(control.key,
                        key: ValueKey('shortcut-${control.label}'),
                        style: TextStyle(
                            color: colors.primary,
                            fontSize: 14,
                            fontWeight: FontWeight.bold,
                            decoration: TextDecoration.underline)))),
        ]))),
      if (_active)
        Positioned(
            left: 12,
            right: 12,
            bottom: 12,
            child: Semantics(
                identifier: 'keyboard-access-overlay',
                container: true,
                child: Material(
                    color: colors.surfaceContainerHighest,
                    borderRadius: BorderRadius.circular(8),
                    child: Padding(
                        padding: const EdgeInsets.all(10),
                        child: Row(children: [
                          Expanded(
                              child: Text(
                                  'Control shortcuts${_prefix.isEmpty ? '' : ': $_prefix'} · press the underlined letters. '
                                  'Arrows adjust the selected slider or scroll area. Esc cancels.',
                                  style: TextStyle(
                                      color: colors.onSurface, fontSize: 12))),
                          Semantics(
                              label: 'Close shortcuts (Escape)',
                              button: true,
                              child: IconButton(
                                  onPressed: _stop,
                                  icon: const Icon(Icons.close))),
                        ]))))),
    ]);
  }
}
