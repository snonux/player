import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:player_android/providers/server_endpoint_provider.dart';
import 'package:player_android/services/server_endpoint_router.dart';

/// Use real canonical routing without subscribing to a native network plugin.
/// Endpoint pairing and network changes have dedicated provider tests.
Override unpairedServerEndpointOverride() =>
    serverEndpointRouterProvider.overrideWith((ref) {
      final router =
          ServerEndpointRouter(primary: Uri.parse('http://test.invalid'));
      ref.onDispose(() => unawaited(router.close()));
      return router;
    });
