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

  void show(GlobalSyncState next) {
    state = next;
    notifyListeners();
  }
}

void main() {
  const last = 100000;
  const boundary = last + 48 * 60 * 60 * 1000;
  const synced = GlobalSyncState(scope: 'account:a', lastFullSuccessMs: last);
  testWidgets('global chrome rebuilds when coordinator state changes',
      (tester) async {
    final sync = _VisibleSync();
    await tester.pumpWidget(ProviderScope(
        overrides: [
          globalSyncCoordinatorProvider.overrideWith((ref) => sync),
          podcastAssociationOfferProvider.overrideWith((ref) async => null),
        ],
        child:
            const MaterialApp(home: GlobalSyncChrome(child: Text('Library')))));
    sync.show(const GlobalSyncState(scope: 'account:a'));
    await tester.pump();
    expect(find.text('Not synced yet'), findsOneWidget);
    sync.show(GlobalSyncState(
        scope: 'account:a',
        lastFullSuccessMs: DateTime.now().millisecondsSinceEpoch));
    await tester.pumpAndSettle();
    expect(find.text('Synced'), findsOneWidget);
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
    Future<void> show(GlobalSyncState state) => tester.pumpWidget(MaterialApp(
            home: Scaffold(
                body: Column(children: [
          const Text('Playback continues'),
          GlobalSyncStatus(
              state: state,
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
  });
}
