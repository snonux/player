import 'dart:convert';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../app_routes.dart';

enum MusicCollection {
  library('Library', AppRoutes.home),
  albums('Albums', AppRoutes.musicAlbums),
  artists('Artists', AppRoutes.musicArtists),
  playlists('Playlists', AppRoutes.musicPlaylists);

  const MusicCollection(this.label, this.route);
  final String label, route;
}

class NavigationPreferences {
  const NavigationPreferences(
      {this.landing = MusicCollection.library, this.hidden = const {}});
  final MusicCollection landing;
  final Set<MusicCollection> hidden;
  MusicCollection get initialCollection =>
      hidden.contains(landing) ? MusicCollection.library : landing;
  NavigationPreferences copyWith(
          {MusicCollection? landing, Set<MusicCollection>? hidden}) =>
      NavigationPreferences(
          landing: landing ?? this.landing, hidden: hidden ?? this.hidden);
  factory NavigationPreferences.fromJson(Map<String, dynamic> data) =>
      NavigationPreferences(
          landing: MusicCollection.values
                  .where((v) => v.name == data['landing'])
                  .firstOrNull ??
              MusicCollection.library,
          hidden: MusicCollection.values
              .where((v) =>
                  v != MusicCollection.library &&
                  data['hidden'] is List &&
                  (data['hidden'] as List).contains(v.name))
              .toSet());
}

class NavigationPreferencesNotifier
    extends AsyncNotifier<NavigationPreferences> {
  static const storageKey = 'player_navigation';
  @override
  Future<NavigationPreferences> build() async {
    final prefs = await SharedPreferences.getInstance();
    try {
      return NavigationPreferences.fromJson(
          jsonDecode(prefs.getString(storageKey) ?? '{}')
              as Map<String, dynamic>);
    } catch (_) {
      return const NavigationPreferences();
    }
  }

  Future<void> save(NavigationPreferences value) async {
    if (state.isLoading) return;
    final previous = state;
    state =
        const AsyncLoading<NavigationPreferences>().copyWithPrevious(previous);
    try {
      final prefs = await SharedPreferences.getInstance();
      final saved = await prefs.setString(
          storageKey,
          jsonEncode({
            'landing': value.initialCollection.name,
            'hidden': value.hidden.map((v) => v.name).toList(),
          }));
      if (!saved) throw StateError('Could not save navigation');
      state = AsyncData(value.copyWith(landing: value.initialCollection));
    } catch (_) {
      state = previous;
      rethrow;
    }
  }
}

final navigationPreferencesProvider =
    AsyncNotifierProvider<NavigationPreferencesNotifier, NavigationPreferences>(
        NavigationPreferencesNotifier.new);
