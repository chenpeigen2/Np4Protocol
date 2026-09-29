# np4_client（Flutter 跨平台客户端）

Np4Protocol 的 GUI 客户端，覆盖 **Windows / macOS / Linux / Android**。**iOS 暂不支持**（产品决策；桥本身可扩展，见文末）。

```
UI（ConnectScreen / ChatScreen）
        │  Stream / async API
Np4Client（lib/bridge/np4_bridge.dart，纯 Dart，可测试）
        │  Np4Transport 接口
        ├── FfiTransport（np4_ffi.dart）── 常驻 engine isolate：FFI 调用 + 25ms 事件轮询
        │         │  dart:ffi（4 个符号）
        │   libnp4bridge（Go：go/cmd/np4bridge，cgo 导出）
        │         │  直接调用
        │   np4.Node（协议全栈：onion / cell / mix / p2p，与 CLI 共用同一实现）
        └── FakeTransport（fake_transport.dart）── 内存环回，widget 测试用，无原生库
```

## 集成原则

- **协议逻辑零复制**：客户端嵌入的是 `go/pkg/np4` 的同一份实现，CLI 和 GUI 永不漂移。
- **ABI 极小且稳定**：原生库只导出 `np4_create / np4_call / np4_stop / np4_free` 四个符号，参数与返回值全部是 JSON（内容 base64）。加功能 = 加 JSON 字段，ABI 不变。
- **事件用轮询，不用回调**：跨运行时的原生回调有指针生命周期陷阱（Dart 异步消费时内存可能已被 Go 释放）。轮询有界队列（1024 条，满则丢最旧并计数）让 FFI 严格同步，Dart 侧依然暴露 `Stream<Np4Incoming>`。混洗延迟本身 ≥ 700ms，25ms 轮询无感知。
- **UI 线程永不阻塞**：FFI 全部跑在常驻 engine isolate（`publish_keys` 冷启动可阻塞数秒）。
- **匿名语义原样继承**：`send` 是 mix 专属、硬失败，无静默直连回退；UI 不提供不安全直连模式。

## 快速开始

前置：Flutter SDK（`flutter doctor` 通过）；各平台工具链见下。

```bash
cd clients/flutter
./tool/setup.sh          # flutter create 四平台 + 构建原生库 + 胶水补丁 + pub get + analyze + test
flutter run -d macos     # 或 windows / linux / <android-device>
```

### macOS 构建前提（本机无 Xcode 时）

Flutter 的 macOS 桌面构建必须用完整 Xcode（Command Line Tools 不够）。装好后：

```bash
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
sudo xcodebuild -license accept && sudo xcodebuild -runFirstLaunch   # 或直接打开一次 Xcode
./tool/setup.sh     # 重跑：会给首次构建生成的 macos/Podfile 接入 Np4Bridge pod（幂等）
flutter run -d macos --dart-define=NP4_BOOTSTRAP=<multiaddr> --dart-define=NP4_AUTOCONNECT=1
```

原生 dylib（universal）已由 `build_native.sh` 产出到 `native/macos/`，`macos/Np4Bridge.podspec` 会把它 vendored 进 `.app/Contents/Frameworks`；macOS 沙箱的网络 client/server entitlements 也已在脚手架里开好。

### 与服务器配对

在公网服务器上跑 `bootstrap start`（bootstrap 现兼任 relay，见 `go/cmd/bootstrap/README.md`），把输出的 multiaddr 填进连接页，hops 用 **1**。两个客户端互相填对方的 Peer ID 即可匿名聊天。

## 原生库构建（tool/build_native.sh）

| 目标 | 产物 | 工具链 | 本仓库已验证 |
|---|---|---|---|
| android | `android/app/src/main/jniLibs/<abi>/libnp4bridge.so`（arm64-v8a / armeabi-v7a / x86_64） | Android NDK（自动探测，API 21） | ✅ NDK r30 |
| macos | `native/macos/libnp4bridge.dylib`（arm64+x86_64 universal，@rpath id） | 系统 clang + lipo | ✅ |
| windows | `native/windows/np4bridge.dll` | mingw-w64（`brew install mingw-w64`） | 脚本就绪 |
| linux | `native/linux/libnp4bridge.so` | Linux 原生构建，或 `zig cc` 交叉 | 脚本就绪 |

Android 的 `.so` 由 Gradle 自动打包（`jniLibs`）；macOS 经 `macos/Np4Bridge.podspec` vendored 进 `.app/Contents/Frameworks`（setup.sh 自动写 podspec + 补 Podfile + 开沙箱网络 entitlements）；Windows/Linux 由 setup.sh 往各自 CMakeLists 追加拷贝/安装规则。产物不入库（`.gitignore`），CI 每次构建。

## 已知边界

- **iOS 暂不支持**：桥层无需改动即可扩展（Go 侧 `-buildmode=c-archive` + xcframework + vendored pod），待排期。
- 消息为尽力送达；"已发送"仅表示已进入匿名队列（端到端 ACK 是 [v2]）。
- 对方离线 / 未发布 key / relay 不足（hops > 在线 relay 数）都会硬失败并弹出错误。
- 身份文件存在 app support 目录（`np4_identity`）；卸载清数据 = 身份更换 = 对方需要重新填你的 Peer ID。
