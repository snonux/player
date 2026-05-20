import 'package:flutter/material.dart';

/// Share screen — will list and manage share links for a media item.
///
/// Currently a lightweight placeholder; feature implementation will replace
/// the body without touching the router or other screens.
class ShareScreen extends StatelessWidget {
  const ShareScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Share')),
      body: const Center(child: Text('Share placeholder')),
    );
  }
}
