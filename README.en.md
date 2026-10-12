<div align="center">

# Cline2API

Cline API reverse proxy · multi-account rotation · dual protocol · desktop app

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20macOS%20%7C%20Linux-blue)](#build)

**🌏 中文: [中文 README](README.md)**

</div>

---

## Introduction

Cline2API is a reverse proxy for the Cline API with multi-account rotation, dual protocol support (OpenAI + Anthropic Messages API), API key authentication, and a bilingual admin panel (English/Chinese, auto-detected from your browser language with a manual toggle in the sidebar). A single-file cross-platform desktop app (Windows / macOS / Linux) is included — just download and run.

**Built with**: Go (backend + proxy + desktop shell), HTML/CSS/JS (embedded admin frontend).

## Features

- **Dual protocol**: serves both `/v1/chat/completions` (OpenAI) and `/v1/messages` (Anthropic Messages API)
- **Multi-account rotation**: load-balances across Cline accounts (`round_robin` / `fill` / `random`)
- **Bilingual admin panel**: `/admin/` manages accounts, API keys, models, headers and proxy settings; auto-follows your browser language, manually switchable in the sidebar
- **Dynamic model sync**: fetches the official Cline recommended-models API on startup (free / cline-pass / recommended); a popup notifies you when the model list changes, and you can also click "Sync Models from Cline" in the panel anytime
- **Custom models**: add/remove model IDs manually and pick a default model (falls back to the first free model automatically)
- **API key auth**: protects proxy endpoints; generate/delete multiple API keys
- **System Prompt override**: place an `override.md` next to the executable to replace the system prompt for all requests
- **Account import/export**: OAuth login, manual tokens, batch file import, and cross-device export
- **Request logs**: per-request token usage, latency, TPS, and more
- **Desktop app**: single-file cross-platform app (Wails v2); closing the window stops the service

## Quick Start

### Option 1: Desktop app (recommended for sharing)

Download the executable for your platform from [Releases](https://github.com/luawei1/cline2api/releases) and double-click it.

> On Windows, the SmartScreen "Windows protected your PC" warning is normal because no code-signing certificate is purchased. Click "More info → Run anyway".

| Platform | File | Notes |
|----------|------|-------|
| Windows x64 | `cline-proxy-desktop.exe` | WebView2 built into Win10/11 |
| macOS Apple Silicon | `cline-proxy-desktop-darwin-arm64` | Requires Xcode CLT |
| macOS Intel | `cline-proxy-desktop-darwin-amd64` | Requires Xcode CLT |
| Linux x64 | `cline-proxy-desktop-linux-amd64` | Requires GTK3 + WebKit2GTK |

### Option 2: Command line

```bash
go build -o cline-proxy .
./cline-proxy              # default port 3457
./cline-proxy -port 8080   # custom port
```

Then open http://127.0.0.1:3457/admin/ for the admin panel.

### Option 3: Docker

```bash
docker compose up -d      # build and start
docker compose logs -f    # view logs
docker compose down       # stop
```

The container listens on `0.0.0.0:3457` (`-p 3457:3457` maps it externally). The admin panel has no auth by default — do **not** expose the port to the public internet.

**Data persistence**: all data (accounts, API keys, settings, request logs) lives in the container's `/app/data` directory, which compose mounts to `./data` on the host. **Recreating the container (without deleting `./data`) keeps all data**. Without compose, mount it yourself: `docker run -v ./data:/app/data ...`.

<details>
<summary>Upgrading an old deployment to this version (one-time migration)</summary>

Older versions wrote data to the container's writable layer (`/app/.cline-*.json`), so mounted volumes were never read and data was lost on container recreation. This version stores everything in `CLINE2API_DATA_DIR` (`/app/data` by default in Docker):

- **docker compose deployments**: copy the old host-side `.cline-accounts.json` (next to docker-compose.yml) into `./data/` before starting: `mkdir -p data && cp .cline-accounts.json data/`
- **Old container still running**: salvage the data before removing it: `docker cp cline-proxy:/app/.cline-accounts.json ./data/` (repeat for other `.cline-*.json` files)
- On startup the program also automatically migrates legacy data files found next to the executable or in the working directory into the data directory (only when the target does not exist yet).

</details>

> Optional environment variable `CLINE2API_DATA_DIR`: sets the data directory. Defaults to `/app/data` in Docker, or the executable's directory elsewhere.

## Usage Guide

### 1. Add a Cline account

In the admin panel, go to **Import**:

- **OAuth browser login**: starts the device-authorization flow; complete login in your system browser (works with an already-logged-in Cline browser)
- **Manual token**: paste an existing refreshToken
- **Batch import**: upload a JSON file or paste text (one token per line, or a JSON array `[{refreshToken, email}]`)

### 2. Configure your client

```
Base URL: http://127.0.0.1:3457/v1
API Key:  <key generated in the admin panel>
Model:    <model from the synced list, e.g. stealth/ox-alpha>
```

Both OpenAI and Anthropic API formats are supported.

### 3. Account export/import (device migration)

- **Export**: click "Export" on the Accounts page to download `cline-accounts-export.json`
- **Import**: upload that file via "Import from File" on another device
- The export format is fully compatible with the batch-import format

### 4. System Prompt override

Create `override.md` next to the executable; its content replaces the system prompt for all client requests.

### 5. Listen address & access settings (LAN / multi-NIC)

By default the proxy listens on `127.0.0.1` (local only). The **Access Settings** section of the admin panel lets you:

- **Choose a listen address**: `127.0.0.1` (local) / `0.0.0.0` (all interfaces) / detected local IPs; saving restarts the listener immediately
- **Admin password**: none by default; once set, `/admin/` requires a password (session cookie, 24h); save an empty field to clear it

You can also set the listen address on the command line (priority: env var > panel setting > `127.0.0.1`):

```bash
# CLI: listen on all interfaces (LAN devices can reach it via your local IP)
./cline-proxy -host 0.0.0.0

# Bind to a specific NIC
./cline-proxy -host 192.168.1.100

# Environment variable (works for the desktop app too)
CLINE_PROXY_HOST=0.0.0.0 ./cline-proxy
```

> ⚠️ **Security warning**: `/admin/` has no auth (unless you set a password). Listening on a non-loopback address (e.g. `0.0.0.0`) exposes it to your LAN. Only do this on a trusted network, or restrict port `3457` in your firewall.

### 6. Egress proxy (cross-region restrictions)

Direct requests to the Cline upstream (`api.cline.bot` / `api.workos.com`) may be region-restricted from mainland China. Configure an **application-level egress proxy** in the admin panel without enabling TUN mode on the host:

- **Cline egress proxy**: open **Upstreams → Cline Egress Proxies**, enter one proxy URL per line (`http`, `https`, `socks5`, or `socks5h`; for example `socks5://127.0.0.1:1080`). Cline requests (chat, login/token refresh, and model sync) use the configured pool with round-robin, random, or fill strategy. The setting is stored in `.cline-proxy.json` and takes effect immediately.
- **OpenCode egress proxy**: the OpenCode Zen section has a separate proxy pool for the opencode upstream and automatically cools down rate-limited exits.

Priority: application proxy > environment proxy (`HTTPS_PROXY`) > direct connection. Loopback and private-network targets (such as a local Ollama or custom Provider) always connect directly.

## Build

### Desktop app (single-file, cross-platform)

Wails requires the native WebView of each platform — build on each target system:

```bash
# Windows (this machine)
./desktop/build.sh

# macOS
xcode-select --install
./desktop/build.sh

# Linux
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
./desktop/build.sh
```

### CI builds

Pushing a `v*` tag triggers GitHub Actions to build and release all three platforms:

```bash
git tag v1.0.0
git push origin v1.0.0
```

### Release zip (recommended for cloud-drive sharing)

Browsers are less likely to block a zip than a bare exe:

```bash
./desktop/build.sh && ./desktop/dist.sh
# Produces desktop/dist/ccline2api-windows-amd64.zip
```

## Data Files

Files are looked up in this order: executable directory → working directory → `~/.cline2api/`.

| File | Purpose |
|------|---------|
| `.cline-accounts.json` | Account pool, API keys, custom models and default model |
| `.cline-request-logs.json` | Request logs |
| `.cline-proxy.json` | Cline egress proxy pool configuration |
| `.cline-zen.json` | OpenCode (Zen) configuration, including its egress proxy pool |
| `override.md` | System Prompt override (optional) |

> ⚠️ The account file contains plaintext refreshTokens — treat it as sensitive. Never ship it in a release package or commit it to Git.

## Available Models

**Synced dynamically by default**: on startup the proxy fetches the official Cline recommended-models endpoint (free / cline-pass / recommended). When the list changes, the admin panel shows a popup; you can also hit "Sync Models from Cline" in Settings → Available Models at any time.

- After a successful sync, the panel lists the **remote models** (hardcoded built-ins remain only as an offline fallback)
- Remote models are ready to use; you can add/remove **custom models** in the panel (items with an ✕ delete button are custom)
- Set the default model in "Proxy Config → Default Model"; if unset, it falls back to the first free model

> Built-in fallback models (used offline / when sync fails):
> `cline-free/glm-5.2`, `cline-pass/glm-5.2`, `cline-pass/deepseek-v4-flash`, `cline-pass/qwen3.7-max`, `deepseek/deepseek-v4-flash`, `poolside/laguna-s-2.1:free`

## Project Structure

```
├── main.go              CLI entry (go build .)
├── desktop_main.go      Desktop entry (go build -tags desktop)
├── proxy.go             HTTP server, API routes, protocol conversion, SSE
├── admin.go             Admin REST API
├── admin_html.go        Admin frontend (embedded)
├── models_sync.go       Cline model sync (startup + manual)
├── i18n.go              Bilingual (zh/en) messages for the admin API
├── auth.go              WorkOS OAuth + token refresh
├── pool.go              Account pool management, multi-location lookup
├── request_logs.go      Request logs
├── desktop/             Desktop build scripts, docs, icon generator
├── Dockerfile           Docker build
├── docker-compose.yml   Docker Compose
└── .github/workflows/   CI builds for all three platforms
```

## Tech Stack

- [Go 1.25](https://go.dev) — backend, proxy, desktop shell
- [Wails v2](https://wails.io) — cross-platform desktop WebView (single file, not Electron)
- [WebView2](https://developer.microsoft.com/microsoft-edge/webview2/) / [WebKit](https://webkit.org) / [WebKitGTK](https://webkitgtk.org) — native WebView per platform

## License

[MIT License](LICENSE) © 2026 [luawei1](https://github.com/luawei1)
