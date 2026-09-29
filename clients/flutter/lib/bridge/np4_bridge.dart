import 'dart:async';
import 'dart:convert';

/// Configuration for creating an np4 node (mirrors go/pkg/bridge.Config).
class Np4Config {
  const Np4Config({
    this.port = 0,
    required this.identityPath,
    required this.bootstrap,
    this.hops = 1,
    this.rendezvous = 'np4-network',
  });

  /// TCP listen port; 0 picks a random one.
  final int port;

  /// Persistent identity file path. Must be a writable, app-private location
  /// (use the app support directory). Losing it changes the peer ID.
  final String identityPath;

  /// Bootstrap node multiaddr, e.g.
  /// `/ip4/203.0.113.7/tcp/4000/p2p/12D3KooW...`. Required for mix mode;
  /// an empty string creates a direct-only node whose Send always fails.
  final String bootstrap;

  /// Onion path length. Must be ≤ number of online relays. With the
  /// bootstrap-as-relay single-server deployment use 1 (the default here).
  final int hops;

  final String rendezvous;

  Map<String, dynamic> toJson() => {
        'port': port,
        'identity_path': identityPath,
        'bootstrap': bootstrap,
        'hops': hops,
        'rendezvous': rendezvous,
      };
}

/// A message delivered to us. `sender` is always "anonymous" on the mix path —
/// that is the point.
class Np4Incoming {
  Np4Incoming({required this.sender, required this.content, this.timestamp});

  final String sender;
  final String content;
  final DateTime? timestamp;

  @override
  String toString() => 'Np4Incoming($sender: $content)';
}

/// Result of creating a node.
class Np4NodeInfo {
  Np4NodeInfo({required this.handle, required this.peerId, required this.addrs});

  final int handle;
  final String peerId;
  final List<String> addrs;
}

/// Error surfaced by the native bridge ({"ok":false,"error":...} envelope).
class Np4BridgeException implements Exception {
  Np4BridgeException(this.message);
  final String message;

  @override
  String toString() => 'Np4BridgeException: $message';
}

/// Transport abstraction over the native bridge. The FFI implementation
/// (np4_ffi.dart) talks to the Go library; the fake (fake_transport.dart)
/// is an in-memory loopback for tests. UI code only ever sees Np4Client.
abstract class Np4Transport {
  Future<Np4NodeInfo> create(Np4Config config);

  /// Runs one bridge method and returns the parsed result map.
  Future<Map<String, dynamic>> call(
      int handle, String method, Map<String, dynamic> args);

  Future<void> stop(int handle);

  /// Events from all handles owned by this transport. Currently one event
  /// type: `message` (with sender/content).
  Stream<Np4Incoming> get events;

  /// Releases the transport (kills the engine isolate if there is one).
  Future<void> dispose();
}

/// Forwards every call to an inner transport — base class for wrappers
/// (recording/mocking in tests, instrumentation later).
abstract base class Np4TransportProxy implements Np4Transport {
  Np4TransportProxy(this.inner);

  final Np4Transport inner;

  @override
  Future<Np4NodeInfo> create(Np4Config config) => inner.create(config);

  @override
  Future<Map<String, dynamic>> call(
          int handle, String method, Map<String, dynamic> args) =>
      inner.call(handle, method, args);

  @override
  Future<void> stop(int handle) => inner.stop(handle);

  @override
  Stream<Np4Incoming> get events => inner.events;

  @override
  Future<void> dispose() => inner.dispose();
}

/// High-level client for one np4 node. Mix sends only — there is no direct
/// fallback here, matching the protocol's hard-fail semantics.
class Np4Client {
  Np4Client._(this._transport, this._handle, this.peerId);

  final Np4Transport _transport;
  final int _handle;
  final String peerId;

  final _messages = StreamController<Np4Incoming>.broadcast();

  /// Incoming messages. Never closes on send errors; listen with onError.
  Stream<Np4Incoming> get messages => _messages.stream;

  /// Creates a node, and (when bootstrapped) publishes our key so peers can
  /// address us. Key publication needs a non-empty DHT routing table, so in a
  /// fresh network this may take a few seconds while the DHT warms up.
  /// Pass the FFI transport (see np4_ffi.dart) or a fake in tests.
  static Future<Np4Client> connect(Np4Config config,
      {required Np4Transport transport}) async {
    final info = await transport.create(config);
    if (config.bootstrap.isNotEmpty) {
      await transport.call(info.handle, 'publish_keys', {});
    }
    final client = Np4Client._(transport, info.handle, info.peerId);
    client._sub = transport.events.listen(client._messages.add);
    return client;
  }

  StreamSubscription<Np4Incoming>? _sub;

  /// Mix send. Returns when the packet is accepted into the entry mix — NOT
  /// an end-to-end delivery guarantee (that needs the [v2] return-path ACK).
  Future<void> send(String destPeerId, String text) {
    final content = base64Encode(utf8.encode(text));
    return _transport
        .call(_handle, 'send', {'dest': destPeerId, 'content_b64': content});
  }

  /// Blocks until everything queued in the entry mix has been dispatched to
  /// the first relay. One-shot command flows should call this before exit.
  Future<void> waitFlushed({int timeoutMs = 30000}) =>
      _transport.call(_handle, 'wait_flushed', {'timeout_ms': timeoutMs});

  Future<Np4NodeInfo> info() async {
    final res = await _transport.call(_handle, 'info', {});
    return Np4NodeInfo(
      handle: _handle,
      peerId: (res['peer_id'] as String?) ?? peerId,
      addrs: List<String>.from(res['addrs'] as List? ?? []),
    );
  }

  Future<void> dispose() async {
    await _sub?.cancel();
    await _messages.close();
    await _transport.stop(_handle);
    await _transport.dispose();
  }
}
