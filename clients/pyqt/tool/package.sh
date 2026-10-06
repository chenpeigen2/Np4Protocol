#!/usr/bin/env bash
# Package the PyQt client into a standalone app (macOS .app; Windows/Linux
# produce a dist/<name>/ folder — PyInstaller does not cross-compile).
#
#   Usage: tool/package.sh [windowed|console]
#
# Prereqs: .venv with PyQt6 + pyinstaller; native lib built by
# ../flutter/tool/build_native.sh. The dylib is bundled into np4bridge/
# and found via the _MEIPASS paths in np4_bridge._candidate_dirs.
set -euo pipefail
cd "$(dirname "$0")/.."

MODE="${1:-windowed}"
FLAGS=(--windowed)
[ "$MODE" = "console" ] && FLAGS=()

NEWADDR_HINT="run bootstrap start and copy its multiaddr"

.venv/bin/python -m PyInstaller -y --noconfirm "${FLAGS[@]}" \
  --name np4chat \
  --icon assets/icon.icns \
  --add-binary "../flutter/native/macos/libnp4bridge.dylib:np4bridge" \
  --hidden-import np4_bridge \
  --hidden-import np4_worker \
  --hidden-import np4_controller \
  main.py

echo
echo "打包完成：dist/np4chat.app（$MODE）"
echo "启动：open dist/np4chat.app   # 连接页填 bootstrap multiaddr（$NEWADDR_HINT）"
echo "诊断：console 模式或 ~/Library/Application Support/np4chat/np4chat.log"
