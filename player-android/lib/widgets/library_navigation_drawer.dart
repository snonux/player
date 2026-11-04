import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../app_routes.dart';
import '../l10n/music_localizations.dart';

/// Stable library destinations across connected, device and offline modes.
class LibraryNavigationDrawer extends StatelessWidget {
  const LibraryNavigationDrawer({super.key, this.children = const []});
  final List<Widget> children;

  @override
  Widget build(BuildContext context) => Drawer(
        child: SafeArea(
          child: ListView(children: [
            ListTile(title: Text(context.tr('Library'))),
            for (final destination in const [
              (Icons.podcasts, 'Podcasts', AppRoutes.podcasts),
              (
                Icons.download_outlined,
                'Downloads',
                AppRoutes.offlineDownloads
              ),
            ])
              ListTile(
                key: ValueKey(
                    'library_navigation_${destination.$2.toLowerCase()}'),
                leading: Icon(destination.$1),
                title: Text(context.tr(destination.$2)),
                onTap: () {
                  final router = GoRouter.of(context);
                  Navigator.of(context).pop();
                  if (router.state.uri.path != destination.$3) {
                    router.push(destination.$3);
                  }
                },
              ),
            if (children.isNotEmpty) const Divider(),
            ...children,
          ]),
        ),
      );
}
