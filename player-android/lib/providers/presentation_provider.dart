import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

enum PlayerAccent { blue, teal, plum }

enum ArtworkShape { rounded, square, circle }

enum ArtworkDisplay { contain, cover }

class PresentationPreferences {
  const PresentationPreferences(
      {this.accent = PlayerAccent.blue,
      this.shape = ArtworkShape.rounded,
      this.display = ArtworkDisplay.cover,
      this.keepScreenOn = false});
  final PlayerAccent accent;
  final ArtworkShape shape;
  final ArtworkDisplay display;
  final bool keepScreenOn;

  PresentationPreferences copyWith(
          {PlayerAccent? accent,
          ArtworkShape? shape,
          ArtworkDisplay? display,
          bool? keepScreenOn}) =>
      PresentationPreferences(
          accent: accent ?? this.accent,
          shape: shape ?? this.shape,
          display: display ?? this.display,
          keepScreenOn: keepScreenOn ?? this.keepScreenOn);
}

class PresentationNotifier extends AsyncNotifier<PresentationPreferences> {
  static const _key = 'player_presentation';
  @override
  Future<PresentationPreferences> build() async {
    final prefs = await SharedPreferences.getInstance();
    try {
      final data =
          jsonDecode(prefs.getString(_key) ?? '{}') as Map<String, dynamic>;
      return PresentationPreferences(
          accent: PlayerAccent.values
                  .where((v) => v.name == data['accent'])
                  .firstOrNull ??
              PlayerAccent.blue,
          shape: ArtworkShape.values
                  .where((v) => v.name == data['shape'])
                  .firstOrNull ??
              ArtworkShape.rounded,
          display: ArtworkDisplay.values
                  .where((v) => v.name == data['display'])
                  .firstOrNull ??
              ArtworkDisplay.cover,
          keepScreenOn: data['keepScreenOn'] == true);
    } catch (_) {
      return const PresentationPreferences();
    }
  }

  Future<void> save(PresentationPreferences value) async {
    if (state.isLoading) return;
    final previous = state;
    state = const AsyncLoading<PresentationPreferences>()
        .copyWithPrevious(previous);
    try {
      final prefs = await SharedPreferences.getInstance();
      final saved = await prefs.setString(
          _key,
          jsonEncode({
            'accent': value.accent.name,
            'shape': value.shape.name,
            'display': value.display.name,
            'keepScreenOn': value.keepScreenOn
          }));
      if (!saved) throw StateError('Could not save appearance');
      state = AsyncData(value);
    } catch (_) {
      state = previous;
      rethrow;
    }
  }
}

final presentationProvider =
    AsyncNotifierProvider<PresentationNotifier, PresentationPreferences>(
        PresentationNotifier.new);
