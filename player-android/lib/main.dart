import 'package:flutter/material.dart';

void main() => runApp(const PlayerAndroidApp());

class PlayerAndroidApp extends StatelessWidget {
  const PlayerAndroidApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Player',
      initialRoute: '/',
      routes: {
        '/': (context) => const HomeScreen(),
        '/now-playing': (context) => const NowPlayingScreen(),
      },
    );
  }
}

// HomeScreen shows the media library. Placeholder until the library API is wired.
class HomeScreen extends StatelessWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Library')),
      body: Center(
        child: ElevatedButton(
          onPressed: () => Navigator.pushNamed(context, '/now-playing'),
          child: const Text('Now Playing'),
        ),
      ),
    );
  }
}

// NowPlayingScreen shows the active media item. Placeholder until playback is wired.
class NowPlayingScreen extends StatelessWidget {
  const NowPlayingScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Now Playing')),
      body: const Center(child: Text('No media selected')),
    );
  }
}
