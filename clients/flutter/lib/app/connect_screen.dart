import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

import '../bridge/np4_bridge.dart';
import '../bridge/np4_ffi.dart';
import 'chat_screen.dart';
import 'theme.dart';
import 'transitions.dart';

/// Build-time demo/dev hooks (flutter run --dart-define=...):
///   NP4_BOOTSTRAP=multiaddr  prefills the bootstrap field
///   NP4_AUTOCONNECT=1        connects immediately after launch
const _kEnvBootstrap = String.fromEnvironment('NP4_BOOTSTRAP');
// --dart-define=NP4_AUTOCONNECT=1: bool.fromEnvironment only parses the
// literal "true", so read the string and accept the common truthy spellings.
final _kEnvAutoConnect = const ['1', 'true']
    .contains(const String.fromEnvironment('NP4_AUTOCONNECT'));

/// First-run screen: point the client at a bootstrap node and join.
class ConnectScreen extends StatefulWidget {
  const ConnectScreen({super.key});

  @override
  State<ConnectScreen> createState() => _ConnectScreenState();
}

class _ConnectScreenState extends State<ConnectScreen>
    with TickerProviderStateMixin {
  final _bootstrapCtrl = TextEditingController();
  final _hopsCtrl = TextEditingController(text: '1');
  bool _connecting = false;
  String _status = '';

  // One ticker, three clocks: a repeating shield pulse, a one-shot entrance
  // for the staggered choreography, and a slow ambient glow drift.
  late final AnimationController _pulseFx = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 2200),
  );
  late final Animation<double> _pulse = Tween(begin: 0.0, end: 1.0).animate(
    CurvedAnimation(parent: _pulseFx, curve: Curves.easeInOut),
  );
  late final AnimationController _enterFx = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1150),
  );
  late final AnimationController _glowFx = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 9000),
  );
  late final Animation<double> _glow = Tween(begin: 0.0, end: 1.0).animate(
    CurvedAnimation(parent: _glowFx, curve: Curves.easeInOut),
  );

  @override
  void initState() {
    super.initState();
    _pulseFx.repeat(reverse: true);
    _glowFx.repeat(reverse: true);
    _enterFx.forward();
    if (_kEnvBootstrap.isNotEmpty) {
      _bootstrapCtrl.text = _kEnvBootstrap;
    }
    if (_kEnvAutoConnect && _kEnvBootstrap.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _connect());
    }
  }

  @override
  void dispose() {
    _enterFx.dispose();
    _glowFx.dispose();
    _pulseFx.dispose();
    _bootstrapCtrl.dispose();
    _hopsCtrl.dispose();
    super.dispose();
  }

  Future<void> _connect() async {
    final bootstrap = _bootstrapCtrl.text.trim();
    if (bootstrap.isEmpty) {
      _showError('请填写 bootstrap 节点的 multiaddr');
      return;
    }
    final hops = int.tryParse(_hopsCtrl.text.trim()) ?? 1;
    setState(() {
      _connecting = true;
      _status = '正在创建节点（加载身份、启动 libp2p）…';
    });

    try {
      final support = await getApplicationSupportDirectory();
      final client = await connectFfi(Np4Config(
        identityPath: '${support.path}/np4_identity',
        bootstrap: bootstrap,
        hops: hops,
      ));
      debugPrint('[np4] connected as ${client.peerId}');
      setState(() => _status = '已连接，正在向 DHT 发布密钥（对方需要它才能寻址你）…');
      // publish_keys already ran inside connect(); give the DHT a moment and
      // hand off. Cold-start: path selection retries up to 25s on first send.
      if (!mounted) return;
      Navigator.of(context).pushReplacement(FadeSlideRoute(
        builder: (_) => ChatScreen(client: client, bootstrap: bootstrap),
      ));
    } catch (e) {
      if (!mounted) return;
      _showError('连接失败：$e');
      setState(() {
        _connecting = false;
        _status = '';
      });
    }
  }

  void _showError(String message) {
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Container(
        // A single soft glow at the top — it drifts, slowly, so the screen
        // never feels frozen. The only ornament here.
        decoration: BoxDecoration(
          gradient: RadialGradient(
            center: Alignment.lerp(const Alignment(-0.9, -1.3),
                const Alignment(0.7, -1.1), _glow.value)!,
            radius: 1.4,
            colors: const [Color(0x1434D399), Colors.transparent],
            stops: const [0, 0.55],
          ),
        ),
        child: SafeArea(
          child: LayoutBuilder(
            builder: (ctx, constraints) => SingleChildScrollView(
              child: ConstrainedBox(
                constraints: BoxConstraints(minHeight: constraints.maxHeight),
                child: Center(
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 480),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 24),
                      child: Column(
                        mainAxisAlignment: MainAxisAlignment.center,
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          const SizedBox(height: 24),
                          StaggerIn(
                            fx: _enterFx,
                            interval: const Interval(0, 0.35,
                                curve: Curves.easeOutCubic),
                            child: Center(
                              child: AnimatedBuilder(
                                animation: _pulse,
                                builder: (ctx, _) {
                                  final t = _pulse.value;
                                  return Container(
                                    width: 92 + 26 * t,
                                    height: 92 + 26 * t,
                                    decoration: BoxDecoration(
                                      shape: BoxShape.circle,
                                      border: Border.all(
                                        color: Np4Colors.accent.withValues(
                                            alpha: 0.28 * (1 - t)),
                                        width: 1.4,
                                      ),
                                    ),
                                    child: Center(
                                      child: Container(
                                        width: 92,
                                        height: 92,
                                        decoration: BoxDecoration(
                                          shape: BoxShape.circle,
                                          color: Np4Colors.accentContainer,
                                          border: Border.all(
                                            color: Np4Colors.accent
                                                .withValues(alpha: 0.35),
                                            width: 1.2,
                                          ),
                                        ),
                                        child: const Icon(Icons.shield_outlined,
                                            size: 44, color: Np4Colors.accent),
                                      ),
                                    ),
                                  );
                                },
                              ),
                            ),
                          ),
                          const SizedBox(height: 24),
                          StaggerIn(
                            fx: _enterFx,
                            interval: const Interval(0.18, 0.5,
                                curve: Curves.easeOutCubic),
                            child: const Text(
                              'NP4 匿名聊天',
                              textAlign: TextAlign.center,
                              style: TextStyle(
                                color: Np4Colors.textPrimary,
                                fontSize: 26,
                                fontWeight: FontWeight.w700,
                                letterSpacing: 0.3,
                              ),
                            ),
                          ),
                          const SizedBox(height: 10),
                          StaggerIn(
                            fx: _enterFx,
                            interval: const Interval(0.28, 0.6,
                                curve: Curves.easeOutCubic),
                            child: const Text(
                              '定长 cell · 洋葱路由 · 批量混洗\n对方看到的只有一个词：anonymous',
                              textAlign: TextAlign.center,
                              style: TextStyle(
                                color: Np4Colors.textMuted,
                                fontSize: 14,
                                height: 1.6,
                              ),
                            ),
                          ),
                          const SizedBox(height: 32),
                          StaggerIn(
                            fx: _enterFx,
                            interval: const Interval(0.4, 0.85,
                                curve: Curves.easeOutCubic),
                            child: Container(
                              padding: const EdgeInsets.all(20),
                              decoration: BoxDecoration(
                                color: Np4Colors.surface,
                                borderRadius: BorderRadius.circular(20),
                                border: Border.all(color: Np4Colors.border),
                              ),
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  const _FieldLabel('Bootstrap 节点'),
                                  TextField(
                                    controller: _bootstrapCtrl,
                                    enabled: !_connecting,
                                    style: const TextStyle(
                                        fontSize: 13,
                                        color: Np4Colors.textPrimary),
                                    decoration: const InputDecoration(
                                      hintText:
                                          '/ip4/203.0.113.7/tcp/4000/p2p/12D3…',
                                      prefixIcon: Icon(Icons.hub_outlined,
                                          size: 20,
                                          color: Np4Colors.textFaint),
                                      prefixIconConstraints: BoxConstraints(
                                          minWidth: 40, minHeight: 20),
                                    ),
                                  ),
                                  const Padding(
                                    padding: EdgeInsets.only(left: 4, top: 6),
                                    child: Text(
                                      '服务器上 ./bootstrap start 输出的地址；云服务器把内网 IP 换成公网 IP',
                                      style: TextStyle(
                                          color: Np4Colors.textFaint,
                                          fontSize: 11.5),
                                    ),
                                  ),
                                  const SizedBox(height: 18),
                                  const _FieldLabel('洋葱跳数'),
                                  TextField(
                                    controller: _hopsCtrl,
                                    enabled: !_connecting,
                                    keyboardType: TextInputType.number,
                                    style: const TextStyle(
                                        color: Np4Colors.textPrimary),
                                    decoration: const InputDecoration(
                                      prefixIcon: Icon(Icons.route_outlined,
                                          size: 20, color: Np4Colors.textFaint),
                                      prefixIconConstraints: BoxConstraints(
                                          minWidth: 40, minHeight: 20),
                                    ),
                                  ),
                                  const Padding(
                                    padding: EdgeInsets.only(left: 4, top: 6),
                                    child: Text(
                                      '需 ≤ 在线 relay 数；bootstrap 兼任 relay 的单服务器部署用 1',
                                      style: TextStyle(
                                          color: Np4Colors.textFaint,
                                          fontSize: 11.5),
                                    ),
                                  ),
                                  const SizedBox(height: 22),
                                  FilledButton(
                                    onPressed: _connecting ? null : _connect,
                                    child: _connecting
                                        ? const SizedBox(
                                            width: 20,
                                            height: 20,
                                            child: CircularProgressIndicator(
                                                strokeWidth: 2,
                                                color: Np4Colors.onAccent),
                                          )
                                        : const Row(
                                            mainAxisSize: MainAxisSize.min,
                                            children: [
                                              Text('进入匿名网络'),
                                              SizedBox(width: 8),
                                              Icon(Icons.arrow_forward_rounded,
                                                  size: 18),
                                            ],
                                          ),
                                  ),
                                ],
                              ),
                            ),
                          ),
                          if (_status.isNotEmpty) ...[
                            const SizedBox(height: 16),
                            Row(
                              mainAxisAlignment: MainAxisAlignment.center,
                              children: [
                                const SizedBox(
                                  width: 12,
                                  height: 12,
                                  child: CircularProgressIndicator(
                                      strokeWidth: 1.6,
                                      color: Np4Colors.accent),
                                ),
                                const SizedBox(width: 8),
                                Flexible(
                                  child: Text(
                                    _status,
                                    style: const TextStyle(
                                        color: Np4Colors.textMuted,
                                        fontSize: 12.5),
                                  ),
                                ),
                              ],
                            ),
                          ],
                          const SizedBox(height: 28),
                          StaggerIn(
                            fx: _enterFx,
                            interval: const Interval(0.55, 0.95,
                                curve: Curves.easeOutCubic),
                            child: const Text(
                              '消息为尽力送达，对方离线即丢失；\n"已发送" 仅表示已进入匿名队列。',
                              textAlign: TextAlign.center,
                              style: TextStyle(
                                  color: Np4Colors.textFaint,
                                  fontSize: 12,
                                  height: 1.6),
                            ),
                          ),
                          const SizedBox(height: 16),
                        ],
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Small uppercase-feeling section label above each field.
class _FieldLabel extends StatelessWidget {
  const _FieldLabel(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(left: 4, bottom: 8),
      child: Text(
        text,
        style: const TextStyle(
          color: Np4Colors.textMuted,
          fontSize: 12.5,
          fontWeight: FontWeight.w600,
          letterSpacing: 0.4,
        ),
      ),
    );
  }
}
