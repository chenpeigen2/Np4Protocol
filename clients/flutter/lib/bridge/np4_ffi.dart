import 'dart:async';
import 'dart:convert';
import 'dart:isolate';

import 'np4_bridge.dart';
import 'np4_ffi_raw.dart';

/// FFI transport over the native np4bridge library.
///
/// All native calls execute on a dedicated engine isolate so the UI thread
/// never blocks on DHT/onion work (publish_keys can block for seconds while
/// the DHT routing table warms up; Send builds onions and touches the mix).
/// Events are polled on the engine (25ms) and pushed to the UI isolate as
/// ordinary Dart messages — no native callbacks, no pointer lifetimes to
/// reason about.
class FfiTransport implements Np4Transport {
  FfiTransport._(this._inbox, this._replies, this._isolate) {
    _replySub = _replies.listen(_onMessage);
  }

  static Future<FfiTransport> start() async {
    final ready = ReceivePort();
    final replies = ReceivePort();
    final isolate =
        await Isolate.spawn(_engineMain, [ready.sendPort, replies.sendPort]);
    final inbox = await ready.first as SendPort;
    ready.close();
    return FfiTransport._(inbox, replies, isolate);
  }

  final SendPort _inbox;
  final ReceivePort _replies;
  final Isolate _isolate;
  late final StreamSubscription _replySub;

  var _nextId = 0;
  final _pending = <int, Completer<Map<String, dynamic>>>{};
  final _events = StreamController<Np4Incoming>.broadcast();

  @override
  Stream<Np4Incoming> get events => _events.stream;

  void _onMessage(dynamic raw) {
    final msg = raw as Map;
    if (msg.containsKey('event')) {
      final ev = (msg['event'] as Map).cast<String, dynamic>();
      _events.add(Np4Incoming(
        sender: (ev['sender'] as String?) ?? 'anonymous',
        // The bridge JSON carries content as base64 (go/pkg/bridge).
        content:
            utf8.decode(base64Decode(ev['content_b64'] as String? ?? '')),
      ));
      return;
    }
    final completer = _pending.remove(msg['id']);
    if (completer == null) return;
    if (msg['ok'] == true) {
      completer
          .complete((msg['result'] as Map?)?.cast<String, dynamic>() ?? {});
    } else {
      completer.completeError(Np4BridgeException(msg['error'] as String));
    }
  }

  Future<Map<String, dynamic>> _request(String op, Map<String, dynamic> payload) {
    final id = ++_nextId;
    final completer = Completer<Map<String, dynamic>>();
    _pending[id] = completer;
    _inbox.send({'id': id, 'op': op, 'payload': payload});
    return completer.future;
  }

  @override
  Future<Np4NodeInfo> create(Np4Config config) async {
    final result = await _request('create', config.toJson());
    return Np4NodeInfo(
      handle: result['handle'] as int,
      peerId: result['peer_id'] as String,
      addrs: List<String>.from(result['addrs'] as List),
    );
  }

  @override
  Future<Map<String, dynamic>> call(
          int handle, String method, Map<String, dynamic> args) =>
      _request('call', {'handle': handle, 'method': method, 'args': args});

  @override
  Future<void> stop(int handle) => _request('stop', {'handle': handle});

  @override
  Future<void> dispose() async {
    _replySub.cancel();
    await _events.close();
    _isolate.kill(priority: Isolate.immediate);
  }
}

/// Engine isolate main: owns the Np4Lib instance and every node handle.
/// Native calls are synchronous here; replies and events cross back to the
/// UI isolate via SendPort messages.
Future<void> _engineMain(List caps) async {
  final ready = caps[0] as SendPort;
  final main = caps[1] as SendPort;
  final lib = Np4Lib.instance();
  final inbox = ReceivePort();
  ready.send(inbox.sendPort);

  final handles = <int>[];

  inbox.listen((raw) {
    final req = (raw as Map).cast<String, dynamic>();
    final id = req['id'] as int;
    final op = req['op'] as String;
    try {
      switch (op) {
        case 'create':
          final result =
              lib.create((req['payload'] as Map).cast<String, dynamic>());
          handles.add(result['handle'] as int);
          main.send({'id': id, 'ok': true, 'result': result});
        case 'call':
          final payload = (req['payload'] as Map).cast<String, dynamic>();
          final result = lib.call(payload['handle'] as int, {
            'method': payload['method'],
            'args': payload['args'] ?? {},
          });
          main.send({'id': id, 'ok': true, 'result': result});
        case 'stop':
          final payload = (req['payload'] as Map).cast<String, dynamic>();
          final handle = payload['handle'] as int;
          lib.stop(handle);
          handles.remove(handle);
          main.send({'id': id, 'ok': true, 'result': {}});
        default:
          main.send({'id': id, 'ok': false, 'error': 'unknown op $op'});
      }
    } catch (e) {
      main.send({'id': id, 'ok': false, 'error': e.toString()});
    }
  });

  // Drain each node's event queue. Mix latency dominates (>= 700ms end to
  // end), so a 25ms poll adds nothing perceptible while keeping the FFI
  // strictly synchronous.
  Timer.periodic(const Duration(milliseconds: 25), (_) {
    for (final handle in List<int>.of(handles)) {
      try {
        final result = lib.call(handle, {'method': 'poll', 'args': {}});
        for (final ev in (result['events'] as List? ?? [])) {
          main.send({'event': ev, 'handle': handle});
        }
      } catch (_) {
        // The node may be mid-stop; the next tick handles the rest.
      }
    }
  });
}

/// Convenience: an [Np4Client] over the real native library.
Future<Np4Client> connectFfi(Np4Config config) async {
  final transport = await FfiTransport.start();
  try {
    return await Np4Client.connect(config, transport: transport);
  } catch (_) {
    await transport.dispose();
    rethrow;
  }
}
