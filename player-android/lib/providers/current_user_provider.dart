import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import 'auth_state_provider.dart';

/// Provides the currently authenticated [User] object.
///
/// Uses the identity returned by login/bootstrap. A missing or invalid saved
/// identity leaves admin controls hidden until the account signs in again.
final currentUserProvider = FutureProvider.autoDispose<User?>((ref) async {
  final auth = await ref.watch(authStateProvider.future);
  return auth.isAuthenticated ? auth.user : null;
});
