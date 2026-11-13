import 'dart:async';
import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/global_sync_coordinator.dart';
import 'package:player_android/services/sync_account.dart';

const scope = 'account:https://example.com#1';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('only manual sync requests activate transient footer feedback',
      () async {
    var entered = Completer<void>();
    var release = Completer<void>();
    var fail = false;
    final coordinator = GlobalSyncCoordinator(
        GlobalSyncStatusStore(),
        () => GlobalSyncAccount(
            scope: scope,
            current: () => true,
            syncMedia: (_) async {
              entered.complete();
              await release.future;
              if (fail) throw const SocketException('offline');
            },
            flushProgress: () async {},
            pendingOther: () async => 0));
    addTearDown(coordinator.dispose);
    final automatic = coordinator.syncNow(explicit: false);
    await entered.future;
    expect(coordinator.state.active, isTrue);
    expect(coordinator.manualSyncActive, isFalse);
    expect(coordinator.manualSyncRevision, 0);
    release.complete();
    await automatic;
    expect(coordinator.state.lastFullSuccessMs, isNotNull);
    expect(coordinator.manualSyncActive, isFalse);
    for (final failure in [false, true]) {
      fail = failure;
      entered = Completer<void>();
      release = Completer<void>();
      final manual = coordinator.syncNow();
      await entered.future;
      expect(coordinator.state.active, isTrue);
      expect(coordinator.manualSyncActive, isTrue);
      expect(coordinator.manualSyncRevision, failure ? 2 : 1);
      release.complete();
      await manual;
      expect(coordinator.manualSyncActive, isFalse);
      expect(coordinator.state.errorKind, failure ? 'offline' : isNull);
    }
  });
  for (final queuedManual in [false, true]) {
    test(
        'manual outcome survives queued ${queuedManual ? 'manual' : 'automatic'} sync',
        () async {
      final entered = [Completer<void>(), Completer<void>()];
      final release = [Completer<void>(), Completer<void>()];
      var runs = 0;
      final explicitRuns = <bool>[];
      final coordinator = GlobalSyncCoordinator(
          GlobalSyncStatusStore(),
          () => GlobalSyncAccount(
              scope: scope,
              current: () => true,
              syncMedia: (explicit) async {
                final run = runs++;
                explicitRuns.add(explicit);
                entered[run].complete();
                await release[run].future;
                if (explicit) throw const SocketException('offline');
              },
              flushProgress: () async {},
              pendingOther: () async => 0));
      addTearDown(coordinator.dispose);
      final first = coordinator.syncNow(explicit: !queuedManual);
      await entered[0].future;
      coordinator.trigger(explicit: queuedManual);
      release[0].complete();
      await first;
      await entered[1].future;
      expect(coordinator.state.active, isTrue);
      expect(coordinator.manualSyncActive, queuedManual);
      if (queuedManual) {
        expect(coordinator.manualSyncResult, isNull,
            reason: 'the queued explicit attempt has not completed');
      } else {
        expect(coordinator.manualSyncResult?.active, isFalse);
        expect(coordinator.manualSyncResult?.errorKind, 'offline');
      }
      final finished = Completer<void>();
      coordinator.addListener(() {
        if (!coordinator.state.active &&
            !coordinator.manualSyncActive &&
            !finished.isCompleted) {
          finished.complete();
        }
      });
      release[1].complete();
      await finished.future;
      expect(explicitRuns, queuedManual ? [false, true] : [true, false]);
      expect(coordinator.manualSyncResult?.active, isFalse);
      expect(coordinator.manualSyncResult?.errorKind, 'offline');
      if (!queuedManual) {
        expect(coordinator.state.errorKind, isNull,
            reason: 'automatic success must not replace the manual failure');
      }
    });
  }
  test(
      'global partial failure preserves full success timestamp and stale clock',
      () async {
    var now = 1000000;
    var fail = false;
    final coordinator = GlobalSyncCoordinator(
        GlobalSyncStatusStore(),
        () => GlobalSyncAccount(
            scope: scope,
            current: () => true,
            syncMedia: (_) async {
              if (fail) throw const SocketException('offline');
            },
            flushProgress: () async {},
            pendingOther: () async => 0),
        now: () => DateTime.fromMillisecondsSinceEpoch(now));
    expect(coordinator.state.configured, false);
    await coordinator.syncNow();
    expect(coordinator.state.lastFullSuccessMs, now);
    fail = true;
    now += const Duration(hours: 49).inMilliseconds;
    await coordinator.syncNow();
    expect(coordinator.state.lastFullSuccessMs, 1000000);
    expect(coordinator.state.staleAt(now), true);
    fail = false;
    await coordinator.syncNow();
    expect(coordinator.state.staleAt(now), false);
    coordinator.dispose();
  });
  test(
      'global account refresh drops prior status before loading, late run cannot stamp it',
      () async {
    const other = 'account:https://other.example#2';
    await GlobalSyncStatusStore().record(other, attemptMs: 123);
    var selected = scope;
    final held = Completer<void>();
    final entered = Completer<void>();
    GlobalSyncAccount capture() {
      final owner = selected;
      return GlobalSyncAccount(
          scope: owner,
          current: () => owner == selected,
          syncMedia: (_) async {
            entered.complete();
            await held.future;
          },
          flushProgress: () async {},
          pendingOther: () async => owner == other ? 4 : 0);
    }

    final coordinator = GlobalSyncCoordinator(GlobalSyncStatusStore(), capture);
    final running = coordinator.syncNow();
    await entered.future;
    expect(coordinator.manualSyncActive, isTrue);
    selected = other;
    final refreshing = coordinator.refreshStatus();
    expect(coordinator.state.scope, other);
    expect(coordinator.manualSyncActive, isFalse);
    expect(coordinator.state.lastFullSuccessMs, isNull);
    await refreshing;
    expect(coordinator.state.lastFullSuccessMs, 123);
    expect(coordinator.state.pending, 4);
    held.complete();
    await running;
    expect(coordinator.state.scope, other);
    expect(coordinator.state.lastFullSuccessMs, 123);
    expect(
        (await GlobalSyncStatusStore().read(scope)).lastFullSuccessMs, isNull);
    coordinator.dispose();
  });
  test('late status read cannot erase a newly committed full success',
      () async {
    final mediaEntered = Completer<void>();
    final releaseMedia = Completer<void>();
    final refreshEntered = Completer<void>();
    final releaseRefresh = Completer<int>();
    var pendingCalls = 0;
    final coordinator = GlobalSyncCoordinator(
        GlobalSyncStatusStore(),
        () => GlobalSyncAccount(
            scope: scope,
            current: () => true,
            syncMedia: (_) async {
              mediaEntered.complete();
              await releaseMedia.future;
            },
            flushProgress: () async {},
            pendingOther: () {
              pendingCalls++;
              if (pendingCalls == 2) {
                refreshEntered.complete();
                return releaseRefresh.future;
              }
              return Future.value(0);
            }),
        now: () => DateTime.fromMillisecondsSinceEpoch(9000));
    final running = coordinator.syncNow();
    await mediaEntered.future;
    final refreshing = coordinator.refreshStatus();
    await refreshEntered.future;
    releaseMedia.complete();
    await running;
    expect((await GlobalSyncStatusStore().read(scope)).lastFullSuccessMs, 9000);
    expect(coordinator.state.lastFullSuccessMs, 9000);
    releaseRefresh.complete(0);
    await refreshing;
    expect(coordinator.state.lastFullSuccessMs, 9000);
    coordinator.dispose();
  });
  test(
      'global queue failure and new edits during success keep truthful pending',
      () async {
    var pending = 1, fail = true;
    final coordinator = GlobalSyncCoordinator(
        GlobalSyncStatusStore(),
        () => GlobalSyncAccount(
            scope: scope,
            current: () => true,
            syncMedia: (_) async {},
            flushProgress: () async {},
            pendingOther: () async => pending,
            syncQueue: () async {
              if (fail) throw const SyncDeferred('policy');
              pending = 2;
            }),
        now: () => DateTime.fromMillisecondsSinceEpoch(9000));
    await coordinator.syncNow();
    expect(coordinator.state.lastFullSuccessMs, isNull);
    expect(coordinator.state.errorKind, 'policy');
    expect(coordinator.state.pending, 1);
    fail = false;
    await coordinator.syncNow();
    expect(coordinator.state.lastFullSuccessMs, 9000);
    expect(coordinator.state.pending, 2);
    expect(coordinator.state.errorKind, isNull);
    coordinator.dispose();
  });
}
