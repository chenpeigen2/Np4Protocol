import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:np4_client/bridge/fake_transport.dart';
import 'package:np4_client/bridge/np4_bridge.dart';

void main() {
  test('Np4Client connects, publishes keys, and round-trips over mix',
      () async {
    final alice = await Np4Client.connect(
      Np4Config(identityPath: '/tmp/a', bootstrap: '/ip4/1.2.3.4/tcp/1/p2p/x'),
      transport: FakeTransport(),
    );
    final bob = await Np4Client.connect(
      Np4Config(identityPath: '/tmp/b', bootstrap: '/ip4/1.2.3.4/tcp/1/p2p/x'),
      transport: FakeTransport(),
    );

    final received = <Np4Incoming>[];
    final sub = bob.messages.listen(received.add);
    addTearDown(sub.cancel);

    await alice.send(bob.peerId, '你好，mix');
    await Future<void>.delayed(const Duration(milliseconds: 50));

    expect(received, hasLength(1));
    expect(received.first.sender, 'anonymous');
    expect(received.first.content, '你好，mix');
  });

  test('connect publishes keys when bootstrapped', () async {
    final t = FakeTransport();
    final published = <String>[];
    // The fake accepts any call; assert publish_keys was issued by sending
    // through a transport wrapper that records methods.
    final client = await Np4Client.connect(
      Np4Config(identityPath: '/tmp/a', bootstrap: '/ip4/1.2.3.4/tcp/1/p2p/x'),
      transport: _RecordingTransport(t, published),
    );
    expect(published, contains('publish_keys'));
    expect(client.peerId, startsWith('FAKE_PEER_'));
  });

  test('connect skips publish_keys in direct-only mode', () async {
    final t = FakeTransport();
    final published = <String>[];
    final client = await Np4Client.connect(
      const Np4Config(identityPath: '/tmp/a', bootstrap: ''),
      transport: _RecordingTransport(t, published),
    );
    expect(published, isNot(contains('publish_keys')));
    // Mix send without routing hard-fails — the anonymity contract.
    await expectLater(
      client.send('FAKE_PEER_1', 'hi'),
      throwsA(isA<Np4BridgeException>()),
    );
  });

  test('send content is base64-encoded into the transport call', () async {
    final t = FakeTransport();
    final seen = <Map<String, dynamic>>[];
    final client = await Np4Client.connect(
      Np4Config(identityPath: '/tmp/a', bootstrap: '/ip4/1.2.3.4/tcp/1/p2p/x'),
      transport: _CapturingTransport(t, seen),
    );
    await client.send('FAKE_PEER_1', 'hi');
    expect(seen.single['content_b64'], base64Encode(utf8.encode('hi')));
  });
}

class _RecordingTransport extends Np4TransportProxy {
  _RecordingTransport(super.inner, this.methods);
  final List<String> methods;

  @override
  Future<void> call(int handle, String method, Map<String, dynamic> args) {
    methods.add(method);
    return super.call(handle, method, args);
  }
}

class _CapturingTransport extends Np4TransportProxy {
  _CapturingTransport(super.inner, this.calls);
  final List<Map<String, dynamic>> calls;

  @override
  Future<void> call(int handle, String method, Map<String, dynamic> args) {
    if (method == 'send') calls.add(args);
    return super.call(handle, method, args);
  }
}
