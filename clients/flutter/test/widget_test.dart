import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:np4_client/app/theme.dart';
import 'package:np4_client/main.dart';

void main() {
  testWidgets('connect screen renders with the new visual language',
      (tester) async {
    await tester.pumpWidget(const Np4App());
    expect(find.text('NP4 匿名聊天'), findsWidgets);
    expect(find.text('进入匿名网络'), findsOneWidget);
    expect(find.byIcon(Icons.shield_outlined), findsOneWidget);
  });

  testWidgets('theme is forced dark with the NP4 palette', (tester) async {
    final theme = buildNp4Theme();
    expect(theme.colorScheme.primary, const Color(0xFF34D399));
    expect(theme.scaffoldBackgroundColor, const Color(0xFF0A0F1C));
  });
}
