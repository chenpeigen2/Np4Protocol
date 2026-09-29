import 'dart:async';
import 'dart:convert';

import 'np4_bridge.dart';

/// In-memory loopback transport for widget/unit tests — no native library,
/// no isolate. All fake nodes share one static bus; a send to another fake
/// node's peer ID is delivered to that node's events stream.
class FakeTransport implements Np4Transport {
  static final _bus = StreamController<Map<String, dynamic>>.broadcast();
  static final _registered = <String>{};
  static int _nextHandle = 0;

  String _peerId = '';

  @override
  Future<Np4NodeInfo> create(Np4Config config) async {
    final handle = ++_nextHandle;
    _peerId = 'FAKE_PEER_$handle';
    _registered.add(_peerId);
    return Np4NodeInfo(handle: handle, peerId: _peerId, addrs: ['/fake/1']);
  }

  @override
  Future<void> call(int handle, String method, Map<String, dynamic> args) async {
    switch (method) {
      case 'send':
        final dest = args['dest'] as String;
        if (!_registered.contains(dest)) {
          throw Np4BridgeException('unknown dest $dest');
        }
        _bus.add({
          'to': dest,
          'sender': 'anonymous',
          'content_b64': args['content_b64'] as String,
        });
      case 'publish_keys':
      case 'wait_flushed':
      case 'info':
        return;
      default:
        throw Np4BridgeException('unknown method $method');
    }
  }

  @override
  Future<void> stop(int handle) async {
    _registered.remove(_peerId);
  }

  @override
  Stream<Np4Incoming> get events => _bus.stream
      .where((e) => e['to'] == _peerId)
      .map((e) => Np4Incoming(
            sender: e['sender'] as String,
            content:
                utf8.decode(base64Decode(e['content_b64'] as String)),
          ));

  @override
  Future<void> dispose() async {}
}
