import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/share.dart';
import 'package:player_android/screens/edit_share_dialog.dart';

class EditClient extends PlayerApiClient {
  EditClient() : super(dio: Dio());
  String? token, description;
  DateTime? expiry;
  bool fail = false;
  @override
  Future<Share> updateShare(String token,
      {required String description, DateTime? expiresAt}) async {
    this.token = token;
    this.description = description;
    expiry = expiresAt;
    if (fail) throw Exception('offline');
    return Share(
        token: token,
        mediaId: 1,
        createdBy: 2,
        usedCount: 1,
        description: description);
  }
}

void main() {
  Future<void> open(WidgetTester tester, EditClient client) async {
    await tester.pumpWidget(MaterialApp(
        home: Scaffold(
            body: Builder(
      builder: (context) => TextButton(
          onPressed: () => showDialog<Share>(
              context: context,
              builder: (_) => EditShareDialog(
                    client: client,
                    share: Share(
                        token: 'same-token',
                        mediaId: 1,
                        createdBy: 2,
                        usedCount: 1,
                        description: 'Original',
                        expiresAt: DateTime.utc(2020)),
                  )),
          child: const Text('Open')),
    ))));
    await tester.tap(find.text('Open'));
    await tester.pumpAndSettle();
  }

  testWidgets('description-only edit retains exact expired lifetime and token',
      (tester) async {
    final client = EditClient();
    await open(tester, client);
    await tester.enterText(
        find.byKey(const Key('edit_share_description')), 'Changed');
    await tester.tap(find.byKey(const Key('edit_share_save')));
    await tester.pumpAndSettle();
    expect(client.token, 'same-token');
    expect(client.description, 'Changed');
    expect(client.expiry, isNull);
    expect(find.byType(EditShareDialog), findsNothing);
  });
  testWidgets('failed save keeps draft and permits cancel without retrying',
      (tester) async {
    final client = EditClient()..fail = true;
    await open(tester, client);
    await tester.enterText(
        find.byKey(const Key('edit_share_description')), 'Draft');
    await tester.tap(find.byKey(const Key('edit_share_save')));
    await tester.pumpAndSettle();
    expect(find.text('Draft'), findsOneWidget);
    expect(find.textContaining('Could not save share.'), findsOneWidget);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(find.byType(EditShareDialog), findsNothing);
  });
}
