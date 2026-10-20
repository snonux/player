import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/dio_client.dart';
import '../services/radio_controller.dart';
import '../services/radio_engine.dart';
import 'api_client_provider.dart';
import 'audio_handler_provider.dart';
import 'playback_session_provider.dart';

/// The web radio state of the signed-in account.
///
/// Rebuilt (and its station stopped) when the API client changes, i.e. on a
/// server or account switch. Tests override it with a controller built on a
/// fake [RadioPlaybackEngine].
final radioControllerProvider = ChangeNotifierProvider<RadioController>((ref) {
  final client = ref.watch(apiClientProvider);
  final engine = AudioHandlerRadioEngine(
    handler: ref.read(audioHandlerProvider),
    coordinator: ref.read(playbackSessionCoordinatorProvider),
    urlFor: client.radioUrl,
    headersFor: (uri) => accountRequestHeaders(
      uri: uri,
      baseUrl: ref.read(playerBaseUrlProvider),
      storage: ref.read(tokenStorageProvider),
      cookieJar: ref.read(cookieJarProvider),
      mutations: ref.read(credentialMutationQueueProvider),
    ),
  );
  return RadioController(
    engine: engine,
    exploreSource: (query, limit) => client.searchRadioStations(
      search: query.search,
      tag: query.tag,
      country: query.country,
      order: query.order,
      limit: limit,
    ),
    addBookmark: client.addRadioBookmark,
    removeBookmark: client.removeRadioBookmark,
  );
});
