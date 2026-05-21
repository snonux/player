/// Immutable value object that captures the current state of the
/// [SearchFilterBar] and maps directly onto the query parameters accepted by
/// `GET /api/v1/media` (see player-server/docs/api.md).
///
/// All fields are optional — a default-constructed [MediaFilter] represents
/// "no filters applied" and produces the same result as calling
/// `listMedia(setId: ...)` with no extra parameters.
///
/// Design notes:
///   - Value object (all fields final, `==` / `hashCode` based on fields):
///     callers can do cheap equality checks to detect real changes and avoid
///     redundant API calls.
///   - No dependencies on Flutter or Riverpod; a plain Dart class so it is
///     trivially unit-testable without pumping widgets.
///   - [copyWith] supports incremental updates from the filter bar without
///     re-constructing the whole object (Immutable / Open-Closed).
class MediaFilter {
  /// Plain-text search term forwarded as the `search` query parameter.
  ///
  /// `null` or empty string ⟹ no search filter.
  final String? query;

  /// Media type filter: one of `'video'`, `'audio'`, `'image'`, or `null`
  /// to return all types.
  ///
  /// Maps to the `type` query parameter.
  final String? type;

  /// When `true`, only favourite items are returned (`favorites=true`).
  ///
  /// `false` or `null` ⟹ no favourites filter.
  final bool favoritesOnly;

  /// Sort order forwarded as the `sort` query parameter.
  ///
  /// Valid values: `'name'`, `'date'`, `'duration'`, `'play_count'`,
  /// `'random'`, or `null` for the server default.
  final String? sortBy;

  /// Creates a filter with all fields explicitly specified.
  ///
  /// All parameters have defaults corresponding to "no filter", so
  /// `const MediaFilter()` is a valid zero-filter instance.
  const MediaFilter({
    this.query,
    this.type,
    this.favoritesOnly = false,
    this.sortBy,
  });

  /// Returns a new [MediaFilter] with the supplied fields overridden.
  ///
  /// Fields not listed retain their current value, allowing callers to update
  /// a single dimension without re-stating the rest (Open-Closed Principle).
  MediaFilter copyWith({
    // Sentinel object used to detect an explicit `null` override (i.e. the
    // caller wants to clear a nullable field rather than leave it unchanged).
    Object? query = _sentinel,
    Object? type = _sentinel,
    bool? favoritesOnly,
    Object? sortBy = _sentinel,
  }) {
    return MediaFilter(
      query: query == _sentinel ? this.query : query as String?,
      type: type == _sentinel ? this.type : type as String?,
      favoritesOnly: favoritesOnly ?? this.favoritesOnly,
      sortBy: sortBy == _sentinel ? this.sortBy : sortBy as String?,
    );
  }

  // ---------------------------------------------------------------------------
  // Value semantics
  // ---------------------------------------------------------------------------

  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is MediaFilter &&
          other.query == query &&
          other.type == type &&
          other.favoritesOnly == favoritesOnly &&
          other.sortBy == sortBy;

  @override
  int get hashCode =>
      Object.hash(query, type, favoritesOnly, sortBy);

  @override
  String toString() => 'MediaFilter('
      'query: $query, '
      'type: $type, '
      'favoritesOnly: $favoritesOnly, '
      'sortBy: $sortBy)';
}

// Private sentinel object used by [MediaFilter.copyWith] to distinguish
// "omitted" from "explicitly set to null".
const _sentinel = Object();
