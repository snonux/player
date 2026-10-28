import 'package:flutter/material.dart';

/// App-bar "hamburger" holding the secondary actions of a screen, so the
/// screen itself shows only its essential controls and content.
///
/// [items] builds the sheet rows on every open, so they reflect the current
/// screen state. Rows are usually [OverflowMenuItem]s; self-contained tiles
/// (switches, tiles that resolve their own availability) also work.
class OverflowMenuButton extends StatelessWidget {
  const OverflowMenuButton(
      {super.key, required this.items, this.enabled = true});

  final List<Widget> Function(BuildContext sheetContext) items;
  final bool enabled;

  @override
  Widget build(BuildContext context) => IconButton(
        key: const Key('overflow_menu_button'),
        tooltip: 'More options',
        style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
        icon: const Icon(Icons.menu),
        onPressed: !enabled
            ? null
            : () => showModalBottomSheet<void>(
                  context: context,
                  // Queue and other sheets nest this menu; root navigator keeps
                  // the sheet visible above the host modal.
                  useRootNavigator: true,
                  isScrollControlled: true,
                  useSafeArea: true,
                  showDragHandle: true,
                  builder: (sheet) => SafeArea(
                    child: SingleChildScrollView(
                      key: const Key('overflow_menu_sheet'),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: items(sheet),
                      ),
                    ),
                  ),
                ),
      );
}

/// A menu row that closes the menu before running [onSelected], so the
/// action can open its own dialog, sheet or route from the screen.
class OverflowMenuItem extends StatelessWidget {
  const OverflowMenuItem({
    super.key,
    required this.icon,
    required this.label,
    required this.onSelected,
    this.subtitle,
    this.selected = false,
  });

  final IconData icon;
  final String label;
  final String? subtitle;
  final bool selected;
  final VoidCallback? onSelected;

  @override
  Widget build(BuildContext context) => ListTile(
        leading: Icon(icon),
        title: Text(label),
        subtitle: subtitle == null ? null : Text(subtitle!),
        trailing: selected ? const Icon(Icons.check) : null,
        selected: selected,
        enabled: onSelected != null,
        onTap: onSelected == null
            ? null
            : () {
                Navigator.of(context).pop();
                onSelected!();
              },
      );
}
