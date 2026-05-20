import 'package:flutter/material.dart';

/// Login screen — will host the credentials form once the auth API is wired.
///
/// Currently a lightweight placeholder; feature implementation will replace
/// the body without touching the router or other screens.
class LoginScreen extends StatelessWidget {
  const LoginScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Login')),
      body: const Center(child: Text('Login placeholder')),
    );
  }
}
