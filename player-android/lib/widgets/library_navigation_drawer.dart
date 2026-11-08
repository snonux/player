import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../app_routes.dart';
import '../l10n/music_localizations.dart';

/// Stable library destinations across connected, device and offline modes.
///
/// Destinations come first and are grouped under short section labels;
/// [extras] add mode-specific library destinations right after Podcasts and
/// Downloads, and [children] add further sections below a divider.
class LibraryNavigationDrawer extends StatelessWidget {
  const LibraryNavigationDrawer(
      {super.key, this.children = const [], this.extras = const []});
  final List<Widget> children;
  final List<Widget> extras;

  @override
  Widget build(BuildContext context) => Drawer(
        child: SafeArea(
          child:
              ListView(padding: const EdgeInsets.only(bottom: 16), children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(28, 20, 16, 12),
              child:
                  Text('Player', style: Theme.of(context).textTheme.titleLarge),
            ),
            DrawerSectionLabel(context.tr('Library')),
            for (final destination in const [
              (Icons.podcasts, 'Podcasts', AppRoutes.podcasts),
              (
                Icons.download_outlined,
                'Downloads',
                AppRoutes.offlineDownloads
              ),
            ])
              DrawerDestination(
                key: ValueKey(
                    'library_navigation_${destination.$2.toLowerCase()}'),
                icon: destination.$1,
                label: context.tr(destination.$2),
                route: destination.$3,
              ),
            ...extras,
            if (children.isNotEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 28, vertical: 8),
                child: Divider(height: 1),
              ),
            ...children,
          ]),
        ),
      );
}

/// Small section title inside the navigation drawer.
class DrawerSectionLabel extends StatelessWidget {
  const DrawerSectionLabel(this.label, {super.key});
  final String label;
  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.fromLTRB(28, 12, 16, 8),
        child: Text(label,
            style: Theme.of(context).textTheme.titleSmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant)),
      );
}

/// A drawer row that closes the drawer and opens [route], highlighted while
/// that route is the current page.
class DrawerDestination extends StatelessWidget {
  const DrawerDestination(
      {super.key,
      required this.icon,
      required this.label,
      required this.route,
      this.onTap});
  final IconData icon;
  final String label;
  final String route;

  /// Replaces the default push, e.g. for a library switch.
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final router = GoRouter.maybeOf(context);
    final current = router?.state.uri.path == route;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12),
      child: ListTile(
        shape: const StadiumBorder(),
        selected: current,
        selectedTileColor: Theme.of(context).colorScheme.secondaryContainer,
        leading: Icon(icon),
        title: Text(label),
        onTap: () {
          Navigator.of(context).pop();
          if (onTap != null) {
            onTap!();
          } else if (!current) {
            (router ?? GoRouter.of(context)).push(route);
          }
        },
      ),
    );
  }
}
