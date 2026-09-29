import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

import '../bridge/np4_bridge.dart';
import '../bridge/np4_ffi.dart';
import 'chat_screen.dart';

/// Build-time demo/dev hooks (flutter run --dart-define=...):
///   NP4_BOOTSTRAP=multiaddr  prefills the bootstrap field
///   NP4_AUTOCONNECT=1        connects immediately after launch
const _kEnvBootstrap = String.fromEnvironment('NP4_BOOTSTRAP');
const _kEnvAutoConnect = bool.fromEnvironment('NP4_AUTOCONNECT');

/// First-run screen: point the client at a bootstrap node and join.
class ConnectScreen extends StatefulWidget {
  const ConnectScreen({super.key});

  @override
  State<ConnectScreen> createState() => _ConnectScreenState();
}

class _ConnectScreenState extends State<ConnectScreen> {
  final _bootstrapCtrl = TextEditingController();
  final _hopsCtrl = TextEditingController(text: '1');
  bool _connecting = false;
  String _status = '';

  @override
  void initState() {
    super.initState();
    if (_kEnvBootstrap.isNotEmpty) {
      _bootstrapCtrl.text = _kEnvBootstrap;
    }
    if (_kEnvAutoConnect && _kEnvBootstrap.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _connect());
    }
  }

  @override
  void dispose() {
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
      Navigator.of(context).pushReplacement(MaterialPageRoute(
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
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 480),
            child: ListView(
              shrinkWrap: true,
              padding: const EdgeInsets.all(24),
              children: [
                Icon(Icons.shield_outlined,
                    size: 64, color: Theme.of(context).colorScheme.primary),
                const SizedBox(height: 12),
                Text('NP4 匿名聊天',
                    textAlign: TextAlign.center,
                    style: Theme.of(context).textTheme.headlineSmall),
                const SizedBox(height: 8),
                const Text(
                  '消息经定长 cell + 洋葱路由 + 批量混洗发送，'
                  '对方只能看到 "anonymous"。',
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 32),
                TextField(
                  controller: _bootstrapCtrl,
                  enabled: !_connecting,
                  decoration: const InputDecoration(
                    labelText: 'Bootstrap 节点 multiaddr',
                    hintText: '/ip4/203.0.113.7/tcp/4000/p2p/12D3KooW...',
                    border: OutlineInputBorder(),
                    helperText: '服务器上 ./bootstrap start 输出的地址；'
                        '云服务器需把内网 IP 换成公网 IP',
                  ),
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: _hopsCtrl,
                  enabled: !_connecting,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(
                    labelText: '洋葱跳数（hops）',
                    border: OutlineInputBorder(),
                    helperText: '需 ≤ 在线 relay 数；'
                        'bootstrap 兼任 relay 的单服务器部署用 1',
                  ),
                ),
                const SizedBox(height: 24),
                FilledButton(
                  onPressed: _connecting ? null : _connect,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 12),
                    child: _connecting
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Text('连接'),
                  ),
                ),
                if (_status.isNotEmpty) ...[
                  const SizedBox(height: 16),
                  Text(_status, textAlign: TextAlign.center),
                ],
                const SizedBox(height: 24),
                Text(
                  '提示：消息为尽力送达，对方离线即丢失；'
                  '"已发送" 仅表示已进入匿名队列。',
                  style: Theme.of(context).textTheme.bodySmall,
                  textAlign: TextAlign.center,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
