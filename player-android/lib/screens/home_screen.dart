import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';

/// Home screen — will show the media library once the list API is wired.
///
/// Uses [ConsumerWidget] so it can later watch Riverpod providers (e.g.
/// a media-list provider) without changing the class hierarchy.
class HomeScreen extends ConsumerWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Scaffold(
      appBar: AppBar(title: const Text('Library')),
      body: Center(
        child: ElevatedButton(
          onPressed: () => context.go(AppRoutes.mediaDetailPath(0)),
          child: const Text('Open media (placeholder)'),
        ),
      ),
    );
  }
}
