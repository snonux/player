import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/navigation_key.dart';
import 'package:player_android/providers/podcast_association_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/widgets/podcast_association.dart';

final selected = StateProvider<PodcastAssociationAccount?>((ref) => null);

class ConsentController extends PodcastAssociationController {
  ConsentController()
      : super(PodcastRepository(), () => throw UnimplementedError());
  final prompted = <String>{};
  final merged = <String>[];
  final repeated = <bool>[];
  int attempts = 0;
  bool storageFailure = false;
  bool storageFalse = false;
  Completer<void>? mergeGate;
  Completer<bool>? promptGate;
  @override
  Future<PodcastAssociationAccount?> offer(PodcastAssociationAccount account,
      {bool repeat = false}) async {
    repeated.add(repeat);
    return account.current() && (repeat || !prompted.contains(account.scope))
        ? account
        : null;
  }

  @override
  Future<bool> markPrompted(PodcastAssociationAccount account) async {
    attempts++;
    if (promptGate != null) return promptGate!.future;
    if (storageFailure) throw StateError('storage blocked');
    if (storageFalse) return false;
    if (!account.current()) return false;
    prompted.add(account.scope);
    return true;
  }

  @override
  Future<void> associate(PodcastAssociationAccount account) async {
    await mergeGate?.future;
    if (!account.current()) throw StateError('Account changed');
    merged.add(account.scope);
  }
}

void main() {
  late ConsentController controller;
  late PodcastAssociationAccount account;
  var current = true;
  setUp(() {
    current = true;
    controller = ConsentController();
    account = PodcastAssociationAccount(
        scope: 'account:https://server.example#1',
        label: 'alice@https://server.example',
        current: () => current,
        supported: () async => true);
  });
  Future<ProviderContainer> show(WidgetTester tester,
      {bool host = true, bool navigator = true}) async {
    final container = ProviderContainer(overrides: [
      selected.overrideWith((ref) => account),
      podcastAssociationAccountProvider
          .overrideWith((ref) => ref.watch(selected)),
      podcastAssociationControllerProvider.overrideWithValue(controller),
      podcastAssociationOfferProvider.overrideWith((ref) async {
        final a = ref.watch(podcastAssociationAccountProvider);
        return a == null ? null : controller.offer(a);
      }),
      podcastSubscriptionsProvider.overrideWith((ref) async => []),
    ]);
    addTearDown(container.dispose);
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
            navigatorKey: navigator ? navigatorKey : null,
            builder: host
                ? (context, child) => PodcastAssociationHost(child: child!)
                : null,
            home: const Scaffold(body: PodcastAssociationSettings()))));
    await tester.pumpAndSettle();
    return container;
  }

  testWidgets(
      'automatic consent names account; Keep separate and settings Sync are explicit',
      (tester) async {
    await show(tester);
    expect(
        find.text(
            "Sync this device's podcasts with alice@https://server.example?"),
        findsOneWidget);
    expect(controller.merged, isEmpty);
    await tester.tap(find.text('Keep separate'));
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(controller.merged, isEmpty);
    await tester.tap(find.text("Sync this device's podcasts"));
    await tester.pumpAndSettle();
    expect(controller.repeated, contains(true));
    await tester.tap(find.text('Sync'));
    await tester.pumpAndSettle();
    expect(controller.merged, [account.scope]);
  });

  testWidgets('Back dismisses without association or another automatic prompt',
      (tester) async {
    await show(tester);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(controller.prompted, {account.scope});
    expect(controller.merged, isEmpty);
    expect(find.byType(AlertDialog), findsNothing);
  });

  testWidgets(
      'switching account while consent is open removes Sync and cannot merge',
      (tester) async {
    final container = await show(tester);
    current = false;
    container.read(selected.notifier).state = null;
    await tester.pumpAndSettle();
    expect(find.text('Account changed'), findsOneWidget);
    expect(find.text('Sync'), findsNothing);
    await tester.tap(find.text('Close'));
    await tester.pumpAndSettle();
    expect(controller.merged, isEmpty);
    expect(find.text("Sync this device's podcasts"), findsNothing);
  });

  testWidgets(
      'failed automatic prompt storage is bounded; settings retry remains explicit',
      (tester) async {
    controller.storageFailure = true;
    final container = await show(tester);
    for (var i = 0; i < 3; i++) {
      container.invalidate(podcastAssociationOfferProvider);
      await tester.pumpAndSettle();
    }
    expect(controller.attempts, 1);
    expect(find.byType(AlertDialog), findsNothing);
    expect(tester.takeException(), isNull);
    controller.storageFailure = false;
    await tester.tap(find.text("Sync this device's podcasts"));
    await tester.pumpAndSettle();
    expect(controller.attempts, 2);
    expect(find.byType(AlertDialog), findsOneWidget);
  });

  testWidgets(
      'false storage result stays quiet automatically and explains Settings retry failure',
      (tester) async {
    controller.storageFalse = true;
    final container = await show(tester);
    container.invalidate(podcastAssociationOfferProvider);
    await tester.pumpAndSettle();
    expect(controller.attempts, 1);
    expect(
        find.text('Could not prepare podcast sync. Try again.'), findsNothing);
    await tester.tap(find.text("Sync this device's podcasts"));
    await tester.pumpAndSettle();
    expect(find.text('Could not prepare podcast sync. Try again.'),
        findsOneWidget);
    expect(find.byType(AlertDialog), findsNothing);
    expect(controller.merged, isEmpty);
    expect(tester.takeException(), isNull);
  });

  testWidgets('missing root navigator cannot loop automatic offers',
      (tester) async {
    final container = await show(tester, navigator: false);
    container.invalidate(podcastAssociationOfferProvider);
    await tester.pumpAndSettle();
    expect(controller.attempts, 0);
    expect(find.byType(AlertDialog), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
      'disposing host during pending prompt preparation never opens dialog',
      (tester) async {
    controller.promptGate = Completer<bool>();
    await show(tester);
    await tester.pumpWidget(const SizedBox.shrink());
    controller.promptGate!.complete(true);
    await tester.pumpAndSettle();
    expect(controller.merged, isEmpty);
    expect(find.byType(AlertDialog), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('disposing settings during merge cannot use its old WidgetRef',
      (tester) async {
    controller.mergeGate = Completer<void>();
    await show(tester, host: false);
    await tester.tap(find.text("Sync this device's podcasts"));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Sync'));
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox.shrink());
    controller.mergeGate!.complete();
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });

  testWidgets(
      'disposing consent host keeps navigator alive without using its old ref',
      (tester) async {
    final container = await show(tester);
    expect(find.text('Sync'), findsOneWidget);
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
            navigatorKey: navigatorKey,
            home: const Scaffold(body: Text('Host removed')))));
    await tester.pumpAndSettle();
    expect(find.text('Sync'), findsOneWidget);
    await tester.tap(find.text('Sync'));
    await tester.pumpAndSettle();
    expect(controller.merged, isEmpty);
    expect(tester.takeException(), isNull);
  });

  testWidgets('account switch during merge refuses old consent',
      (tester) async {
    controller.mergeGate = Completer<void>();
    final container = await show(tester);
    await tester.tap(find.text('Sync'));
    await tester.pumpAndSettle();
    current = false;
    container.read(selected.notifier).state = null;
    controller.mergeGate!.complete();
    await tester.pumpAndSettle();
    expect(controller.merged, isEmpty);
    expect(tester.takeException(), isNull);
  });
}
