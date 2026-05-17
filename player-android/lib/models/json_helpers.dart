DateTime? dateTimeFromJson(Object? value) =>
    value is String && value.isNotEmpty ? DateTime.parse(value) : null;

String? dateTimeToJson(DateTime? value) => value?.toIso8601String();
