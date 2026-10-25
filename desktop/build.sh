#!/usr/bin/env sh
# 构建当前平台的单文件桌面版。
# Wails 依赖各平台原生 WebView（C 绑定），不能交叉编译，
# 需要在每个目标系统上分别运行此脚本。
#
# 各平台前置依赖：
#   Windows: WebView2 运行时（Win10/11 自带）
#   macOS:   Xcode Command Line Tools (xcode-select --install)
#   Linux:   GTK3 + WebKit2GTK，例如：
#            Debian/Ubuntu: sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
#            Fedora:        sudo dnf install gtk3-devel webkit2gtk4.1-devel
#            Arch:          sudo pacman -S gtk3 webkit2gtk
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OS=$(go env GOOS)
ARCH=$(go env GOARCH)
OUT="$ROOT/desktop/build/$OS-$ARCH"
NAME="cline-proxy-desktop"
# 版本号：优先环境变量 VERSION，否则取最近 git tag（如 v1.3.0），再退回 dev
VERSION=${VERSION:-$(git describe --tags --always 2>/dev/null || echo "dev")}
GIT_COMMIT=${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "")}
BUILD_DATE=${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
LDFLAGS="-s -w -X main.appVersion=$VERSION -X main.appGitCommit=$GIT_COMMIT -X main.appBuildDate=$BUILD_DATE"

if [ "$OS" = "windows" ]; then
  NAME="$NAME.exe"
  # GUI 子系统，双击不弹黑色控制台窗口
  LDFLAGS="$LDFLAGS -H windowsgui"
fi

mkdir -p "$OUT"
cd "$ROOT"
go build -tags "desktop production" -trimpath -ldflags="$LDFLAGS" -o "$OUT/$NAME" .
printf 'Built: %s (version %s)\n' "$OUT/$NAME" "$VERSION"
