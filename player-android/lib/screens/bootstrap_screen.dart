import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';

// Minimum password length enforced by the server (see handlers_auth.go,
// service/auth.go — passwords shorter than this are rejected with 400).
const _kMinPasswordLength = 8;

/// First-run setup screen that creates the initial admin account.
///
/// Displayed when no users exist on the server.  Submitting the form calls
/// POST /api/auth/bootstrap; on success the auth state transitions to
/// [AuthStatus.authenticated] and the router redirects to [AppRoutes.home].
///
/// Design notes:
///   - All mutable state lives in [_BootstrapFormState]; the outer widget is a
///     lightweight [ConsumerStatefulWidget] that owns the Riverpod [Ref].
///   - Validation is client-side only (password length, match check).  Server
///     errors (e.g. 403 "already bootstrapped") are surfaced via a snack-bar.
///   - [ConsumerStatefulWidget] is used instead of [StatelessWidget] so that
///     [WidgetRef] is available across the async submit path without storing it
///     as a field (which would risk using a stale ref after disposal).
class BootstrapScreen extends ConsumerStatefulWidget {
  const BootstrapScreen({super.key});

  @override
  ConsumerState<BootstrapScreen> createState() => _BootstrapScreenState();
}

class _BootstrapScreenState extends ConsumerState<BootstrapScreen> {
  // Form key used to trigger programmatic validation across all fields.
  final _formKey = GlobalKey<FormState>();

  // Controllers for the three text fields.  Disposed in [dispose] to avoid
  // memory leaks when the widget is removed from the tree.
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();
  final _confirmController = TextEditingController();

  // Whether an HTTP request is in flight; drives loading indicator visibility
  // and disables the submit button to prevent double-submission.
  bool _isLoading = false;

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    _confirmController.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Validation helpers
  // ---------------------------------------------------------------------------

  /// Returns an error string if [value] is empty or null; null otherwise.
  String? _validateRequired(String? value) {
    if (value == null || value.trim().isEmpty) {
      return 'This field is required.';
    }
    return null;
  }

  /// Validates the password field: must meet minimum-length policy.
  String? _validatePassword(String? value) {
    final required = _validateRequired(value);
    if (required != null) return required;

    if (value!.length < _kMinPasswordLength) {
      return 'Password must be at least $_kMinPasswordLength characters.';
    }
    return null;
  }

  /// Validates the confirm-password field: must match the password field.
  String? _validateConfirm(String? value) {
    final required = _validateRequired(value);
    if (required != null) return required;

    if (value != _passwordController.text) {
      return 'Passwords do not match.';
    }
    return null;
  }

  // ---------------------------------------------------------------------------
  // Submit logic
  // ---------------------------------------------------------------------------

  /// Validates the form and submits the bootstrap request.
  ///
  /// On success, persists the authenticated session via [AuthStateNotifier.login]
  /// and lets go_router's redirect logic navigate to [AppRoutes.home].
  ///
  /// On server error, extracts a human-readable message from the [DioException]
  /// (status code + response body) and shows it in a [SnackBar].
  Future<void> _submit() async {
    // Client-side validation: abort if any field is invalid.
    if (!(_formKey.currentState?.validate() ?? false)) return;

    setState(() => _isLoading = true);

    try {
      final apiClient = ref.read(apiClientProvider);
      final username = _usernameController.text.trim();
      final password = _passwordController.text;

      // Call POST /api/v1/auth/bootstrap.  On success the server creates the
      // admin user and sets a session cookie.  The returned User confirms the
      // account was created; we use its username as the session marker stored
      // in TokenStorage so that [AuthStateNotifier.build] recognises a prior
      // successful login on the next app start.
      //
      // NOTE: The server uses cookie-based session auth.  For a full mobile
      // bearer-token flow a follow-up task should call createAPIToken after
      // bootstrap and persist that token instead.
      final user = await apiClient.bootstrap(
        username: username,
        password: password,
      );

      // Persist the session marker and update auth state → router redirects
      // automatically to AppRoutes.home via the refreshListenable.
      await ref
          .read(authStateProvider.notifier)
          .login(user.username);
    } on DioException catch (e) {
      // Only show the snack-bar if the widget is still mounted; async gaps can
      // occur between the await above and this error handler.
      if (!mounted) return;
      _showError(_dioErrorMessage(e));
    } catch (e) {
      if (!mounted) return;
      _showError('An unexpected error occurred. Please try again.');
    } finally {
      // Guard against calling setState on a disposed widget (e.g. if the error
      // path triggers navigation and dispose runs before finally executes).
      if (mounted) {
        setState(() => _isLoading = false);
      }
    }
  }

  /// Shows [message] in a [SnackBar] anchored to the nearest [Scaffold].
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
      appBar: AppBar(title: const Text('Set Up Admin Account')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
          child: Form(
            key: _formKey,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                // Introductory copy explaining the one-time setup context.
                Text(
                  'Welcome to Player',
                  style: Theme.of(context).textTheme.headlineMedium,
                ),
                const SizedBox(height: 8),
                Text(
                  'No accounts exist yet. Create the initial admin account '
                  'to get started.',
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
                const SizedBox(height: 32),

                // Username field.
                TextFormField(
                  key: const Key('bootstrap_username'),
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

                // Password field (obscured, minimum-length validated).
                TextFormField(
                  key: const Key('bootstrap_password'),
                  controller: _passwordController,
                  decoration: const InputDecoration(
                    labelText: 'Password',
                    border: OutlineInputBorder(),
                    helperText:
                        'Minimum $_kMinPasswordLength characters.',
                  ),
                  obscureText: true,
                  textInputAction: TextInputAction.next,
                  validator: _validatePassword,
                ),
                const SizedBox(height: 16),

                // Confirm-password field (must match password field).
                TextFormField(
                  key: const Key('bootstrap_confirm'),
                  controller: _confirmController,
                  decoration: const InputDecoration(
                    labelText: 'Confirm Password',
                    border: OutlineInputBorder(),
                  ),
                  obscureText: true,
                  textInputAction: TextInputAction.done,
                  onFieldSubmitted: (_) => _isLoading ? null : _submit(),
                  validator: _validateConfirm,
                ),
                const SizedBox(height: 32),

                // Submit button: replaced by a progress indicator while loading.
                _isLoading
                    ? const Center(child: CircularProgressIndicator())
                    : ElevatedButton(
                        key: const Key('bootstrap_submit'),
                        onPressed: _submit,
                        child: const Text('Create Admin Account'),
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
/// Prefers a `message` or `error` field from the response JSON body; falls
/// back to the HTTP status line, or a generic connectivity message.
String _dioErrorMessage(DioException e) {
  final statusCode = e.response?.statusCode;

  // Try to read a server-supplied message from the response body.
  final body = e.response?.data;
  if (body is Map<String, dynamic>) {
    final msg = body['message'] as String? ?? body['error'] as String?;
    if (msg != null && msg.isNotEmpty) return msg;
  }

  // Fallback to HTTP status descriptions.
  if (statusCode == 403) {
    return 'Bootstrap already complete — an admin account already exists.';
  }
  if (statusCode == 400) {
    return 'Invalid request. Check your username and password.';
  }
  if (statusCode != null) {
    return 'Server error ($statusCode). Please try again.';
  }

  return 'Could not reach the server. Check your network connection.';
}
