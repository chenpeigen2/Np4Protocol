import 'dart:convert';
import 'dart:io';

/// Chat history persistence: JSONL next to the identity file
/// (<identity>.chatlog, last [limit] entries). Plaintext by design —
/// anyone with filesystem access to the identity already owns this
/// identity's messages; deleting the file erases the history.
class ChatHistory {
  ChatHistory(this.path);

  static const limit = 500;
  final String path;

  /// Loads the persisted conversation (oldest first). Torn tail lines are
  /// skipped; missing file → empty list.
  List<Map<String, dynamic>> load() {
    try {
      final file = File(path);
      if (!file.existsSync()) return const [];
      final entries = <Map<String, dynamic>>[];
      for (final line in file.readAsLinesSync()) {
        if (line.trim().isEmpty) continue;
        try {
          entries.add(jsonDecode(line) as Map<String, dynamic>);
        } on FormatException {
          continue; // torn tail line: skip, next write rewrites cleanly
        }
      }
      return entries.length > 200
          ? entries.sublist(entries.length - 200)
          : entries;
    } catch (e) {
      // Storage errors must never block the UI; start with empty history.
      return const [];
    }
  }

  /// Appends one entry, trimming the file to the most recent [limit].
  void append(Map<String, dynamic> entry) {
    try {
      final file = File(path);
      final lines = file.existsSync()
          ? file.readAsLinesSync()
          : <String>[];
      lines.add(jsonEncode(entry));
      final trimmed = lines.length > limit
          ? lines.sublist(lines.length - limit)
          : lines;
      file.writeAsStringSync(
        '${trimmed.join('\n')}\n',
        mode: FileMode.write,
        encoding: utf8,
      );
    } catch (e) {
      // History write failures are non-fatal: log-free fallback is to keep
      // chatting (the in-memory list is untouched).
    }
  }
}
