import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../bridge/np4_bridge.dart';
import 'theme.dart';

class _ChatMessage {
  _ChatMessage({
    required this.sender,
    required this.text,
    required this.time,
    required this.mine,
    this.verified = false,
  });

  final String sender;
  final String text;
  final DateTime time;
  final bool mine;
  // Incoming only: the pairwise auth tag matched a known contact. Our own
  // echoed messages never carry it.
  final bool verified;

  String get shortSender =>
      sender.length > 12 ? '${sender.substring(0, 12)}…' : sender;
}

/// Mix-routed chat. The peer appears as "anonymous" unless the sender-auth
/// tag attributes them; our own messages are echoed locally. Delivery is
/// best-effort: offline peers lose the message.
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
  int _relayCount = 0;
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
      // Dedupe by peer ID, keep relays out of the contact picker — they are
      // infrastructure, and messaging the sole relay is impossible.
      final deduped = <String, PeerEntry>{};
      var relays = 0;
      for (final p in peers) {
        if (p.isRelay) {
          relays++;
        } else {
          deduped.putIfAbsent(p.peerId, () => p);
        }
      }
      setState(() {
        _relayCount = relays;
        _peers
          ..clear()
          ..addAll(deduped.values);
      });
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
    debugPrint(
        '[np4] message received from ${msg.sender} (verified=${msg.verified}): ${msg.content}');
    setState(() {
      _messages.add(_ChatMessage(
        sender: msg.sender,
        text: msg.content,
        time: DateTime.now(),
        mine: false,
        verified: msg.verified,
      ));
    });
    _scrollToBottom();
  }

  Future<void> _send() async {
    final text = _inputCtrl.text.trim();
    final dest = _destCtrl.text.trim();
    if (text.isEmpty || dest.isEmpty) return;
    // Delivery is best-effort: a destination outside the online list is most
    // likely dead or stale, and the message will be silently lost. Say so.
    if (!_peers.any((p) => p.peerId == dest)) {
      final go = await showDialog<bool>(
        context: context,
        builder: (_) => AlertDialog(
          title: const Text('可能无法送达'),
          content: Text(
              '对方 ${dest.substring(0, dest.length > 24 ? 24 : dest.length)}… 不在当前在线列表中：\n'
              '对方可能已离线，或地址已过期。\n'
              '消息仍会进入匿名队列，但大概率丢失。仍要发送吗？'),
          actions: [
            TextButton(
                onPressed: () => Navigator.of(context).pop(false),
                child: const Text('取消')),
            TextButton(
                onPressed: () => Navigator.of(context).pop(true),
                child: const Text('仍要发送')),
          ],
        ),
      );
      if (go != true) return;
    }
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

  // -- presentation ---------------------------------------------------------

  String get _statusLine => _relayCount == 0
      ? '无在线 relay — 发送会失败'
      : '联系人 ${_peers.length} · relay $_relayCount · 尽力送达';

  void _showPeerPicker() {
    showModalBottomSheet<void>(
      context: context,
      builder: (sheetCtx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 0, 20, 8),
              child: Row(
                children: [
                  const Expanded(
                    child: Text('在线联系人',
                        style: TextStyle(
                            color: Np4Colors.textPrimary,
                            fontSize: 16,
                            fontWeight: FontWeight.w600)),
                  ),
                  Text('${_peers.length}',
                      style: const TextStyle(
                          color: Np4Colors.textFaint, fontSize: 13)),
                ],
              ),
            ),
            const Divider(),
            if (_peers.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 28),
                child: Text('没有在线联系人',
                    style: TextStyle(color: Np4Colors.textFaint, fontSize: 13)),
              )
            else
              Flexible(
                child: ListView.builder(
                  shrinkWrap: true,
                  itemCount: _peers.length,
                  itemBuilder: (ctx, i) {
                    final p = _peers[i];
                    return ListTile(
                      leading: CircleAvatar(
                        radius: 18,
                        backgroundColor: Np4Colors.accentContainer,
                        child: Text(p.peerId.substring(8, 9).toUpperCase(),
                            style: const TextStyle(
                                color: Np4Colors.accent,
                                fontSize: 14,
                                fontWeight: FontWeight.w600)),
                      ),
                      title: Text(
                        '${p.peerId.substring(0, 16)}…',
                        style: const TextStyle(
                            color: Np4Colors.textPrimary, fontSize: 14),
                      ),
                      trailing: const Icon(Icons.chevron_right,
                          color: Np4Colors.textFaint),
                      onTap: () {
                        Navigator.of(sheetCtx).pop();
                        setState(() => _destCtrl.text = p.peerId);
                      },
                    );
                  },
                ),
              ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
  }

  Widget _bubble(_ChatMessage m) {
    final mine = m.mine;
    return Align(
      alignment: mine ? Alignment.centerRight : Alignment.centerLeft,
      child: TweenAnimationBuilder<double>(
        tween: Tween(begin: 0.6, end: 1),
        duration: const Duration(milliseconds: 160),
        curve: Curves.easeOut,
        builder: (ctx, opacity, child) =>
            Opacity(opacity: opacity, child: child),
        child: Container(
          margin: const EdgeInsets.symmetric(vertical: 5, horizontal: 14),
          padding: const EdgeInsets.fromLTRB(14, 9, 14, 7),
          constraints: BoxConstraints(
              maxWidth: MediaQuery.of(context).size.width * 0.78),
          decoration: BoxDecoration(
            color: mine ? Np4Colors.accentContainer : Np4Colors.surfaceHigh,
            border: Border.all(
              color: mine
                  ? Np4Colors.accent.withValues(alpha: 0.28)
                  : Np4Colors.border,
            ),
            borderRadius: BorderRadius.only(
              topLeft: const Radius.circular(18),
              topRight: const Radius.circular(18),
              bottomLeft: Radius.circular(mine ? 18 : 5),
              bottomRight: Radius.circular(mine ? 5 : 18),
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (!mine) ...[
                Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      m.verified ? Icons.verified : Icons.error_outline,
                      size: 13,
                      color: m.verified ? Np4Colors.accent : Np4Colors.warn,
                    ),
                    const SizedBox(width: 4),
                    Text(
                      m.verified ? '已验证 · ${m.shortSender}' : '未验证来源',
                      style: TextStyle(
                        fontSize: 11,
                        fontWeight: FontWeight.w500,
                        color: m.verified ? Np4Colors.accent : Np4Colors.warn,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 4),
              ],
              Text(
                m.text,
                style: const TextStyle(
                    color: Np4Colors.textPrimary,
                    fontSize: 15,
                    height: 1.45),
              ),
              const SizedBox(height: 3),
              Align(
                alignment: Alignment.centerRight,
                child: Text(
                  '${m.time.hour.toString().padLeft(2, '0')}:${m.time.minute.toString().padLeft(2, '0')}',
                  style: const TextStyle(
                      color: Np4Colors.textFaint, fontSize: 10),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget get _emptyState {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 72,
            height: 72,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              color: Np4Colors.surface,
              border: Border.all(color: Np4Colors.border),
            ),
            child: const Icon(Icons.forum_outlined,
                size: 30, color: Np4Colors.textFaint),
          ),
          const SizedBox(height: 16),
          const Text('还没有消息',
              style: TextStyle(
                  color: Np4Colors.textPrimary,
                  fontSize: 15,
                  fontWeight: FontWeight.w600)),
          const SizedBox(height: 6),
          const Text('在下方选择在线联系人，开始匿名对话',
              style: TextStyle(color: Np4Colors.textFaint, fontSize: 12.5)),
        ],
      ),
    );
  }

  Widget get _composer {
    final dest = _destCtrl.text.trim();
    final destKnown = _peers.any((p) => p.peerId == dest);
    return Container(
      decoration: const BoxDecoration(
        color: Np4Colors.surface,
        border: Border(top: BorderSide(color: Np4Colors.border)),
      ),
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 12),
      child: Column(
        children: [
          Row(
            children: [
              Expanded(
                child: InkWell(
                  borderRadius: BorderRadius.circular(14),
                  onTap: _showPeerPicker,
                  child: Container(
                    height: 42,
                    padding: const EdgeInsets.symmetric(horizontal: 12),
                    decoration: BoxDecoration(
                      color: Np4Colors.surfaceHigh,
                      borderRadius: BorderRadius.circular(14),
                      border: Border.all(
                        color: dest.isEmpty
                            ? Np4Colors.border
                            : (destKnown
                                ? Np4Colors.accent.withValues(alpha: 0.45)
                                : Np4Colors.warn.withValues(alpha: 0.45)),
                      ),
                    ),
                    child: Row(
                      children: [
                        Icon(Icons.person_outline,
                            size: 18,
                            color: dest.isEmpty
                                ? Np4Colors.textFaint
                                : Np4Colors.accent),
                        const SizedBox(width: 8),
                        Expanded(
                          child: Text(
                            dest.isEmpty
                                ? '选择在线联系人'
                                : '${dest.substring(0, dest.length > 20 ? 20 : dest.length)}…',
                            style: TextStyle(
                              fontSize: 13,
                              color: dest.isEmpty
                                  ? Np4Colors.textFaint
                                  : Np4Colors.textPrimary,
                            ),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        const Icon(Icons.expand_more,
                            size: 18, color: Np4Colors.textFaint),
                      ],
                    ),
                  ),
                ),
              ),
              IconButton(
                icon: const Icon(Icons.refresh, size: 20),
                color: Np4Colors.textMuted,
                tooltip: '刷新在线节点',
                onPressed: _refreshPeers,
              ),
            ],
          ),
          const SizedBox(height: 8),
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(
                child: TextField(
                  controller: _inputCtrl,
                  enabled: !_sending,
                  onSubmitted: (_) => _send(),
                  style: const TextStyle(
                      color: Np4Colors.textPrimary, fontSize: 15),
                  minLines: 1,
                  maxLines: 4,
                  textInputAction: TextInputAction.send,
                  decoration: const InputDecoration(
                    hintText: '输入消息…',
                    contentPadding:
                        EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Material(
                color:
                    _sending ? Np4Colors.surfaceHigh : Np4Colors.accent,
                borderRadius: BorderRadius.circular(24),
                child: InkWell(
                  borderRadius: BorderRadius.circular(24),
                  onTap: _sending ? null : _send,
                  child: const SizedBox(
                    width: 46,
                    height: 46,
                    child: Icon(Icons.arrow_upward_rounded,
                        color: Np4Colors.onAccent, size: 24),
                  ),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('NP4 匿名聊天'),
            const SizedBox(height: 2),
            Row(
              children: [
                Container(
                  width: 6,
                  height: 6,
                  decoration: BoxDecoration(
                    shape: BoxShape.circle,
                    color: _relayCount == 0 ? Np4Colors.danger : Np4Colors.accent,
                  ),
                ),
                const SizedBox(width: 6),
                Text(
                  _statusLine,
                  style: TextStyle(
                    fontSize: 11,
                    color: _relayCount == 0
                        ? Np4Colors.danger
                        : Np4Colors.textMuted,
                  ),
                ),
              ],
            ),
          ],
        ),
        actions: [
          IconButton(
            icon: const Icon(Icons.info_outline, size: 20),
            color: Np4Colors.textMuted,
            tooltip: '我的 Peer ID',
            onPressed: () {
              showDialog<void>(
                context: context,
                builder: (_) => AlertDialog(
                  title: const Text('我的 Peer ID'),
                  content: SelectableText(
                    widget.client.peerId,
                    style: const TextStyle(
                        color: Np4Colors.textPrimary,
                        fontSize: 12.5,
                        fontFamily: 'monospace'),
                  ),
                  actions: [
                    TextButton(
                      onPressed: () {
                        Clipboard.setData(
                            ClipboardData(text: widget.client.peerId));
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
          Expanded(
            child: _messages.isEmpty
                ? _emptyState
                : ListView.builder(
                    controller: _scrollCtrl,
                    padding: const EdgeInsets.symmetric(vertical: 10),
                    itemCount: _messages.length,
                    itemBuilder: (context, i) => _bubble(_messages[i]),
                  ),
          ),
          SafeArea(top: false, child: _composer),
        ],
      ),
    );
  }
}
