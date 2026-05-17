class Tag {
  final int id;
  final String name;

  const Tag({required this.id, required this.name});

  factory Tag.fromJson(Map<String, dynamic> json) =>
      Tag(id: json['id'] as int? ?? 0, name: json['name'] as String? ?? '');

  Map<String, dynamic> toJson() => {'id': id, 'name': name};
}
