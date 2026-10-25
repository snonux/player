import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/share.dart';
import 'package:player_android/screens/edit_share_dialog.dart';

class EditClient extends PlayerApiClient {
  EditClient() : super(dio: Dio());
  String? token, description, quality;
  @override
  Future<List<Map<String, dynamic>>> streamProfiles() async => [
        {'id': 'aac128', 'label': 'Balanced · AAC 128 kbps'}
      ];
  DateTime? expiry;
  bool fail = false;
  @override
  Future<Share> updateShare(String token,
      {required String description,
      DateTime? expiresAt,
      String? audioQuality}) async {
    this.token = token;
    this.description = description;
    quality = audioQuality;
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
  Future<void> open(WidgetTester tester, EditClient client,
      {bool audio = false}) async {
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
                        mediaType: audio ? 'audio' : 'video',
                        audioQuality: audio ? 'opus96' : '',
                        expiresAt: DateTime.utc(2020)),
                  )),
          child: const Text('Open')),
    ))));
    await tester.tap(find.text('Open'));
    await tester.pumpAndSettle();
  }

  testWidgets(
      'saved share quality stays available and an edit submits Original',
      (tester) async {
    final client = EditClient();
    await open(tester, client, audio: true);
    await tester.tap(find.text('Audio quality'));
    await tester.pumpAndSettle();
    expect(find.text('Data saver · Opus 96 kbps'), findsOneWidget);
    final picker = find.byType(DropdownButtonFormField<String>);
    await tester.ensureVisible(picker);
    await tester.tap(picker);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Original (no conversion)').last);
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('edit_share_save')));
    await tester.pumpAndSettle();
    expect(client.quality, 'original');
    expect(client.expiry, isNull);
  });
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
