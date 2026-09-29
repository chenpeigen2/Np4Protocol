import 'dart:convert';
import 'dart:ffi';
import 'dart:io';

import 'np4_bridge.dart' show Np4BridgeException;

/// Raw FFI bindings for the four np4bridge symbols (go/cmd/np4bridge).
/// Every call takes a UTF-8 JSON body and returns a NUL-terminated malloc'd
/// JSON envelope owned by the caller until np4_free. Pointers are only ever
/// used synchronously inside a call — that is why events are polled.
class Np4Lib {
  Np4Lib._() {
    _create = _lib
        .lookupFunction<Pointer<Uint8> Function(Pointer<Uint8>, Int32),
            Pointer<Uint8> Function(Pointer<Uint8>, int)>('np4_create');
    _call = _lib
        .lookupFunction<Pointer<Uint8> Function(Int64, Pointer<Uint8>, Int32),
            Pointer<Uint8> Function(int, Pointer<Uint8>, int)>('np4_call');
    _stop = _lib
        .lookupFunction<Void Function(Int64), void Function(int)>('np4_stop');
    _free = _lib.lookupFunction<Void Function(Pointer<Uint8>),
        void Function(Pointer<Uint8>)>('np4_free');
  }

  static Np4Lib? _instance;
  static Np4Lib instance() => _instance ??= Np4Lib._();

  late final DynamicLibrary _lib = _open();
  late final Pointer<Uint8> Function(Pointer<Uint8>, int) _create;
  late final Pointer<Uint8> Function(int, Pointer<Uint8>, int) _call;
  late final void Function(int) _stop;
  late final void Function(Pointer<Uint8>) _free;

  static DynamicLibrary _open() {
    if (Platform.isWindows) {
      return DynamicLibrary.open('np4bridge.dll');
    }
    if (Platform.isAndroid) {
      return DynamicLibrary.open('libnp4bridge.so');
    }
    if (Platform.isMacOS) {
      try {
        // The pod vendors the dylib into .app/Contents/Frameworks, which the
        // Runner's rpath (@executable_path/../Frameworks) resolves.
        return DynamicLibrary.open('libnp4bridge.dylib');
      } on ArgumentError {
        return DynamicLibrary.process();
      }
    }
    if (Platform.isLinux) {
      return DynamicLibrary.open('libnp4bridge.so');
    }
    throw UnsupportedError(
        'np4bridge: platform "${Platform.operatingSystem}" is not supported (iOS is deferred)');
  }

  Map<String, dynamic> create(Map<String, dynamic> config) =>
      _invoke((body, len) => _create(body, len), jsonEncode(config));

  Map<String, dynamic> call(int handle, Map<String, dynamic> request) =>
      _invoke((body, len) => _call(handle, body, len), jsonEncode(request));

  void stop(int handle) => _stop(handle);

  Map<String, dynamic> _invoke(
      Pointer<Uint8> Function(Pointer<Uint8>, int) fn, String body) {
    final bytes = utf8.encode(body);
    final buf = malloc<Uint8>(bytes.length + 1);
    buf[bytes.length] = 0;
    buf.asTypedList(bytes.length).setAll(0, bytes);
    final out = fn(buf, bytes.length);
    final text = _readCString(out);
    _free(out);
    malloc.free(buf);
    final env = jsonDecode(text) as Map<String, dynamic>;
    if (env['ok'] != true) {
      throw Np4BridgeException((env['error'] as String?) ?? 'unknown error');
    }
    return (env['result'] as Map?)?.cast<String, dynamic>() ?? {};
  }

  static String _readCString(Pointer<Uint8> p) {
    var n = 0;
    while (p[n] != 0) {
      n++;
    }
    return utf8.decode(p.asTypedList(n));
  }
}
