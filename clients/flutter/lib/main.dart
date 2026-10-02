import 'package:flutter/material.dart';

import 'app/connect_screen.dart';
import 'app/theme.dart';

void main() {
  runApp(const Np4App());
}

class Np4App extends StatelessWidget {
  const Np4App({super.key});

  @override
  Widget build(BuildContext context) {
    // Forced dark: a privacy tool with one deliberate look, not a themeable
    // toy. Both slots share the dark scheme so system switching is a no-op.
    return MaterialApp(
      title: 'NP4 匿名聊天',
      theme: buildNp4Theme(),
      darkTheme: buildNp4Theme(),
      themeMode: ThemeMode.dark,
      home: const ConnectScreen(),
    );
  }
}
