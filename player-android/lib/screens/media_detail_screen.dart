import 'package:flutter/material.dart';

/// Media-detail screen — will display a single media item with player controls.
///
/// Currently a lightweight placeholder; feature implementation will replace
/// the body without touching the router or other screens.
class MediaDetailScreen extends StatelessWidget {
  const MediaDetailScreen({super.key, required this.mediaId});

  /// The string form of the media ID extracted from the route path parameter.
  final String mediaId;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('Media $mediaId')),
      body: Center(child: Text('Media detail for ID $mediaId – placeholder')),
    );
  }
}
