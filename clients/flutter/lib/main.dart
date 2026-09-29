import 'package:flutter/material.dart';

import 'app/connect_screen.dart';

void main() {
  runApp(const Np4App());
}

class Np4App extends StatelessWidget {
  const Np4App({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'NP4 匿名聊天',
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF3B5BDB)),
        useMaterial3: true,
      ),
      darkTheme:
          ThemeData(colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF3B5BDB), brightness: Brightness.dark)),
      home: const ConnectScreen(),
    );
  }
}
