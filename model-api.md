## 1. 最终结论

**Cline 4.1.15 的 Free Models 来自：**
```text
GET https://api.cline.bot/api/v1/ai/cline/recommended-models
```

该 API 返回 JSON 包含 `recommended`、`free` 和 `clinePass` 三个数组。**Free 模型由服务器直接返回 `free` 数组**，客户端不进行任何价格判断。

---

## 2. API 信息

| 项 | 值 |
|---|---|
| **HTTP Method** | `GET` |
| **URL** | `https://api.cline.bot/api/v1/ai/cline/recommended-models` |
| **Authentication** | **无需登录 / 无 Token** |
| **环境变量覆盖** | `CLINE_API_BASE_URL` (生产环境默认 `https://api.cline.bot`) |

---

## 3. 完整请求

```bash
curl 'https://api.cline.bot/api/v1/ai/cline/recommended-models'
```

- **Query 参数**: 无
- **Body**: 无
- **Headers**: 仅默认浏览器/Node headers，**无自定义 Authorization**
- **Timeout**: 5000ms (`T_p=5e3`)
- **缓存**: 5分钟 (`fMh`)

---

## 4. Response 示例

```json
{
  "recommended": [
    {"id": "anthropic/claude-opus-4.6", "name": "Claude Opus 4.6", "description": "...", "tags": ["BEST"]}
  ],
  "free": [
    {"id": "kwaipilot/kat-coder-pro", "name": "KwaiKAT Kat Coder Pro", "description": "...", "tags": ["FREE"]},
    {"id": "arcee-ai/trinity-large-preview:free", "name": "Arcee AI Trinity Large Preview", "description": "...", "tags": ["FREE"]}
  ],
  "clinePass": []
}
```

---

## 5. Free 判断逻辑

**Free 判断依据**: 服务器直接返回的 `free` 数组

- 客户端**不检查** `pricing.input === 0` 或 `model.free === true` 等字段
- 后台 `bqd()` 函数**强制设置** `pricing: {input:0, output:0}` 给 free 模型，但这是设置而非过滤
- Free Tab 渲染 `r.free` 数组: `we.free.map(e => h$(e, "FREE"))`

---

## 6. 模型字段

| 字段 | 类型 | 含义 |
|---|---|---|
| `id` | string | **实际 API Model ID** (如 `kwaipilot/kat-coder-pro`) |
| `name` | string | 显示名称 |
| `description` | string | 描述 |
| `tags` | string[] | 标签 (如 `["FREE"]`) |
| `contextWindow` | number | 上下文窗口 |
| `maxInputTokens` | number | 最大输入 tokens |
| `maxTokens` | number | 最大输出 tokens |
| `capabilities` | string[] | 能力列表 |
| `pricing` | object | 价格 (客户端设为 0) |
| `releaseDate` | string | 发布日期 |
| `family` | string | 模型家族 |
| `status` | string | 状态 (active/preview/deprecated/legacy) |
| `modalities` | object | 输入/输出模态 |
| `operation` | string | 操作类型 |

---

## 7. Cline 内部调用链

```text
Free Tab UI (Settings → API Configuration → ClinePass → Free)
  ↓
next/webview-ui/build/assets/index.js
  ↓ Ar.makeUnaryRequest("refreshClineRecommendedModelsRpc", en.create({}), en.toJSON, YF.fromJSON)
  ↓ window.postMessage({type:"grpc_request", method:"refreshClineRecommendedModelsRpc", ...})
  ↓ ProtoBuf decode → ModelsService handler → ywu(t, e) 函数
      ↓ $0r() [5分钟内存缓存]
          ↓ mMh() [缓存未命中]
              ↓ r7o({}) → B_p(fetch, `${base}/api/v1/ai/cline/recommended-models`, 5000)
                  ↓ R_p() → use().apiBaseUrl → p8t().apiBaseUrl → "https://api.cline.bot"
              ↓ k_p(i) [验证结构] → P_p(s, obn, timeout) [enrichment]
          ↓ 缓存: K0r = {data, timestamp}
      ↓ Nst.create({recommended, free, clinePass})
  ↓ webview: v(R.free ?? []) → Free Tab 渲染
```

**关键文件/函数**:
- `xqd(t=fetch)` — 构建 URL: `${p8t().apiBaseUrl}/api/v1/ai/cline/recommended-models`
- `p8t()` — 返回配置，`YOt.production.apiBaseUrl = "https://api.cline.bot"`
- `ywu(t,e)` — RPC handler，调用 `$0r()` → `r.free.map(...)` 返回 `free` 数组
- `r7o(t)` — 实际 HTTP 请求，通过 `B_p()` 包装 `fetch()`，5秒超时
- `B_p(t,e,r)` — 带 AbortController 的 fetch 包装，**无 auth headers**
- `NQt` — 硬编码 fallback (4 recommended + 2 free models)
- `Pge` — webview 硬编码 fallback (4 recommended + 2 free models)

---

## 8. Token 来源

| 问题 | 答案 |
|---|---|
| Token 从哪里来 | **不需要 Token** |
| Token 是否必须 | **否** |
| 未登录是否可以获取 | **是** |
| Free Models 是否需要 Cline 账号 | **否** |

`xqd()` → `B_p()` → `fetch(url, {signal})` —**纯粹的无认证 GET 请求**，无 Authorization/Cookie/API-Key headers。

---

## 9. 是否可以被第三方工具直接调用

**✅ 可以直接调用** — 无需 Token，无需特殊 Headers。

---

## 10. 实现方案

```text
我的工具
    ↓
GET https://api.cline.bot/api/v1/ai/cline/recommended-models
    ↓
解析 JSON 响应
    ↓
提取 response.free 数组
    ↓
展示模型列表 (id, name, description)
```

**curl 示例**:
```bash
curl 'https://api.cline.bot/api/v1/ai/cline/recommended-models'
```

**Node.js 示例**:
```js
const res = await fetch('https://api.cline.bot/api/v1/ai/cline/recommended-models');
const data = await res.json();
const freeModels = data.free ?? [];
```

---

## 11. Combo 组合模型路由（本项目扩展）

本项目在客户端侧支持 **Combo（具名模型组合）**：管理员在 `/admin/` 后台把若干模型
按顺序配置成一个组合 ID（如 `claude-free-combo`），客户端把该 ID 当作普通模型名使用。

**适用端点** — 组合通过模型名定位，三个端点均支持：

| 端点 | 用法 |
|---|---|
| `POST /v1/chat/completions` | `"model": "<combo-id>"` |
| `POST /v1/messages` (Anthropic) | `"model": "<combo-id>"` |
| `POST /v1/responses` | `"model": "<combo-id>"` |

**回退（fallback）语义** — 按管理员配置的条目顺序依次尝试：

1. 配置顺序即优先级：**列表中靠前的启用条目先被尝试**，首个成功者服务本次请求。
2. 条目失败且被判定为**可重试**（限流 429 / 余额 402 / 瞬态 5xx / 超时 / 账号不可用，
   以及上游 400「上下文超限」截断重试后仍失败）时，自动**继续尝试下一个条目**。
3. **永久性错误**（401/403 及一般 400）直接失败，不再换下一个条目。
4. **固定源条目**（pinned source）：条目可显式固定 `cline` / `provider` / `zen`，
   此时只用该上游，失败**不回退**到其他源；固定源对该模型不可用时该条目被跳过，
   继续走下一个条目（不会中止整条组合）。
5. 停用条目 / 重复条目 / 后台停用的模型会被跳过（计入轨迹，不发起请求）。
6. 默认不触发全局 ModelChain 或 free 链兜底（应急回退需管理员显式开启）。

**查找可用组合 ID** — `GET /v1/models`（及 `/models`）会把启用中的组合合并进模型列表，
组合条目以 `owned_by: "combo"` 标识：

```bash
curl -H "x-api-key: YOUR_KEY" http://127.0.0.1:8999/v1/models
```

```json
{
  "object": "list",
  "data": [
    {"id": "kwaipilot/kat-coder-pro", "object": "model", "created": 1, "owned_by": "cline"},
    {"id": "claude-free-combo",  "object": "model", "created": 1, "owned_by": "combo"}
  ]
}
```

> 组合的创建 / 编辑 / 删除 / 启停在 `/admin/` 后台完成；响应中的 `created` 为当前时间戳。

---

## 附录：关于 `ox-alpha` 等 UI 所示模型的说明

UI 中显示的 `ox-alpha`、`deepseek-v4-flash`、`laguna-s-2.1` 是**本地硬编码**的 fallback 模型，不是远程 API 返回的 Free 模型：

| UI 显示 | 本地硬编码 Model ID | 远程 API Free 模型 |
|---|---|---|
| Ox Alpha | `stealth/ox-alpha` | (不同的模型) |
| DeepSeek V4 Flash | `deepseek-ai/deepseek-v4-flash` | (不同的模型) |
| Laguna S 2.1 | `poolside/laguna-s-2.1:free` | (不同的模型) |

远程 API 返回的 Free 模型（4.1.15 build）：`kwaipilot/kat-coder-pro`、`arcee-ai/trinity-large-preview:free`
(具体取决于服务器端，可能会更新)