import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/foundation.dart';

/// One message of the Google Cast v2 protocol ("CASTV2").
///
/// On the wire every message is a 4-byte big-endian length followed by a
/// protobuf `CastMessage`. Only its string payload variant is used: all
/// namespaces this app speaks (connection, heartbeat, receiver, media)
/// exchange JSON. The protobuf is small and fixed, so it is encoded by hand
/// rather than through a protobuf dependency.
@immutable
class CastMessage {
  const CastMessage({
    required this.sourceId,
    required this.destinationId,
    required this.namespace,
    required this.payload,
  });

  final String sourceId;
  final String destinationId;
  final String namespace;

  /// The UTF-8 payload, a JSON object for every namespace used here.
  final String payload;

  /// The payload parsed as a JSON object; null when it is not one.
  Map<String, dynamic>? get json {
    try {
      final decoded = jsonDecode(payload);
      return decoded is Map<String, dynamic> ? decoded : null;
    } on FormatException {
      return null;
    }
  }

  @override
  String toString() => 'CastMessage($namespace $sourceId->$destinationId)';
}

// CastMessage field numbers (cast_channel.proto).
const _fieldProtocolVersion = 1;
const _fieldSourceId = 2;
const _fieldDestinationId = 3;
const _fieldNamespace = 4;
const _fieldPayloadType = 5;
const _fieldPayloadUtf8 = 6;

const _wireVarint = 0;
const _wireLengthDelimited = 2;

/// Largest frame accepted from a receiver. Real messages are a few KiB; a
/// bogus length must not make the app allocate gigabytes.
const kMaxCastFrameBytes = 64 * 1024;

/// Encodes [message] as one length-prefixed frame.
Uint8List encodeCastFrame(CastMessage message) {
  final body = BytesBuilder(copy: false);
  // proto2 "required" fields: always present, even with value 0.
  _writeVarintField(body, _fieldProtocolVersion, 0); // CASTV2_1_0
  _writeStringField(body, _fieldSourceId, message.sourceId);
  _writeStringField(body, _fieldDestinationId, message.destinationId);
  _writeStringField(body, _fieldNamespace, message.namespace);
  _writeVarintField(body, _fieldPayloadType, 0); // STRING
  _writeStringField(body, _fieldPayloadUtf8, message.payload);
  final bytes = body.toBytes();
  final frame = Uint8List(4 + bytes.length);
  ByteData.sublistView(frame).setUint32(0, bytes.length);
  frame.setRange(4, frame.length, bytes);
  return frame;
}

/// Splits a byte stream into [CastMessage]s. Frames may arrive in any
/// chunking; [add] returns the messages completed by each chunk.
class CastFrameDecoder {
  final _buffer = BytesBuilder();

  List<CastMessage> add(List<int> chunk) {
    _buffer.add(chunk);
    var data = _buffer.takeBytes();
    final messages = <CastMessage>[];
    var offset = 0;
    while (data.length - offset >= 4) {
      final length = ByteData.sublistView(data, offset).getUint32(0);
      if (length > kMaxCastFrameBytes) {
        throw const FormatException('Cast frame too large');
      }
      if (data.length - offset - 4 < length) break;
      final body = Uint8List.sublistView(data, offset + 4, offset + 4 + length);
      final message = decodeCastMessage(body);
      if (message != null) messages.add(message);
      offset += 4 + length;
    }
    if (offset < data.length) _buffer.add(Uint8List.sublistView(data, offset));
    return messages;
  }
}

/// Decodes one protobuf `CastMessage` body. Returns null for a binary
/// payload, which no namespace used here sends. Unknown fields are skipped.
CastMessage? decodeCastMessage(Uint8List body) {
  final reader = _ProtoReader(body);
  String source = '', destination = '', namespace = '';
  String? payload;
  while (!reader.done) {
    final key = reader.varint();
    final field = key >> 3;
    final wire = key & 7;
    if (wire == _wireLengthDelimited) {
      final bytes = reader.bytes();
      switch (field) {
        case _fieldSourceId:
          source = utf8.decode(bytes);
        case _fieldDestinationId:
          destination = utf8.decode(bytes);
        case _fieldNamespace:
          namespace = utf8.decode(bytes);
        case _fieldPayloadUtf8:
          payload = utf8.decode(bytes);
      }
    } else {
      reader.skip(wire);
    }
  }
  if (payload == null) return null;
  return CastMessage(
    sourceId: source,
    destinationId: destination,
    namespace: namespace,
    payload: payload,
  );
}

void _writeVarintField(BytesBuilder out, int field, int value) {
  _writeVarint(out, (field << 3) | _wireVarint);
  _writeVarint(out, value);
}

void _writeStringField(BytesBuilder out, int field, String value) {
  final bytes = utf8.encode(value);
  _writeVarint(out, (field << 3) | _wireLengthDelimited);
  _writeVarint(out, bytes.length);
  out.add(bytes);
}

void _writeVarint(BytesBuilder out, int value) {
  var v = value;
  while (v >= 0x80) {
    out.addByte((v & 0x7f) | 0x80);
    v >>= 7;
  }
  out.addByte(v);
}

class _ProtoReader {
  _ProtoReader(this._data);

  final Uint8List _data;
  int _offset = 0;

  bool get done => _offset >= _data.length;

  int varint() {
    var result = 0;
    var shift = 0;
    while (true) {
      if (_offset >= _data.length || shift > 63) {
        throw const FormatException('Truncated varint');
      }
      final byte = _data[_offset++];
      result |= (byte & 0x7f) << shift;
      if (byte < 0x80) return result;
      shift += 7;
    }
  }

  Uint8List bytes() {
    final length = varint();
    if (length < 0 || _offset + length > _data.length) {
      throw const FormatException('Truncated field');
    }
    final view = Uint8List.sublistView(_data, _offset, _offset + length);
    _offset += length;
    return view;
  }

  void skip(int wire) {
    switch (wire) {
      case _wireVarint:
        varint();
      case 1:
        _advance(8);
      case _wireLengthDelimited:
        bytes();
      case 5:
        _advance(4);
      default:
        throw FormatException('Unsupported wire type $wire');
    }
  }

  void _advance(int n) {
    if (_offset + n > _data.length) {
      throw const FormatException('Truncated field');
    }
    _offset += n;
  }
}
