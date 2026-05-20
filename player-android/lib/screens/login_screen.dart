import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';

/// Sign-in screen shown to returning users who already have an account.
///
/// Displays a username and password form; on successful authentication the
/// session token is persisted via [TokenStorage] and the auth state transitions
/// to [AuthStatus.authenticated], which causes go_router to redirect to the
/// home screen automatically (no explicit navigation call needed).
///
/// Design notes:
///   - Mirrors the structure of [BootstrapScreen] for consistency.
///   - All mutable state lives in [_LoginScreenState].
///   - [ConsumerStatefulWidget] is used so [WidgetRef] is available across the
///     async submit path without storing a stale ref as an instance field.
///   - Error mapping is a top-level function ([_dioErrorMessage]) — pure, no
///     widget state, no BuildContext — so it is easy to unit-test in isolation.
class LoginScreen extends ConsumerStatefulWidget {
  const LoginScreen({super.key});

  @override
  ConsumerState<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends ConsumerState<LoginScreen> {
  // Form key for programmatic validation across all fields.
  final _formKey = GlobalKey<FormState>();

  // Controllers for the two text fields; disposed in [dispose] to prevent
  // memory leaks after the widget is removed from the tree.
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();

  // Drives the loading indicator and disables the submit button while an
  // HTTP request is in flight to prevent double-submission.
  bool _isLoading = false;

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Validation helpers
  // ---------------------------------------------------------------------------

  /// Returns an error string when [value] is empty or null; null otherwise.
  String? _validateRequired(String? value) {
    if (value == null || value.trim().isEmpty) {
      return 'This field is required.';
    }
    return null;
  }

  // ---------------------------------------------------------------------------
  // Submit logic
  // ---------------------------------------------------------------------------

  /// Validates the form and submits the login request to the server.
  ///
  /// On success, stores the username as the session token via [AuthStateNotifier.login]
  /// so that [AuthStateNotifier.build] can restore the session on the next app
  /// start; go_router's redirect callback then navigates to [AppRoutes.home]
  /// automatically.
  ///
  /// On failure, a human-readable error is shown in a [SnackBar].  All async
  /// continuations check [mounted] to avoid using a stale [BuildContext] after
  /// the widget is disposed.
  Future<void> _submit() async {
    // Client-side validation: abort early if any field is invalid.
    if (!(_formKey.currentState?.validate() ?? false)) return;

    setState(() => _isLoading = true);

    try {
      final apiClient = ref.read(apiClientProvider);
      final username = _usernameController.text.trim();
      final password = _passwordController.text;

      // POST /api/v1/auth/login.  Returns the authenticated [User] on 200;
      // throws [DioException] with status 401 for wrong credentials.
      final user = await apiClient.login(
        username: username,
        password: password,
      );

      // Persist the session marker so [AuthStateNotifier.build] can restore
      // auth state on the next cold start without requiring re-login.
      // The username acts as the session presence marker here; a follow-up
      // task will replace this with a real bearer token from createAPIToken.
      if (!mounted) return;
      await ref.read(authStateProvider.notifier).login(user.username);
    } on DioException catch (e) {
      // Guard against stale BuildContext if the widget was disposed during
      // the async gap (e.g. a rapid navigation triggered by another listener).
      if (!mounted) return;
      _showError(_dioErrorMessage(e));
    } catch (e) {
      if (!mounted) return;
      _showError('An unexpected error occurred. Please try again.');
    } finally {
      // Only call setState if the widget is still in the tree; dispose can
      // run before the finally block when error-path navigation triggers it.
      if (mounted) {
        setState(() => _isLoading = false);
      }
    }
  }

  /// Displays [message] in a [SnackBar] anchored to the nearest [Scaffold].
  void _showError(String message) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(message),
        backgroundColor: Theme.of(context).colorScheme.error,
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Sign In')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
          child: Form(
            key: _formKey,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                // Page heading.
                Text(
                  'Welcome back',
                  style: Theme.of(context).textTheme.headlineMedium,
                ),
                const SizedBox(height: 8),
                Text(
                  'Enter your credentials to continue.',
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
                const SizedBox(height: 32),

                // Username field.
                TextFormField(
                  key: const Key('login_username'),
                  controller: _usernameController,
                  decoration: const InputDecoration(
                    labelText: 'Username',
                    border: OutlineInputBorder(),
                  ),
                  textInputAction: TextInputAction.next,
                  autocorrect: false,
                  validator: _validateRequired,
                ),
                const SizedBox(height: 16),

                // Password field (obscured; Enter/Done triggers submit).
                TextFormField(
                  key: const Key('login_password'),
                  controller: _passwordController,
                  decoration: const InputDecoration(
                    labelText: 'Password',
                    border: OutlineInputBorder(),
                  ),
                  obscureText: true,
                  textInputAction: TextInputAction.done,
                  onFieldSubmitted: (_) => _isLoading ? null : _submit(),
                  validator: _validateRequired,
                ),
                const SizedBox(height: 32),

                // Submit button: replaced by a centered progress indicator
                // while the HTTP request is in flight.
                _isLoading
                    ? const Center(child: CircularProgressIndicator())
                    : ElevatedButton(
                        key: const Key('login_submit'),
                        onPressed: _submit,
                        child: const Text('Sign In'),
                      ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// File-level helpers
// ---------------------------------------------------------------------------

/// Extracts a user-friendly error message from a [DioException].
///
/// Pure data-transformation function: no widget state, no Riverpod reads, no
/// BuildContext — lives at the top level to make that clear and to ease testing.
///
/// Priority order for message extraction:
///   1. Server-supplied `message` or `error` field from the JSON response body.
///   2. Status-code–specific fallback strings.
///   3. Generic connectivity message when no HTTP response is available.
String _dioErrorMessage(DioException e) {
  final statusCode = e.response?.statusCode;

  // Prefer a human-readable message from the server's JSON response body.
  final body = e.response?.data;
  if (body is Map<String, dynamic>) {
    final msg = body['message'] as String? ?? body['error'] as String?;
    if (msg != null && msg.isNotEmpty) return msg;
  }

  // Status-code fallbacks for common auth error cases.
  if (statusCode == 401) {
    return 'Invalid username or password.';
  }
  if (statusCode == 400) {
    return 'Invalid request. Check your username and password.';
  }
  if (statusCode != null) {
    return 'Server error ($statusCode). Please try again.';
  }

  // No HTTP response: connectivity or DNS failure.
  return 'Could not reach the server. Check your network connection.';
}
