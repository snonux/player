import 'json_helpers.dart';

/// A server ticket that lets a cast device stream one media item on the
/// signed-in user's behalf (POST /api/v1/media/{id}/cast). The receiver
/// cannot send the app's credentials, so the ticket goes into its URLs.
class CastTicket {
  const CastTicket({
    required this.ticket,
    required this.streamContentType,
    required this.compatContentType,
    this.expiresAt,
  });

  final String ticket;
  final DateTime? expiresAt;

  /// Content-Type of the original stream, which the receiver must be told.
  final String streamContentType;

  /// Content-Type of the compatibility rendition.
  final String compatContentType;

  factory CastTicket.fromJson(Map<String, dynamic> json) {
    final ticket = json['ticket'];
    if (ticket is! String || ticket.isEmpty) {
      throw const FormatException('Cast ticket missing');
    }
    return CastTicket(
      ticket: ticket,
      expiresAt: dateTimeFromJson(json['expires_at']),
      streamContentType:
          json['stream_content_type'] as String? ?? 'application/octet-stream',
      compatContentType: json['compat_content_type'] as String? ?? 'video/mp4',
    );
  }
}
