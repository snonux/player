import 'package:flutter/material.dart';

class PodcastPublicationDate extends StatelessWidget {
  const PodcastPublicationDate({super.key, required this.date, this.style});
  final DateTime? date;
  final TextStyle? style;
  @override
  Widget build(BuildContext context) => Text(
      date == null
          ? 'Publication date unavailable'
          : 'Published ${MaterialLocalizations.of(context).formatShortDate(date!.toLocal())}',
      style: style ?? Theme.of(context).textTheme.bodySmall);
}
