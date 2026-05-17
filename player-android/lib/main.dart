import 'package:flutter/material.dart';

void main() => runApp(const PlayerAndroidApp());

class PlayerAndroidApp extends StatelessWidget {
  const PlayerAndroidApp({super.key});

  @override
  Widget build(BuildContext context) {
    return const MaterialApp(
      home: Scaffold(
        body: Center(child: Text('Player Android')),
      ),
    );
  }
}
