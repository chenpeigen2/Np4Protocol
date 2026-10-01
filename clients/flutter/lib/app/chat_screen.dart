import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../bridge/np4_bridge.dart';

class _ChatMessage {
  _ChatMessage({required this.sender, required this.text, required this.time, required this.mine});

  final String sender;
  final String text;
  final DateTime time;
  final bool mine;
}

/// Mix-routed chat. The peer appears as "anonymous"; our own messages are
/// echoed locally. Delivery is best-effort: offline peers lose the message.
class ChatScreen extends StatefulWidget {
  const ChatScreen({super.key, required this.client, required this.bootstrap});

  final Np4Client client;
  final String bootstrap;

  @override
  State<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends State<ChatScreen> {
  final _inputCtrl = TextEditingController();
  final _scrollCtrl = ScrollController();
  final _messages = <_ChatMessage>[];
  final _destCtrl = TextEditingController();
  final _peers = <PeerEntry>[];
  StreamSubscription<Np4Incoming>? _sub;
  Timer? _peersTimer;
  bool _sending = false;

  @override
  void initState() {
    super.initState();
    _sub = widget.client.messages.listen(_onIncoming, onError: (Object e) {
      _toast('接收异常：$e');
    });
    _refreshPeers();
    // Peer liveness: discovery records churn as nodes join and leave.
    _peersTimer = Timer.periodic(
        const Duration(seconds: 30), (_) => _refreshPeers());
  }

  Future<void> _refreshPeers() async {
    try {
      final peers = await widget.client.listPeers();
      if (!mounted) return;
      // Relays are infrastructure, not chat contacts.
      setState(() => _peers
        ..clear()
        ..addAll(peers.where((p) => !p.isRelay)));
      debugPrint('[np4] peers online: ${_peers.length}');
    } catch (_) {
      // Transient DHT state; the next refresh retries.
    }
  }

  @override
  void dispose() {
    _peersTimer?.cancel();
    _sub?.cancel();
    _inputCtrl.dispose();
    _destCtrl.dispose();
    _scrollCtrl.dispose();
    widget.client.dispose();
    super.dispose();
  }

  void _onIncoming(Np4Incoming msg) {
    // Debug builds log delivery so attached tooling (flutter run) can verify
    // end-to-end flow without touching the UI.
    debugPrint('[np4] message received from ${msg.sender}: ${msg.content}');
    setState(() {
      _messages.add(_ChatMessage(
        sender: msg.sender,
        text: msg.content,
        time: DateTime.now(),
        mine: false,
      ));
    });
    _scrollToBottom();
  }

  Future<void> _send() async {
    final text = _inputCtrl.text.trim();
    final dest = _destCtrl.text.trim();
    if (text.isEmpty || dest.isEmpty) return;
    setState(() => _sending = true);
    try {
      await widget.client.send(dest, text);
      setState(() {
        _messages.add(_ChatMessage(
          sender: 'me',
          text: text,
          time: DateTime.now(),
          mine: true,
        ));
        _inputCtrl.clear();
      });
      _scrollToBottom();
    } catch (e) {
      _toast('发送失败：$e');
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  void _scrollToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scrollCtrl.hasClients) {
        _scrollCtrl.animateTo(
          _scrollCtrl.position.maxScrollExtent,
          duration: const Duration(milliseconds: 200),
          curve: Curves.easeOut,
        );
      }
    });
  }

  void _toast(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('NP4 匿名聊天'),
        actions: [
          IconButton(
            icon: const Icon(Icons.info_outline),
            tooltip: '我的 Peer ID',
            onPressed: () {
              showDialog<void>(
                context: context,
                builder: (_) => AlertDialog(
                  title: const Text('我的 Peer ID'),
                  content: SelectableText(widget.client.peerId),
                  actions: [
                    TextButton(
                      onPressed: () {
                        Clipboard.setData(ClipboardData(text: widget.client.peerId));
                        Navigator.of(context).pop();
                        _toast('已复制');
                      },
                      child: const Text('复制'),
                    ),
                  ],
                ),
              );
            },
          ),
        ],
      ),
      body: Column(
        children: [
          Material(
            color: Theme.of(context).colorScheme.surfaceContainerHighest,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
              child: Row(
                children: [
                  Icon(Icons.privacy_tip_outlined,
                      size: 16, color: Theme.of(context).colorScheme.primary),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      '匿名模式：对方显示为 anonymous；尽力送达，离线即丢',
                      style: Theme.of(context).textTheme.bodySmall,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 0),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _destCtrl,
                    decoration: const InputDecoration(
                      labelText: '对方的 Peer ID（可下拉选择在线节点）',
                      isDense: true,
                      border: OutlineInputBorder(),
                    ),
                  ),
                ),
                IconButton(
                  icon: const Icon(Icons.refresh),
                  tooltip: '刷新在线节点',
                  onPressed: _refreshPeers,
                ),
                DropdownButton<String>(
                  hint: const Text('在线节点'),
                  items: _peers
                      .map((p) => DropdownMenuItem<String>(
                            value: p.peerId,
                            child: Text(
                              '${p.peerId.substring(0, 18)}…',
                              overflow: TextOverflow.ellipsis,
                            ),
                          ))
                      .toList(),
                  onChanged: (id) {
                    if (id != null) {
                      setState(() => _destCtrl.text = id);
                    }
                  },
                ),
              ],
            ),
          ),
          Expanded(
            child: _messages.isEmpty
                ? const Center(child: Text('还没有消息。填入对方 Peer ID 开始聊天。'))
                : ListView.builder(
                    controller: _scrollCtrl,
                    padding: const EdgeInsets.all(12),
                    itemCount: _messages.length,
                    itemBuilder: (context, i) {
                      final m = _messages[i];
                      return Align(
                        alignment:
                            m.mine ? Alignment.centerRight : Alignment.centerLeft,
                        child: Container(
                          margin: const EdgeInsets.symmetric(vertical: 4),
                          padding: const EdgeInsets.fromLTRB(12, 8, 12, 6),
                          constraints: BoxConstraints(
                              maxWidth: MediaQuery.of(context).size.width * 0.75),
                          decoration: BoxDecoration(
                            color: m.mine
                                ? Theme.of(context).colorScheme.primaryContainer
                                : Theme.of(context).colorScheme.surfaceContainerHighest,
                            borderRadius: BorderRadius.circular(12),
                          ),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(m.mine ? '我' : m.sender,
                                  style: Theme.of(context).textTheme.labelSmall),
                              Text(m.text),
                              Align(
                                alignment: Alignment.centerRight,
                                child: Text(
                                  '${m.time.hour.toString().padLeft(2, '0')}:${m.time.minute.toString().padLeft(2, '0')}',
                                  style: Theme.of(context).textTheme.labelSmall,
                                ),
                              ),
                            ],
                          ),
                        ),
                      );
                    },
                  ),
          ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _inputCtrl,
                      enabled: !_sending,
                      onSubmitted: (_) => _send(),
                      decoration: const InputDecoration(
                        hintText: '输入消息…',
                        border: OutlineInputBorder(),
                        isDense: true,
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton.filled(
                    onPressed: _sending ? null : _send,
                    icon: _sending
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2))
                        : const Icon(Icons.send),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}
