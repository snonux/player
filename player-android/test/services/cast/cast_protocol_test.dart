import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/cast/cast_protocol.dart';

void main() {
  const message = CastMessage(
    sourceId: 'sender-0',
    destinationId: 'receiver-0',
    namespace: 'urn:x-cast:com.google.cast.receiver',
    payload: '{"type":"GET_STATUS","requestId":1,"name":"Wohnzimmer – TV"}',
  );

  test('a frame round-trips, also when it arrives byte by byte', () {
    final frame = encodeCastFrame(message);
    expect(ByteData.sublistView(frame).getUint32(0), frame.length - 4);

    final decoder = CastFrameDecoder();
    final decoded = <CastMessage>[];
    for (final byte in [...frame, ...frame]) {
      decoded.addAll(decoder.add([byte]));
    }
    expect(decoded, hasLength(2));
    for (final m in decoded) {
      expect(m.sourceId, message.sourceId);
      expect(m.destinationId, message.destinationId);
      expect(m.namespace, message.namespace);
      expect(m.payload, message.payload);
      expect(m.json!['type'], 'GET_STATUS');
    }
  });

  test('encodes the required protobuf fields in the reference layout', () {
    final body = encodeCastFrame(const CastMessage(
      sourceId: 'a',
      destinationId: 'b',
      namespace: 'n',
      payload: '{}',
    )).sublist(4);
    expect(body, [
      0x08, 0x00, // protocol_version = CASTV2_1_0
      0x12, 0x01, 0x61, // source_id = "a"
      0x1a, 0x01, 0x62, // destination_id = "b"
      0x22, 0x01, 0x6e, // namespace = "n"
      0x28, 0x00, // payload_type = STRING
      0x32, 0x02, 0x7b, 0x7d, // payload_utf8 = "{}"
    ]);
  });

  test('skips unknown fields and ignores binary payloads', () {
    final withExtra = Uint8List.fromList([
      0x08, 0x00, 0x12, 0x01, 0x61, 0x1a, 0x01, 0x62, 0x22, 0x01, 0x6e,
      0x28, 0x00, 0x32, 0x02, 0x7b, 0x7d,
      0x48, 0x96, 0x01, // field 9 varint
      0x55, 1, 2, 3, 4, // field 10 fixed32
    ]);
    expect(decodeCastMessage(withExtra)!.payload, '{}');

    final binary = Uint8List.fromList([
      0x08, 0x00, 0x12, 0x01, 0x61, 0x1a, 0x01, 0x62, 0x22, 0x01, 0x6e,
      0x28, 0x01, 0x3a, 0x01, 0x00, // payload_binary
    ]);
    expect(decodeCastMessage(binary), isNull);
  });

  test('rejects oversized and truncated frames', () {
    expect(() => CastFrameDecoder().add([0x7f, 0xff, 0xff, 0xff]),
        throwsFormatException);
    expect(() => decodeCastMessage(Uint8List.fromList([0x12, 0x05, 0x61])),
        throwsFormatException);
  });

  test('json is null for a payload that is not an object', () {
    const notJson = CastMessage(
        sourceId: '', destinationId: '', namespace: '', payload: 'PING');
    expect(notJson.json, isNull);
  });
}
