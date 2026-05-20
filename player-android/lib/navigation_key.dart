import 'package:flutter/material.dart';

/// The go_router navigator key, shared between [routerProvider] and
/// [_UnauthorizedInterceptor] in [DioClient] so that 401 responses can
/// trigger a navigation to /login without requiring a [BuildContext].
///
/// Kept in a dedicated file to break any circular import that would arise
/// if router.dart and api_client_provider.dart tried to import each other.
final navigatorKey = GlobalKey<NavigatorState>(debugLabel: 'go_router');
