import 'package:dio/dio.dart';

// ---------------------------------------------------------------------------
// Shared Dio error-mapping utilities
// ---------------------------------------------------------------------------
//
// These top-level functions centralise the conversion of [DioException]
// values into human-readable UI strings, eliminating duplicate
// _dioErrorMessage implementations that previously existed in
// bootstrap_screen.dart, login_screen.dart, and home_screen.dart (DRY/DIP).
//
// All functions are pure data-transformations: no widget state, no Riverpod
// reads, no BuildContext — making them easy to unit-test in isolation.

/// Maps an exception thrown by any API call to a human-readable UI string.
///
/// Prefers messages extracted from the [DioException] response body; falls
/// back to status-code–specific text; finally uses a generic connectivity
/// message.  Pass [statusFallbacks] to supply caller-specific status-code
/// messages (e.g. 401 → "Invalid username or password." for login).
String dioErrorMessage(
  DioException e, {
  Map<int, String> statusFallbacks = const {},
}) {
  // Prefer a human-readable message from the server's JSON response body.
  final body = e.response?.data;
  if (body is Map<String, dynamic>) {
    final msg = body['message'] as String? ?? body['error'] as String?;
    if (msg != null && msg.isNotEmpty) return msg;
  }

  // Apply caller-specific status-code fallbacks (e.g. auth screens).
  final statusCode = e.response?.statusCode;
  if (statusCode != null) {
    final fallback = statusFallbacks[statusCode];
    if (fallback != null) return fallback;
  }

  // Generic status-code fallback.
  if (statusCode != null) {
    return 'Server error ($statusCode). Please try again.';
  }

  // No HTTP response: connectivity or DNS failure.
  return 'Could not reach the server. Check your network connection.';
}

/// Maps a [DioException] using connection-type heuristics instead of status
/// codes — suited for read-only data-fetching calls (e.g. listing sets)
/// where there is no login-specific 401/403 semantics.
///
/// Distinguishes between connectivity/timeout failures and server-side HTTP
/// errors so the user knows whether to check their network or contact support.
String dioConnectionErrorMessage(DioException e) {
  switch (e.type) {
    case DioExceptionType.connectionError:
    case DioExceptionType.sendTimeout:
    case DioExceptionType.receiveTimeout:
    case DioExceptionType.connectionTimeout:
      return 'Could not reach the server. Check your connection and try again.';
    case DioExceptionType.badResponse:
      final code = e.response?.statusCode ?? 0;
      if (code == 401) return 'Session expired. Please log in again.';
      return 'Server error ($code). Please try again.';
    default:
      return 'Unexpected error. Please try again.';
  }
}

/// Maps any thrown object from [PlayerApiClient.listSets] to a UI string.
///
/// Delegates to [dioConnectionErrorMessage] for [DioException]; returns a
/// generic fallback for all other exception types.
String setsErrorMessage(Object error) {
  if (error is DioException) {
    return dioConnectionErrorMessage(error);
  }
  return 'Unexpected error. Please try again.';
}

/// Maps any thrown object from [PlayerApiClient.listMedia] to a UI string.
///
/// Identical delegation strategy to [setsErrorMessage]: DioExceptions are
/// mapped by [dioConnectionErrorMessage]; all other exceptions fall back to a
/// generic message.  Having a separate function preserves the option to add
/// media-specific status-code overrides (e.g. 403 permission errors) later
/// without altering the sets helper (Open-Closed Principle).
String mediaErrorMessage(Object error) {
  if (error is DioException) {
    return dioConnectionErrorMessage(error);
  }
  return 'Unexpected error. Please try again.';
}

/// Maps any thrown object from [PlayerApiClient.getMedia] to a UI string.
///
/// Adds a 404-specific message ("Media not found") on top of the generic
/// connection-error mapping so the detail screen can distinguish between a
/// missing item and a network/server failure (Open-Closed: isolated from the
/// list-media helper so either can evolve independently).
String mediaDetailErrorMessage(Object error) {
  if (error is DioException) {
    // Surface a friendly "not found" message for 404 so users know the item
    // no longer exists rather than seeing a generic server-error message.
    if (error.response?.statusCode == 404) {
      return 'Media not found. It may have been deleted.';
    }
    return dioConnectionErrorMessage(error);
  }
  return 'Unexpected error. Please try again.';
}

/// Maps any thrown object from [PlayerApiClient.createShare] to a UI string.
///
/// Adds a 404-specific message (media not found) and a 403 message
/// (permission denied) on top of the generic connection-error fallback, so
/// the share dialog can surface actionable guidance rather than a raw code.
/// Kept as a separate function (Open-Closed) so it can evolve independently
/// of the other mappers.
String createShareErrorMessage(Object error) {
  if (error is DioException) {
    if (error.response?.statusCode == 404) {
      return 'Media not found. It may have been deleted.';
    }
    if (error.response?.statusCode == 403) {
      return 'You do not have permission to share this item.';
    }
    return dioConnectionErrorMessage(error);
  }
  return 'Unexpected error. Please try again.';
}

/// Maps any thrown object from [PlayerApiClient.listSets] (used by
/// [PodcastListScreen]) to a UI string.
///
/// Identical delegation strategy to [setsErrorMessage]: DioExceptions are
/// mapped by [dioConnectionErrorMessage]; all other exceptions fall back to a
/// generic message.  Having a separate function preserves the option to add
/// podcast-specific status-code overrides later without altering the sets
/// helper (Open-Closed Principle).
String podcastListErrorMessage(Object error) {
  if (error is DioException) {
    return dioConnectionErrorMessage(error);
  }
  return 'Unexpected error. Please try again.';
}

/// Maps any thrown object from [PlayerApiClient.subscribePodcast] to a UI string.
///
/// Adds human-readable messages for the common failure modes:
///   - 400: the feed URL is malformed or the server could not parse the feed.
///   - 403: the user is not an admin (subscribe requires admin privileges).
///   - 409/500: generic server-side failure (duplicate subscription, etc.).
///
/// Kept as a separate top-level function (Open-Closed, DRY) so it can evolve
/// independently of the share and sets mappers.
String podcastErrorMessage(Object error) {
  if (error is DioException) {
    if (error.response?.statusCode == 400) {
      return 'Invalid feed URL or the feed could not be parsed. Check the URL and try again.';
    }
    if (error.response?.statusCode == 403) {
      return 'Only administrators can subscribe to podcast feeds.';
    }
    return dioConnectionErrorMessage(error);
  }
  return 'Unexpected error. Please try again.';
}
