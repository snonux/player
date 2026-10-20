/// Codec and quality of an audio item, as reported by the server.
class AudioFormat {
  const AudioFormat({
    this.codecLabel = '',
    this.bitrate = 0,
    this.sampleRate = 0,
    this.bitDepth = 0,
    this.channels = 0,
    this.channelLayout = '',
    this.lossless = false,
    this.transcoded = false,
  });

  final String codecLabel;

  /// Bits per second; 0 when unknown.
  final int bitrate;

  /// Hz; 0 when unknown.
  final int sampleRate;

  /// Only reported for lossless codecs; 0 when unknown.
  final int bitDepth;
  final int channels;
  final String channelLayout;
  final bool lossless;

  /// True when the app plays the server's AAC rendition instead.
  final bool transcoded;

  factory AudioFormat.fromJson(Object? json) {
    if (json is! Map) return const AudioFormat();
    int positive(Object? value) =>
        value is int && value > 0 && value < 1 << 40 ? value : 0;
    String text(Object? value) => value is String ? value : '';
    return AudioFormat(
      codecLabel: text(json['codec_label']),
      bitrate: positive(json['bitrate']),
      sampleRate: positive(json['sample_rate']),
      bitDepth: positive(json['bit_depth']),
      channels: positive(json['channels']),
      channelLayout: text(json['channel_layout']),
      lossless: json['lossless'] == true,
      transcoded: json['transcoded'] == true,
    );
  }

  /// One line such as "FLAC · 24-bit · 96 kHz · 2304 kbps · Stereo", the same
  /// wording as the web player; empty when the codec is unknown.
  String get summary {
    if (codecLabel.isEmpty) return '';
    final parts = <String>[codecLabel];
    if (bitDepth > 0) parts.add('$bitDepth-bit');
    if (sampleRate > 0) parts.add('${_khz(sampleRate)} kHz');
    if (bitrate > 0) parts.add('${(bitrate / 1000).round()} kbps');
    final channelName = _channelName();
    if (channelName.isNotEmpty) parts.add(channelName);
    final text = parts.join(' · ');
    return transcoded ? '$text (plays as AAC)' : text;
  }

  static String _khz(int hz) {
    final rounded = (hz / 100).round() / 10;
    return rounded == rounded.roundToDouble()
        ? rounded.toInt().toString()
        : rounded.toString();
  }

  String _channelName() {
    if (channels == 1) return 'Mono';
    if (channels == 2) return 'Stereo';
    final layout = RegExp(r'^\d+\.\d+').firstMatch(channelLayout);
    if (layout != null) return layout.group(0)!;
    return channels > 2 ? '$channels ch' : '';
  }
}
