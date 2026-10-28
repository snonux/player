import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/global_sync_coordinator.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/podcast_sync.dart';
import 'package:player_android/providers/global_sync_provider.dart';
import 'package:player_android/providers/podcast_association_provider.dart';
import 'package:player_android/widgets/global_sync_status.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class _VisibleSync extends GlobalSyncCoordinator {
  _VisibleSync()
      : super(PodcastSync(PodcastSyncStore(PodcastRepository())), () => null);

  void Function()? onSyncNow;
  bool manual = false;
  int revision = 0;
  GlobalSyncState? result;
  @override
  GlobalSyncState? get manualSyncResult => result;
  @override
  int get manualSyncRevision => revision;
  @override
  bool get manualSyncActive => manual;

  void show(GlobalSyncState next) {
    if (!manual && !next.active) result = next;
    state = next;
    notifyListeners();
  }

  @override
  Future<void> syncNow({bool explicit = true}) async => onSyncNow?.call();
}

void main() {
  const last = 100000;
  const boundary = last + 48 * 60 * 60 * 1000;
  const synced = GlobalSyncState(scope: 'account:a', lastFullSuccessMs: last);
  testWidgets('global chrome stays quiet until a manual sync', (tester) async {
    final sync = _VisibleSync();
    await tester.pumpWidget(ProviderScope(
        overrides: [
          globalSyncCoordinatorProvider.overrideWith((ref) => sync),
          podcastAssociationOfferProvider.overrideWith((ref) async => null),
        ],
        child:
            const MaterialApp(home: GlobalSyncChrome(child: Text('Library')))));
    for (final state in [
      const GlobalSyncState(scope: 'account:a'),
      const GlobalSyncState(scope: 'account:a', active: true),
      const GlobalSyncState(scope: 'account:a', pending: 1),
      const GlobalSyncState(scope: 'account:a', lastFullSuccessMs: last),
      const GlobalSyncState(scope: 'account:a', errorKind: 'offline'),
      const GlobalSyncState(scope: 'account:a', errorKind: 'auth'),
    ]) {
      sync.show(state);
      await tester.pump();
      expect(find.byType(TextButton), findsNothing);
      expect(find.text('Library'), findsOneWidget);
    }
    sync.manual = true;
    sync.revision++;
    sync.show(const GlobalSyncState(scope: 'account:a', active: true));
    await tester.pump();
    expect(find.textContaining('Syncing…'), findsWidgets);
    sync.manual = false;
    sync.show(GlobalSyncState(
        scope: 'account:a',
        lastFullSuccessMs: DateTime.now().millisecondsSinceEpoch));
    await tester.pump();
    expect(find.text('Synced'), findsOneWidget);
    await tester.pump(const Duration(seconds: 4));
    expect(find.byType(TextButton), findsNothing);
    sync.show(const GlobalSyncState(scope: 'account:a', pending: 1));
    await tester.pump();
    expect(find.textContaining('1 pending'), findsNothing);
    // A very fast manual request may start and finish between rendered frames.
    sync.revision++;
    sync.show(GlobalSyncState(
        scope: 'account:a',
        lastFullSuccessMs: DateTime.now().millisecondsSinceEpoch));
    await tester.pump();
    expect(find.text('Synced'), findsOneWidget);
    await tester.pump(const Duration(seconds: 4));
    expect(find.byType(TextButton), findsNothing);
  });
  testWidgets('a fast first manual sync still shows its result',
      (tester) async {
    final sync = _VisibleSync();
    await tester.pumpWidget(ProviderScope(
        overrides: [
          globalSyncCoordinatorProvider.overrideWith((ref) => sync),
          podcastAssociationOfferProvider.overrideWith((ref) async => null),
        ],
        child:
            const MaterialApp(home: GlobalSyncChrome(child: Text('Library')))));
    sync.revision++;
    sync.show(GlobalSyncState(
        scope: 'account:a',
        lastFullSuccessMs: DateTime.now().millisecondsSinceEpoch));
    await tester.pump();
    expect(find.text('Synced'), findsOneWidget);
    await tester.pump(const Duration(seconds: 4));
    expect(find.byType(TextButton), findsNothing);
  });
  testWidgets('manual result survives an automatic rerun before the next frame',
      (tester) async {
    final sync = _VisibleSync();
    await tester.pumpWidget(ProviderScope(
        overrides: [
          globalSyncCoordinatorProvider.overrideWith((ref) => sync),
          podcastAssociationOfferProvider.overrideWith((ref) async => null),
        ],
        child:
            const MaterialApp(home: GlobalSyncChrome(child: Text('Library')))));
    sync.manual = true;
    sync.revision++;
    sync.show(const GlobalSyncState(scope: 'account:a', active: true));
    await tester.pump();
    sync.manual = false;
    sync.result = GlobalSyncState(
        scope: 'account:a',
        lastFullSuccessMs: DateTime.now().millisecondsSinceEpoch);
    sync.show(const GlobalSyncState(scope: 'account:a', active: true));
    await tester.pump();
    expect(find.text('Synced'), findsOneWidget);
    expect(find.textContaining('Syncing…'), findsNothing);
    await tester.pump(const Duration(seconds: 4));
    expect(find.byType(TextButton), findsNothing);
    // Another account must not inherit a fast manual result between frames.
    sync.revision++;
    sync.show(const GlobalSyncState(scope: 'account:b', active: true));
    await tester.pump();
    expect(find.byType(TextButton), findsNothing);
    await tester.pump(const Duration(seconds: 4));
  });
  testWidgets('library menu row reports status and runs Sync now',
      (tester) async {
    final sync = _VisibleSync();
    var runs = 0;
    sync.onSyncNow = () => runs++;
    await tester.pumpWidget(ProviderScope(
        overrides: [
          globalSyncCoordinatorProvider.overrideWith((ref) => sync),
        ],
        child: const MaterialApp(
            home: Scaffold(body: Column(children: [GlobalSyncMenuTile()])))));
    expect(find.byKey(const Key('global_sync_menu_tile')), findsNothing,
        reason: 'standalone servers have nothing to sync');
    sync.show(GlobalSyncState(
        scope: 'account:a',
        lastFullSuccessMs: DateTime.now().millisecondsSinceEpoch));
    await tester.pump();
    expect(find.text('Sync now'), findsOneWidget);
    expect(find.text('Synced'), findsOneWidget);
    await tester.tap(find.text('Sync now'));
    expect(runs, 1);
    sync.show(const GlobalSyncState(scope: 'account:a', errorKind: 'auth'));
    await tester.pump();
    expect(find.text('Sign in required'), findsOneWidget);
    expect(
        tester
            .widget<ListTile>(find.byKey(const Key('global_sync_menu_tile')))
            .enabled,
        isFalse);
  });
  test('standalone, never, negative age and exact 48-hour clock', () {
    expect(globalSyncMessage(const GlobalSyncState(), boundary), '');
    expect(
        globalSyncMessage(const GlobalSyncState(scope: 'local'), boundary), '');
    expect(
        globalSyncMessage(const GlobalSyncState(scope: 'account:a'), boundary),
        'Not synced yet');
    expect(globalSyncMessage(synced, last - 1), 'Synced');
    expect(globalSyncMessage(synced, boundary - 1), 'Synced');
    expect(globalSyncMessage(synced, boundary), 'Last synced 2 days ago');
  });
  testWidgets(
      'labeled retry stays nonblocking and auth cannot retry ineffectively',
      (tester) async {
    var retries = 0;
    Future<void> show(GlobalSyncState state, {bool manual = true}) =>
        tester.pumpWidget(MaterialApp(
            home: Scaffold(
                body: Column(children: [
          const Text('Playback continues'),
          GlobalSyncStatus(
              state: state,
              manualSyncActive: manual,
              nowMs: boundary,
              onSync: () {
                retries++;
              }),
        ]))));
    await show(const GlobalSyncState(
        scope: 'account:a',
        lastFullSuccessMs: last,
        pending: 2,
        errorKind: 'offline'));
    expect(find.textContaining('changes saved on this device'), findsOneWidget);
    expect(find.byType(AlertDialog), findsNothing);
    await tester.tap(find.text('Retry'));
    expect(retries, 1);
    expect(find.text('Playback continues'), findsOneWidget);
    await show(const GlobalSyncState(scope: 'account:a', active: true));
    expect(
        tester.widget<TextButton>(find.byType(TextButton)).onPressed, isNull);
    await show(const GlobalSyncState(scope: 'account:a', errorKind: 'auth'));
    expect(find.textContaining('Sign in again'), findsOneWidget);
    expect(
        tester.widget<TextButton>(find.byType(TextButton)).onPressed, isNull);
    expect(find.textContaining('unavailable'), findsNothing);
    await show(const GlobalSyncState());
    expect(find.byType(TextButton), findsNothing);
    await show(
        const GlobalSyncState(
            scope: 'account:a', lastFullSuccessMs: boundary - 1000),
        manual: false);
    expect(find.byType(TextButton), findsNothing,
        reason: 'a background sync never opens the footer');
  });
  testWidgets(
      'manual failure result expires and account switches clear feedback',
      (tester) async {
    Future<void> show(GlobalSyncState state, bool manual) =>
        tester.pumpWidget(MaterialApp(
            home: Scaffold(
                body: GlobalSyncStatus(
                    state: state,
                    nowMs: boundary,
                    manualSyncActive: manual,
                    manualSyncResult: manual ? null : state,
                    onSync: () {}))));
    await show(const GlobalSyncState(scope: 'account:a', active: true), true);
    await show(
        const GlobalSyncState(scope: 'account:a', errorKind: 'offline'), false);
    expect(find.textContaining('Sync unavailable'), findsOneWidget);
    // A background retry must not replace the final result of the manual run.
    await show(const GlobalSyncState(scope: 'account:a', active: true), false);
    expect(find.textContaining('Sync unavailable'), findsOneWidget);
    await tester.pump(const Duration(seconds: 4));
    expect(find.byType(TextButton), findsNothing);
    await show(const GlobalSyncState(scope: 'account:a', active: true), true);
    await show(
        const GlobalSyncState(scope: 'account:a', errorKind: 'offline'), false);
    await show(const GlobalSyncState(scope: 'account:b', pending: 2), false);
    expect(find.byType(TextButton), findsNothing);
    await tester.pump(const Duration(seconds: 4));
    expect(find.byType(TextButton), findsNothing);
  });
}
