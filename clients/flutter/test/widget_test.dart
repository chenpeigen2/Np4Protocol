import 'package:flutter_test/flutter_test.dart';
import 'package:np4_client/main.dart';

void main() {
  testWidgets('connect screen renders', (tester) async {
    await tester.pumpWidget(const Np4App());
    expect(find.text('NP4 匿名聊天'), findsWidgets);
    expect(find.text('连接'), findsOneWidget);
  });
}
