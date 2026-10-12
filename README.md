<div align="center">

# Cline2API

Cline API 反向代理 · 多账号轮询 · 双协议兼容 · 桌面端

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20macOS%20%7C%20Linux-blue)](#构建)

**🌐 English: [English README](README.en.md)**

</div>

---

## 简介

Cline2API 是 Cline API 的反向代理服务，支持多账号轮询、OpenAI 和 Anthropic Messages API 双协议、API Key 鉴权，内置中英文管理后台（自动跟随浏览器语言，可手动切换）。提供跨平台桌面端单文件应用（Windows / macOS / Linux），双击即用。

**开发语言**：Go（后端 + 代理 + 桌面壳），HTML/CSS/JS（管理后台前端，内嵌于二进制）。

## 功能

- **双协议兼容**：同时支持 `/v1/chat/completions`（OpenAI）和 `/v1/messages`（Anthropic Messages API）
- **多账号轮询**：自动在多个 Cline 账号间切换负载（`round_robin` / `fill` / `random` 策略）
- **中英文管理后台**：浏览器访问 `/admin/` 管理账号、API Key、模型配置、请求头、代理设置；自动跟随浏览器语言，侧栏可手动切换
- **动态模型同步**：启动时自动拉取 Cline 官方推荐模型接口（免费/订阅模型），模型变化时弹窗提示，也可在后台手动「从 Cline 同步模型」
- **自定义模型**：后台可手动添加/删除模型 ID，并自由选择默认模型（未设置时自动回退到第一个免费模型）
- **API Key 鉴权**：保护代理端点，支持生成/删除多个 API Key
- **System Prompt 覆盖**：项目目录下放 `override.md` 则自动替换系统提示词
- **账号导入/导出**：支持 OAuth 登录、手动 Token、批量文件导入，以及跨设备导出
- **请求日志**：记录每次请求的 token 用量、耗时、TPS 等指标
- **桌面端**：单文件跨平台桌面应用（Wails v2），关闭窗口即停止服务

## 快速开始

### 方式一：桌面端（推荐，分享给他人）

从 [Releases](https://github.com/luawei1/cline2api/releases) 下载对应平台的可执行文件，双击运行即可。

> Windows 提示 SmartScreen「已保护你的电脑」是**未购买代码签名证书的正常现象**，
> 点击「更多信息 → 仍要运行」即可，不影响使用。

| 平台 | 文件 | 说明 |
|------|------|------|
| Windows x64 | `cline-proxy-desktop.exe` | Win10/11 自带 WebView2 |
| macOS Apple Silicon | `cline-proxy-desktop-darwin-arm64` | 需 Xcode CLT |
| macOS Intel | `cline-proxy-desktop-darwin-amd64` | 需 Xcode CLT |
| Linux x64 | `cline-proxy-desktop-linux-amd64` | 需 GTK3 + WebKit2GTK |

### 方式二：命令行

```bash
go build -o cline-proxy .
./cline-proxy              # 默认端口 3457
./cline-proxy -port 8080   # 指定端口
```

启动后访问 http://127.0.0.1:3457/admin/ 进入管理后台。

### 方式三：Docker

```bash
docker compose up -d      # 构建并启动
docker compose logs -f     # 查看日志
docker compose down        # 停止
```

容器内已配置监听 `0.0.0.0:3457`（`-p 3457:3457` 映射对外可达），管理后台同样无鉴权，请勿将端口暴露到公网。

**数据持久化**：所有数据（账号、API Key、配置、请求日志等）都存放在容器的 `/app/data` 目录，
compose 已挂载到宿主机 `./data`，**重建容器（不删除 `./data`）数据不丢**。不用 compose 时请自行挂载：
`docker run -v ./data:/app/data ...`。

<details>
<summary>老部署升级到本版本（一次性迁移）</summary>

早期版本把数据写在容器可写层（`/app/.cline-*.json`），挂载卷也不会被读取，重建容器即丢失。
升级到本版本后数据统一存放在 `CLINE2API_DATA_DIR`（默认 `/app/data`），并支持一次性自动迁移：

- **docker compose 部署**：把宿主机上旧的 `.cline-accounts.json`（在 docker-compose.yml 同目录）复制进 `./data/` 再启动：
  `mkdir -p data && cp .cline-accounts.json data/`
- **旧容器还在运行**：先把可写层里的数据拷出来再删容器：
  `docker cp cline-proxy:/app/.cline-accounts.json ./data/`（其他 `.cline-*.json` 同理）
- 程序启动时也会自动把可执行文件目录/工作目录下已有的旧数据文件迁移到数据目录（仅当目标不存在时）。

</details>

> 可选环境变量 `CLINE2API_DATA_DIR`：指定数据目录，默认在 Docker 内为 `/app/data`，其他环境为可执行文件所在目录。

## 使用指南

### 1. 添加 Cline 账号

在管理后台 **账号管理 → 导入账号**：

- **OAuth 浏览器登录**：点击按钮启动设备授权流程，在系统浏览器中完成登录（支持已登录 Cline 的浏览器）
- **手动输入 Token**：输入已有账号的 refreshToken
- **批量文件导入**：上传 JSON 文件或粘贴文本（每行一个 token，或 JSON 数组 `[{refreshToken, email}]`）

### 2. 配置客户端

```
Base URL: http://127.0.0.1:3457/v1
API Key:  <在管理后台生成的 Key>
Model:    cline-free/glm-5.2
```

兼容 OpenAI 和 Anthropic 两种 API 格式。

### 3. 账号导出/导入（跨设备迁移）

- **导出**：账号管理页面点击「导出」按钮，下载 `cline-accounts-export.json`
- **导入**：在另一台设备上用「从文件导入」上传该文件
- 导出格式与批量导入格式完全兼容

### 4. System Prompt 覆盖

在 exe 同目录下创建 `override.md`，内容将替换所有客户端请求的系统提示词。

### 5. 监听地址与访问设置（局域网 / 多网卡）

默认只监听 `127.0.0.1`（仅本机可访问）。管理后台 **访问设置** 区可：

- **监听地址下拉选择**：`127.0.0.1`（仅本机）/ `0.0.0.0`（所有网卡）/ 本机检测到的 IP，保存后自动重启监听立即生效，选择会自动检测并展示本机 IP 列表
- **管理后台密码**：默认无密码；设置后访问 `/admin/` 需输入密码登录（会话 Cookie，24 小时有效），留空保存可清除密码

命令行也可指定监听地址（优先级：环境变量 > 后台设置 > `127.0.0.1`）：

```bash
# CLI：监听所有网卡（局域网设备可通过本机 IP 访问）
./cline-proxy -host 0.0.0.0

# 指定某个网卡的 IP
./cline-proxy -host 192.168.1.100

# 环境变量方式（桌面端同样支持）
CLINE_PROXY_HOST=0.0.0.0 ./cline-proxy
```

> ⚠️ **安全警告**：管理后台 `/admin/` 无鉴权（除非设置了访问密码），监听非回环地址（如 `0.0.0.0`）会将其暴露给局域网。
> 请确认网络环境可信，或配合防火墙仅放行需要的 IP 访问 `3457` 端口。

### 6. 出口代理（跨区限制）

国内直连 Cline 上游（`api.cline.bot` / `api.workos.com`）会命中跨区限制，本工具支持在管理后台配置**应用内出口代理**，无需在主机开 TUN：

- **Cline 出口代理**：管理后台「上游服务 → Cline 出口代理」，填入代理列表（每行一个，支持 `http` / `https` / `socks5` / `socks5h`，如 `socks5://127.0.0.1:1080`），所有发往 Cline 的请求（对话、登录/令牌刷新、模型同步）经代理池按策略（轮询 / 随机 / 填满）轮询出去。配置持久化在 `.cline-proxy.json`，保存即生效，无需重启。
- **opencode 出口代理**：同一页面的 opencode Zen 区块，为 opencode（zen）上游单独配置代理池，命中限流时自动冷却当前出口。

优先级：应用内代理 > 环境变量代理（`HTTPS_PROXY`）> 直连。回环 / 内网目标（如本机 Ollama 等自定义 Provider）始终直连，不经代理。

## 构建

### 桌面端（单文件跨平台）

Wails 依赖各平台原生 WebView，需在目标系统上构建：

```bash
# Windows（当前机器）
./desktop/build.sh

# macOS
xcode-select --install
./desktop/build.sh

# Linux
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
./desktop/build.sh
```

### CI 自动构建

推送 `v*` 标签触发 GitHub Actions 三平台自动构建并发布 Release：

```bash
git tag v1.0.0
git push origin v1.0.0
```

### 发布 zip（网盘分发推荐）

裸 exe 上传网盘后浏览器容易拦截，打包成 zip 可显著降低拦截率：

```bash
./desktop/build.sh && ./desktop/dist.sh
# 产物：desktop/dist/ccline2api-windows-amd64.zip
```

## 数据文件

程序按以下顺序查找数据文件（找到即使用）：

1. 可执行文件所在目录
2. 当前工作目录
3. 用户主目录 `~/.cline2api/`

| 文件 | 说明 |
|------|------|
| `.cline-accounts.json` | 账号池、API Key、自定义模型与默认模型 |
| `.cline-request-logs.json` | 请求日志 |
| `.cline-proxy.json` | Cline 出口代理池配置 |
| `.cline-zen.json` | opencode（zen）配置，含其出口代理池 |
| `override.md` | System Prompt 覆盖（可选）|

> ⚠️ 账号文件含明文 refreshToken，属于敏感凭据，不要放入发布包或提交到 Git。

## 可用模型

**默认动态同步**：程序启动时会自动从 Cline 官方推荐模型接口拉取最新模型（免费 / cline-pass / 推荐模型），
模型列表变化时管理后台会弹窗提示，也可在「设置 → 可用模型」点击「从 Cline 同步模型」手动刷新。

- 同步成功后，后台模型列表以**远程模型**为主（内置硬编码模型仅作为离线 fallback）
- 远程模型直接可用；后台「可用模型」区可添加/删除**自定义模型**（带 ✕ 删除按钮的是自定义项）
- 默认模型可在「代理配置 → 默认模型」下拉中设置；未设置时自动回退到第一个免费模型

> 内置 fallback 模型（离线/同步失败时兜底）：
> `cline-free/glm-5.2`、`cline-pass/glm-5.2`、`cline-pass/deepseek-v4-flash`、`cline-pass/qwen3.7-max`、`deepseek/deepseek-v4-flash`、`poolside/laguna-s-2.1:free`

## 项目结构

```
├── main.go              CLI 入口（go build .）
├── desktop_main.go      桌面端入口（go build -tags desktop）
├── proxy.go             HTTP 服务、API 路由、协议转换、SSE
├── admin.go             管理后台 REST API
├── admin_html.go        管理后台前端（内嵌）
├── auth.go              WorkOS OAuth + Token 刷新
├── pool.go              账号池管理、多位置数据查找
├── request_logs.go      请求日志
├── desktop/             桌面端构建脚本、文档、图标生成器
├── Dockerfile           Docker 构建
├── docker-compose.yml   Docker Compose
└── .github/workflows/   CI 三平台自动构建
```

## 技术栈

- [Go 1.25](https://go.dev) — 后端、代理、桌面壳
- [Wails v2](https://wails.io) — 跨平台桌面 WebView（单文件，非 Electron）
- [WebView2](https://developer.microsoft.com/microsoft-edge/webview2/) / [WebKit](https://webkit.org) / [WebKitGTK](https://webkitgtk.org) — 各平台原生 WebView

## 许可证

[MIT License](LICENSE) © 2026 [luawei1](https://github.com/luawei1)
