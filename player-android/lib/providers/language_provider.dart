import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

const languageStorageKey = 'player.language.v1';
const languageChoices = ['system', 'en', 'de'];

String normalizeLanguage(String? value) =>
    languageChoices.contains(value) ? value! : 'system';

Locale resolveMusicLocale(String choice, Locale device) => Locale(
    (choice == 'system' ? device.languageCode : choice) == 'de' ? 'de' : 'en');

class LanguagePreference {
  const LanguagePreference(this.choice, {this.sessionOnly = false});
  final String choice;
  final bool sessionOnly;
}

/// Device preference is independent of accounts and playback state.
class LanguageNotifier extends AsyncNotifier<LanguagePreference> {
  @override
  Future<LanguagePreference> build() async {
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.get(languageStorageKey);
    return LanguagePreference(
        normalizeLanguage(saved is String ? saved : null));
  }

  Future<void> choose(String value) async {
    final choice = normalizeLanguage(value);
    // Keep a usable in-memory choice even if device storage is unavailable.
    state = AsyncData(LanguagePreference(choice));
    try {
      final prefs = await SharedPreferences.getInstance();
      if (!await prefs.setString(languageStorageKey, choice)) {
        throw StateError('Language storage unavailable');
      }
    } catch (_) {
      state = AsyncData(LanguagePreference(choice, sessionOnly: true));
    }
  }
}

final languageProvider =
    AsyncNotifierProvider<LanguageNotifier, LanguagePreference>(
        LanguageNotifier.new);
