
// ===== i18n =====
const I18N = {
  'Cline 代理': 'Cline Proxy',
  '多账号轮询 · 双协议': 'Multi-account rotation · Dual protocol',
  '管理': 'Admin',
  '仪表盘': 'Dashboard',
  '账号管理': 'Accounts',
  '导入账号': 'Import',
  '请求日志': 'Request Logs',
  '设置': 'Settings',
  '关于': 'About',
  '反馈': 'Feedback',
  '查看账号池状态与快捷操作': 'Pool status & quick actions',
  '账号状态': 'Account Status',
  '账号总数': 'Total Accounts',
  '活跃': 'Active',
  '冷却': 'Cooldown',
  '已过期': 'Expired',
  'Token 用量': 'Token Usage',
  '累计输入 Token': 'Total Input Tokens',
  '累计输出 Token': 'Total Output Tokens',
  '累计总 Token': 'Total Tokens',
  '缓存 Token': 'Cached Tokens',
  '快捷操作': 'Quick Actions',
  '添加账号': 'Add Account',
  '刷新全部 Token': 'Refresh All Tokens',
  '从文件导入': 'Import from File',
  '生成 API 密钥': 'Generate API Key',
  '管理 Cline 账号池中的所有账号': 'Manage all accounts in the pool',
  '测试全部': 'Test All',
  '导出': 'Export',
  '添加': 'Add',
  '刷新': 'Refresh',
  '邮箱': 'Email',
  '状态': 'Status',
  '请求': 'Requests',
  '输入': 'Input',
  '输出': 'Output',
  '总 Token': 'Total Tokens',
  '缓存': 'Cache',
  '最后使用': 'Last Used',
  '创建时间': 'Created',
  '操作': 'Actions',
  '加载中...': 'Loading...',
  '通过 OAuth 登录、手动 Token 或批量文件添加账号': 'Add accounts via OAuth login, manual token, or batch file',
  'OAuth 浏览器登录': 'OAuth Browser Login',
  '手动输入 Token': 'Manual Token',
  '批量导入': 'Batch Import',
  '通过浏览器完成 OAuth 认证，支持 Google/GitHub/邮箱登录，自动获取 refreshToken。': 'Complete OAuth in the browser (Google/GitHub/email); refreshToken is fetched automatically.',
  '开始 OAuth 登录': 'Start OAuth Login',
  '等待浏览器授权...': 'Waiting for browser authorization...',
  '点击链接（系统浏览器打开）:': 'Open this link (system browser):',
  '并输入代码:': 'and enter the code:',
  '输入已有的 Cline refreshToken，系统会自动验证并加入池。': 'Paste an existing Cline refreshToken; it will be validated and added.',
  '邮箱（可选，留空自动生成）': 'Email (optional, auto-generated if empty)',
  '批量导入多个账号。支持 JSON 数组或每行一个 token。': 'Import many accounts at once. Supports JSON arrays or one token per line.',
  '导入全部': 'Import All',
  '选择文件': 'Choose File',
  '查看每次请求的 Token 消耗、缓存、耗时与流式速度': 'Token usage, caching, latency & streaming speed per request',
  '时间': 'Time',
  '账号': 'Account',
  '协议': 'Protocol',
  '模型': 'Model',
  '总': 'Total',
  '耗时': 'Duration',
  '加载更多': 'Load More',
  '路由基础': 'Routing Basics',
  'API 密钥管理': 'API Keys',
  '生成的密钥可用于客户端访问代理 API（作为 x-api-key 或 Authorization 头）。': 'Generated keys let clients call the proxy API (as x-api-key or Authorization header).',
  '生成新密钥': 'Generate New Key',
  '可用模型': 'Available Models',
  '添加模型': 'Add Model',
  '计费': 'Billing',
  '付费 (pass)': 'Paid (pass)',
  '免费 (free)': 'Free (free)',
  '访问设置': 'Access Settings',
  '监听地址': 'Listen Address',
  '127.0.0.1（仅本机）': '127.0.0.1 (local only)',
  '0.0.0.0（所有网卡）': '0.0.0.0 (all interfaces)',
  '当前地址': 'Current Address',
  '本机 IP（局域网访问地址）': 'Local IPs (LAN access)',
  '管理后台密码（': 'Admin password (',
  '未启用': 'Disabled',
  '保存': 'Save',
  '设置后访问管理后台需输入密码，默认无密码': 'Password required after enabling; none by default',
  '默认模型': 'Default Model',
  '轮询策略': 'Rotation Strategy',
  '回退模型链': 'Fallback Model Chain',
  '逗号分隔模型 ID，可用': 'Comma-separated model IDs; you can use ',
  '表示内置免费回退链；留空则自动回退到内置免费链。': ' for the built-in free fallback chain. Leave blank to use it automatically.',
  '轮询 (round_robin)': 'Round robin',
  '填满 (fill)': 'Fill',
  '随机 (random)': 'Random',
  '质量优先 (quality)': 'Quality-first (quality)',
  '引擎版本': 'Engine Version',
  '账号文件': 'Account File',
  '请求头配置（模拟 Cline CLI 发出）': 'Request Headers (mimic Cline CLI)',
  '这些请求头会附加到所有转发给 Cline API 的请求中，以模拟官方客户端行为。': 'Attached to every request forwarded to the Cline API.',
  '请求头': 'Header',
  '值': 'Value',
  '添加请求头': 'Add Header',
  '保存请求头': 'Save Headers',
  '危险操作': 'Danger Zone',
  '以下操作会删除全部数据，且删除后无法恢复；每个按钮点击后都会先弹出确认框。': 'These actions delete all data and cannot be undone; every button asks for confirmation first.',
  '删除全部账号': 'Delete All Accounts',
  '删除全部密钥': 'Delete All Keys',
  '应用信息、使用指南与开源协议': 'App info, usage guide & license',
  '应用信息': 'About App',
  'Cline API 反向代理 · 多账号轮询 · 双协议兼容': 'Cline API reverse proxy · multi-account rotation · dual protocol',
  '快速上手': 'Quick Start',
  '添加 Cline 账号': 'Add a Cline account',
  '前往「导入账号」页面，通过 OAuth 登录或手动输入 refreshToken 添加账号。支持批量导入。': 'Go to Import and add via OAuth or a pasted refreshToken. Batch import supported.',
  '生成 API Key': 'Generate an API Key',
  '前往「密钥与安全」页面生成密钥。如不配置任何密钥，代理允许匿名访问。': 'Generate a key in Keys & Security. With no keys, the proxy allows anonymous access.',
  '配置客户端': 'Configure your client',
  '在 Claude Code、Cline 等客户端中设置：': 'Configure in clients like Claude Code or Cline:',
  '功能特性': 'Features',
  '✅ 多账号轮询（轮询/填满/随机）': '✅ Multi-account rotation (round robin / fill / random)',
  '✅ OpenAI & Anthropic 双协议': '✅ OpenAI & Anthropic dual protocol',
  '✅ 429 冷却自动恢复': '✅ Auto-recovery from 429 cooldown',
  '✅ 账号导出/导入（跨设备迁移）': '✅ Account export/import (device migration)',
  '✅ OAuth 系统浏览器登录': '✅ OAuth login via system browser',
  '✅ 请求日志与统计': '✅ Request logs & stats',
  '✅ System Prompt 覆盖': '✅ System Prompt override',
  '✅ 跨平台桌面端（Win/Mac/Linux）': '✅ Cross-platform desktop (Win/Mac/Linux)',
  '项目链接': 'Links',
  '仓库地址': 'Repository',
  '问题反馈': 'Issues',
  '下载更新': 'Releases',
  '开源协议': 'License',
  '数据与隐私': 'Data & Privacy',
  '• 本程序仅在本地运行，默认监听': '• Runs locally, listens on',
  '，不对外暴露。': ', not exposed externally.',
  '• 账号凭据（refreshToken）存储在数据目录的': '• Credentials (refreshToken) are stored in plaintext in the file ',
  '（Docker 中位于 /app/data，可用 CLINE2API_DATA_DIR 配置）中，明文保存，请注意保护。': ' (in Docker: /app/data, configurable via CLINE2API_DATA_DIR); keep them safe.',
  '• 所有 API 请求通过本机代理转发至 Cline 官方服务器，不经过任何第三方。': '• All API requests are proxied to Cline\'s official servers, never through third parties.',
  '• 关闭窗口即停止服务，无后台驻留进程。': '• Closing the window stops the service; no background process.',
  'Cline2API 管理后台': 'Cline2API Admin',
  '该后台已启用访问密码，请输入密码登录': 'Password protection is enabled. Enter the password to continue.',
  '登 录': 'Sign In',
  '即将恢复': 'recovering soon',
  '需要登录': 'Login required',
  '登录失败': 'Login failed',
  '网络错误，请重试': 'Network error, please retry',
  '已在系统浏览器中打开': 'Opened in system browser',
  '打开失败: ': 'Open failed: ',
  '测试': 'Test',
  '重置': 'Reset',
  '删除': 'Delete',
  '从未使用': 'Never used',
  '加载账号失败: ': 'Failed to load accounts: ',
  ' · 输入 ': ' · Input ',
  ' · 输出 ': ' · Output ',
  ' · 缓存 ': ' · Cached ',
  ' · 总 ': ' · Total ',
  '输入 ': 'Input ',
  '测试成功：': 'Test OK: ',
  '测试失败：': 'Test failed: ',
  '未知错误': 'Unknown error',
  '测试失败: ': 'Test failed: ',
  '正在测试全部账号，请稍候...': 'Testing all accounts, please wait...',
  '全部测试通过：': 'All tests passed: ',
  ' 个账号正常': ' accounts OK',
  '测试完成：': 'Tests finished: ',
  ' 成功 / ': ' OK / ',
  ' 失败 · ': ' failed · ',
  '确定删除此账号？': 'Delete this account?',
  '账号已删除': 'Account deleted',
  '删除失败: ': 'Delete failed: ',
  '确定重置此账号？将恢复为活跃状态并刷新 Token，保留历史统计。': 'Reset this account? It will become active, tokens refreshed, history kept.',
  '账号已重置': 'Account reset',
  '重置失败: ': 'Reset failed: ',
  '⚠️ 确定删除所有账号？不可撤销！': '⚠️ Delete ALL accounts? This cannot be undone!',
  '全部账号已删除': 'All accounts deleted',
  '全部 Token 已刷新': 'All tokens refreshed',
  '刷新失败: ': 'Refresh failed: ',
  '正在连接 WorkOS...': 'Connecting to WorkOS...',
  '请在浏览器中打开链接并输入代码': 'Open the link in your browser and enter the code',
  '账号添加成功！': 'Account added!',
  '失败: ': 'Failed: ',
  'OAuth 失败': 'OAuth failed',
  '错误: ': 'Error: ',
  'OAuth 失败: ': 'OAuth failed: ',
  '请输入 refreshToken': 'Enter a refreshToken',
  '账号添加成功: ': 'Account added: ',
  '添加失败: ': 'Add failed: ',
  '账号已导出': 'Accounts exported',
  '导出失败: ': 'Export failed: ',
  '已导出 ': 'Exported ',
  '导出选中': 'Export selected',
  '删除选中': 'Delete selected',
  '导出全部': 'Export all',
  '请先勾选要导出的账号': 'Select accounts to export first',
  '请先勾选要删除的账号': 'Select accounts to delete first',
  '确认删除选中的 ': 'Confirm deleting ',
  ' 个账号？此操作不可恢复': ' selected accounts? This cannot be undone',
  '删除中...': 'Deleting...',
  '已删除 ': 'Deleted ',
  ' 个，失败 ': ' accounts, ',
  ' 个失败': ' failed',
  '共': 'Total ',
  '个账号 · 每页': ' accounts · per page ',
  '全选本页': 'Select all on this page',
  '已下架': 'Delisted',
  '匹配': 'Match ',
  '没有匹配的账号': 'No matching accounts',
  '请先勾选要重置的账号': 'Select accounts to reset first',
  '确认重置选中的 ': 'Confirm resetting ',
  ' 个账号？（清除冷却与失败计数）': ' selected accounts? (clears cooldowns and failure counts)',
  '重置中...': 'Resetting...',
  '已重置 ': 'Reset ',
  '免费模型': 'Free models',
  '付费模型': 'Paid models',
  '（免费）': ' (free)',
  '（付费）': ' (paid)',
  '（当前）': ' (current)',
  '移除（已下架模型支持手动移除）': 'Remove (delisted models can be removed manually)',
  '已下架（保留在列表，标记显示）': 'Delisted (kept in list, marked)',
  '已从上游官方列表移除，实测可能仍可用；同步只标记不删除，上游报模型不存在时自动清理': 'Removed from the upstream official list but may still work; sync only marks it, and it is auto-cleaned when the upstream reports the model no longer exists',
  '请输入账号数据': 'Enter account data',
  '导入完成': 'Import done',
  '导入失败: ': 'Import failed: ',
  '导入了 ': 'Imported ',
  ' 个账号': ' accounts',
  '点击复制': 'Click to copy',
  '密钥已生成': 'Key generated',
  '生成失败: ': 'Generate failed: ',
  '确定删除此密钥？': 'Delete this key?',
  '密钥已删除': 'Key deleted',
  '确定删除所有 API 密钥？': 'Delete ALL API keys?',
  '全部密钥已删除': 'All keys deleted',
  '已复制到剪贴板': 'Copied to clipboard',
  '配置已更新': 'Config updated',
  '更新失败: ': 'Update failed: ',
  '已保存，正在重启监听...': 'Saved, restarting listener...',
  '监听已切换，请通过 ': 'Listener switched, use ',
  ' 访问管理后台': ' to access the admin panel',
  '保存失败: ': 'Save failed: ',
  '密码至少 4 位': 'Password must be at least 4 characters',
  '密码已设置，后台需要重新登录': 'Password set; admin requires re-login',
  '已清除密码': 'Password cleared',
  '存在有值无键的行，已忽略': 'Rows with a value but no key were ignored',
  '请求头已保存': 'Headers saved',
  '加载失败': 'Failed to load',
  '请输入模型 ID': 'Enter a model ID',
  '模型已添加: ': 'Model added: ',
  '确认删除模型 ': 'Delete model ',
  '模型已删除: ': 'Model deleted: ',
  '版本 ': 'Version ',
  '（本机网卡）': ' (local NIC)',
  '已启用': 'Enabled',
  '完成': 'Done',
  '失败': 'Failed',
  'Token 未知': 'Token unknown',
  '加载日志失败: ': 'Failed to load logs: ',
  '测试中...': 'Testing...',
  '启动中...': 'Starting...',
  '从 Cline 同步模型': 'Sync Models from Cline',
  '模型同步': 'Model Sync',
  '上次同步': 'Last synced',
  '从未同步': 'Never synced',
  '新增模型': 'Added models',
  '移除模型': 'Removed models',
  '模型无变化': 'No model changes',
  '模型列表已更新': 'Model list updated',
  '上下文窗口 / 最大输出 (token)，点击⚙修改': 'Context window / max output (tokens), click ⚙ to edit',
  '上下文窗口 (token)': 'Context window (tokens)',
  '最大输出 (token)': 'Max output (tokens)',
  '已保存': 'Saved',
  '保存失败': 'Save failed',
  '同步中...': 'Syncing...',
  '保存中...': 'Saving...',
  '添加中...': 'Adding...',
  '导入中...': 'Importing...',
  '生成中...': 'Generating...',
  '克隆中...': 'Cloning...',
  '刷新中...': 'Refreshing...',
  '暂无模型': 'No models',
  '模型统计': 'Model Usage',
  '按模型统计（仅免费模型）': 'Per-model usage (free models)',
  '展开': 'Expand',
  '收起': 'Collapse',
  '模型冷却中': 'Model cooling',
  '模型冷却中，约 ': 'Model cooling, ~',
  '后释放': ' until release',
  '暂无数据': 'No data yet',
  // opencode 免费模型
  '从 opencode 同步模型': 'Sync Models from opencode',
  'opencode 模型列表已更新': 'opencode model list updated',
  'opencode 免费模型 · 今日用量': 'opencode Free Models · Today',
  '今日请求数': 'Requests today',
  '今日输入 Token': 'Input tokens today',
  '今日输出 Token': 'Output tokens today',
  '今日总 Token': 'Total tokens today',
  '启用 opencode': 'Enable opencode',
  '启用': 'Enable',
  '停用': 'Disable',
  'API Key': 'API Key',
  'API 密钥': 'API Keys',
  'Base URL': 'Base URL',
  '最大并发': 'Max concurrency',
  '重试次数': 'Retries',
  '故障转移': 'Failover',
  '开启（连续失败后暂走 Cline 池）': 'On (temporarily route to Cline pool after consecutive failures)',
  '关闭': 'Off',
  '失败阈值（次）': 'Failure threshold',
  '转移窗口（分钟）': 'Failover window (min)',
  '自动上下文压缩': 'Auto context compaction',
  '开启（超限自动摘要）': 'On (auto summary when over limit)',
  '压缩缓冲 Token': 'Compaction buffer (tokens)',
  '尾部保留 Token': 'Tail keep tokens',
  '摘要最大 Token': 'Max summary tokens',
  '保存 opencode 配置': 'Save opencode Config',
  '手动保存 opencode 配置': 'Manually save opencode Config',
  '上述字段改动后会自动保存，也可点按钮手动触发。': 'Fields above are auto-saved on change; you can also trigger a save manually with the button.',
  'opencode 配置已保存': 'OpenCode config saved',
  'opencode 出口代理': 'OpenCode Egress Proxies',
  '发往 opencode 的请求可经代理池轮询出口；命中限流时冷却当前出口并自动跳过。支持 http / https / socks5 / socks5h，每行一个，如 ': 'Requests to opencode can egress through a rotating proxy pool; the current proxy is cooled down and skipped on rate limits. Supports http / https / socks5 / socks5h, one per line, e.g. ',
  '发往 opencode 的请求可经代理池轮询出口；命中限流时冷却当前出口并自动跳过。支持 http / https / socks5 / socks5h，每行一个，如': 'Requests to opencode can egress through a rotating proxy pool; the current proxy is cooled down and skipped on rate limits. Supports http / https / socks5 / socks5h, one per line, e.g.',
  'opencode 免费模型': 'opencode Free Models',
  '＋ 添加 Provider': '＋ Add Provider',
  '名称 *': 'Name *',
  '免费': 'Free',
  '付费': 'Paid',
  '版本 dev': 'Version dev',
  'JSON 数组格式：[{"refreshToken":"...","email":"..."}]': 'JSON array format: [{"refreshToken":"...","email":"..."}]',
  'API Key: <生成的密钥>': 'API Key: <generated key>',
  '代理策略': 'Proxy strategy',
  'Provider 列表': 'Providers',
  '批量编辑': 'Bulk Edit',
  '批量编辑（每行 Key: Value）': 'Bulk edit (one Key: Value per line)',
  '应用并保存': 'Apply & Save',
  '名称': 'Name',
  '取消': 'Cancel',
  '保存 Provider': 'Save Provider',
  '添加 Provider': 'Add Provider',
  '编辑 Provider': 'Edit Provider',
  '本地服务器可留空）': 'leave empty on local server)',
  '超时(秒)': 'Timeout (s)',
  '优先级（小=优先）': 'Priority (lower = first)',
  '分钟请求上限（0=不限）': 'RPM limit (0 = unlimited)',
  '每日请求上限（0=不限）': 'Daily request limit (0 = unlimited)',
  '配额提示：填入免费层限额后，路由会在派发前自动避开打满额度的上游，改用组内其他 provider，不再拿 429 试错': 'Quota hint: set the free-tier cap and routing avoids exhausted upstreams before dispatch, failing over to other providers in the group instead of discovering 429s by trial',
  '免费来源（供统计）': 'Free source (for stats)',
  '模型 ID（逗号分隔，如 z-ai/glm-5.3-flash, deepseek/deepseek-v4-flash）': 'Model IDs (comma-separated, e.g. z-ai/glm-5.3-flash, deepseek/deepseek-v4-flash)',
  '自定义请求头（每行 Key: Value，如 HTTP-Referer: https://mysite.com）': 'Custom headers (one Key: Value per line, e.g. HTTP-Referer: https://mysite.com)',
  '请求头（每行一个，格式 Key: Value）': 'Headers (one per line, format Key: Value)',
  '保存 opencode 请求头': 'Save opencode Headers',
  'opencode 请求头已保存': 'opencode headers saved',
  '附加到发往 opencode zen 的请求。特殊值：': 'Attached to requests sent to opencode zen. Special values: ',
  '附加到发往 opencode zen 的请求。特殊值：$session / $request / $project / $client 会注入每请求的动态身份；留空删除该头。': 'Attached to requests sent to opencode zen. Special values: $session / $request / $project / $client inject per-request dynamic identity; an empty value deletes the header.',
  '接入任意 OpenAI 兼容上游（OpenRouter / Groq / Cerebras / Gemini / vLLM 等），含设备码登录连接；模型命中时优先走该上游，失败按回退链降级，最终兜底 Cline 池': 'Connect any OpenAI-compatible upstream (OpenRouter / Groq / Cerebras / Gemini / vLLM, etc.), including device-code logins. Matching models route to the provider first; failures fall back along the chain and finally to the Cline pool.',
  'opencode 请求头（模拟官方客户端）': 'opencode Headers (mimic official client)',
  '粘贴 refreshToken': 'Paste refreshToken',
  '如 deepseek/deepseek-v4-flash': 'e.g. deepseek/deepseek-v4-flash',
  '留空保存 = 清除密码': 'leave empty to save = clear password',
  '多账号轮询（轮询/填满/随机）': 'Multi-account rotation (round-robin/fill/random)',
  'OpenAI & Anthropic 双协议': 'OpenAI & Anthropic dual protocol',
  '429 冷却自动恢复': 'Automatic 429 cooldown recovery',
  '账号导出/导入（跨设备迁移）': 'Account export/import (cross-device migration)',
  'OAuth 系统浏览器登录': 'OAuth via system browser',
  '请求日志与统计': 'Request logs & stats',
  'System Prompt 覆盖': 'System Prompt override',
  '跨平台桌面端（Win/Mac/Linux）': 'Cross-platform desktop (Win/Mac/Linux)',
  '⚠️ 当前监听非本机回环地址（0.0.0.0 或局域网 IP），管理后台无鉴权，局域网内任何设备都可访问。请确认网络环境安全，或配合防火墙限制端口。': '⚠️ Listening on a non-loopback address (0.0.0.0 or LAN IP) exposes the admin panel without authentication to the whole network. Ensure the network is trusted or restrict the port via firewall.',
  '概览': 'Overview',
  '账户': 'Accounts',
  '配置': 'Configuration',
  '系统': 'System',
  '路由与模型': 'Routing & Models',
  '上游服务': 'Upstreams',
  '密钥与安全': 'Keys & Security',
  '默认模型、轮询策略、回退链与可用模型': 'Default model, rotation strategy, fallback chain & models',
  'opencode zen、Cline 出口代理与请求头': 'opencode zen, Cline egress proxies & headers',
  'Cline 请求头': 'Cline Request Headers',
  'opencode 请求头': 'OpenCode Request Headers',
  '请求头配置': 'Headers',
  '自定义 Provider': 'Providers',
  'API 密钥、访问控制与危险操作': 'API keys, access control & danger zone',
  '从预设添加（免费源）': 'Add from presets (free tiers)',
  '测试连通性': 'Test connection',
  '暂无自定义 Provider': 'No custom providers yet',
  '出口冷却状态': 'Egress cooldowns',
  '代理列表': 'Proxy list',
  '无冷却': 'None cooling',
  '已停用': 'Disabled',
  '故障转移中（opencode 暂不可用，请求走 Cline 池）': 'Failover active (opencode unavailable, requests routed to Cline pool)',
  '正常': 'Healthy',
  '已同步模型': 'Models synced',
  '接入 opencode（zen）免费模型。按请求中的模型名自动分流：免费模型走 opencode 上游，付费模型直接拒绝，其余走 Cline 账号池。': 'Integrates opencode (zen) free models. Requests are routed automatically by model name: free models go to the opencode upstream, paid models are rejected, everything else goes to the Cline account pool.',
  // Cline 出口代理
  'Cline 出口代理': 'Cline Egress Proxies',
  '国内直连 Cline 上游会被跨区限制；配置后所有发往 Cline 的请求（对话、登录/令牌刷新、模型同步）经代理池轮询出去。支持 http / https / socks5 / socks5h，每行一个，如 ': 'Cline upstream is region-restricted from mainland China; once configured, all requests to Cline (chat, login/token refresh, model sync) egress through a rotating proxy pool. Supports http / https / socks5 / socks5h, one per line, e.g. ',
  '。回环 / 内网地址（本机 Ollama 等自定义 Provider）始终直连；未配置时依次尝试环境变量代理、直连。': '. Loopback / private addresses (local Ollama and other custom providers) always connect directly; without config, env proxies then direct connection are tried.',
  '保存 Cline 代理': 'Save Cline proxies',
  'Cline 代理配置已保存': 'Cline proxy config saved',
  // 模型分组
  'opencode · 免费模型': 'opencode · Free Models',
  'opencode · 付费模型': 'opencode · Paid Models',
  'Cline · 免费模型': 'Cline · Free Models',
  'Cline · 付费模型': 'Cline · Paid Models',
  '用户自定义': 'User Custom',
  '点击展开/折叠': 'Click to expand/collapse',
  // 模型管理（提供商分组 + 启停开关）
  '路由配置': 'Routing',
  '模型管理': 'Models',
  '管理从 Cline / opencode 同步的模型与自定义模型。停用的模型不会出现在客户端模型列表（/v1/models）中，直接请求也会被拒绝。': 'Manage models synced from Cline / opencode and custom models. Disabled models are hidden from the client model list (/v1/models) and rejected when requested.',
  '全部提供商': 'All providers',
  '全部计费': 'All billing',
  '全部状态': 'All statuses',
  '全部启用': 'Enable all',
  '全部停用': 'Disable all',
  '无匹配模型': 'No matching models',
  '点击启用该模型': 'Click to enable this model',
  '点击停用该模型': 'Click to disable this model',
  '自定义': 'Custom',
  '其他': 'Other',
  '＋ 添加模型': '＋ Add Model',
  // 模型组合（Combo）
  '模型组合': 'Model Combos',
  '＋ 新建组合': '＋ New Combo',
  '新建组合': 'New Combo',
  '编辑组合': 'Edit Combo',
  '查看账号列表': 'View accounts',
  '基础连接': 'Connection',
  '请求调优': 'Request tuning',
  '上下文压缩': 'Context compaction',
  '把多个模型按优先级编排成组合（Combo）：命中组合时按顺序依次尝试，一个模型失败（限流/不可用）就自动切到下一个。停用的条目会被直接跳过。': 'Arrange several models into a priority-ordered combo. A matched combo is tried in order; when one model fails (rate-limited/unavailable) it automatically moves to the next. Disabled entries are skipped.',
  '组合 ID': 'Combo ID',
  '组合 ID *': 'Combo ID *',
  '描述': 'Description',
  '可选': 'optional',
  '分类': 'Category',
  'coding（可选）': 'coding (optional)',
  '紧急回退': 'Emergency fallback',
  '允许跳出组合回退': 'Allow fallback outside the combo',
  '开启「紧急回退」后，仅当组合内所有条目都失败时，才允许继续回退到配置的全局回退链 / 内置免费链；关闭则组合内失败即结束。': 'When "Emergency fallback" is on, only after every entry in the combo fails is it allowed to fall through to the configured global fallback chain / built-in free chain. When off, a combo failure ends the request.',
  '模型条目（按优先级从上到下）': 'Model entries (priority, top to bottom)',
  '＋ 添加条目': '＋ Add entry',
  '优先级预览': 'Priority preview',
  '（暂无启用的条目）': '(no enabled entries)',
  '仅显示已启用的条目，停用的条目会被跳过。': 'Only enabled entries are shown; disabled entries are skipped.',
  '保存组合': 'Save Combo',
  '组合已保存': 'Combo saved',
  '暂无模型组合': 'No model combos yet',
  '条目': 'Entries',
  '编辑': 'Edit',
  '克隆': 'Clone',
  '已克隆': 'Cloned',
  '克隆失败: ': 'Clone failed: ',
  '确定删除该组合？': 'Delete this combo?',
  '未选择模型': 'No model selected',
  '目录中未找到': 'Not in catalog',
  '模型已停用': 'Model disabled',
  '尚未添加条目，点击下方「添加条目」开始。': 'No entries yet. Click "Add entry" below to start.',
  '启用/停用该条目': 'Enable/disable this entry',
  '上移': 'Move up',
  '下移': 'Move down',
  '移除': 'Remove',
  '— 选择模型 —': '— Select model —',
  '上游': 'Upstream',
  '自动': 'Auto',
  '已使用': 'in use',
  '请填写组合 ID': 'Enter a combo ID',
  '请填写名称': 'Enter a name',
  '该 ID 为保留别名，请更换': 'This ID is a reserved alias; choose another',
  '条目中存在重复模型: ': 'Duplicate model in entries: ',
  // 组合编辑器：优先级提示 / 拖拽 / 多选添加弹窗
  '顺序即优先级：第 1 个最优先尝试（1st tried → 2nd → …），拖动行首 ⠿ 手柄或用 ↑↓ 按钮调整顺序。': 'Order = priority: entry 1 is tried first (1st tried → 2nd → …). Drag the row handle or use the up/down buttons to reorder.',
  '⊕ 多选添加模型': '⊕ Multi-select models',
  '从模型目录勾选多个模型，确认后按勾选顺序追加。': 'Tick models from the catalog; on confirm they are appended in tick order.',
  '拖动调整顺序': 'Drag to reorder',
  '已调整顺序': 'Order updated',
  '选择模型': 'Select Models',
  '勾选多个模型；确认后按勾选顺序追加到组合，勾选顺序即尝试优先级。': 'Tick one or more models; on confirm they are appended to the combo in tick order (tick order = try priority).',
  '搜索模型 / Provider...': 'Search models / providers...',
  '更多操作': 'More actions',
  '全选': 'Select All',
  '清空': 'Clear',
  '已添加': 'Added',
  '已在组合条目中，无需重复添加': 'Already in this combo',
  '勾选顺序即尝试优先级': 'Tick order = try priority',
  '已选 ': 'Selected ',
  '请先勾选至少一个模型': 'Select at least one model first',
  '替换整表': 'Replace All',
  '确认追加': 'Append Selected',
  '已按勾选顺序替换全部条目': 'All entries replaced in tick order',
  '已按勾选顺序追加 ': 'Appended in tick order: ',
  ' 个条目': ' entries',
  '模型目录为空，请先同步模型': 'Model catalog is empty; sync models first',
  // 请求日志：组合与尝试链路
  '组合': 'Combo',
  '回退': 'Fallback',
  '成功': 'Success',
  '跳过': 'Skipped',
  '尝试链路': 'Attempt trace',
  '点击查看尝试链路': 'Click to view the attempt trace',
  '请求: ': 'Requested: ',
  // 模型管理：两级分组 + 免费导向批量开关
  '全部来源': 'All sources',
  'Cline': 'Cline',
  'Zen (opencode)': 'Zen (opencode)',
  '启用全部免费': 'Enable all free',
  '停用全部付费': 'Disable all paid',
  '确定启用全部免费模型？': 'Enable all free models?',
  '确定停用全部付费模型？': 'Disable all paid models?',
  '确定启用该分组全部模型？': 'Enable all models in this group?',
  '确定停用该分组全部模型？': 'Disable all models in this group?',
  '没有符合条件的模型': 'No matching models',
  // 补齐缺失的 t() 文案（此前 EN 模式回落为中文）
  ' 个': ' failed',
  ' 个请求头': ' headers',
  'Provider 已保存': 'Provider saved',
  'Provider 未找到': 'Provider not found',
  '冷却 · 剩余 ': 'Cooldown · remaining ',
  '已保存 ': 'Saved ',
  '已删除': 'Deleted',
  '已填充预设，请补 API Key 和模型 ID': 'Preset applied; add the API Key and model ID',
  '从预设添加 Provider': 'Add provider from presets',
  '来自 ExtremeRouter 目录，按接入成本分组。点击预设填充表单；免密钥源会自动拉取模型。': 'From the ExtremeRouter catalog, grouped by access cost. Click a preset to fill the form; no-key sources auto-fetch models.',
  '搜索预设...': 'Search presets...',
  '免密钥直连': 'No-key direct',
  '免费额度': 'Free quota',
  '含免费模型': 'Includes free models',
  '本地服务': 'Local servers',
  '免密钥': 'No key',
  '含免费': 'Some free',
  '本地': 'Local',
  '个模型': 'models',
  '没有匹配的预设': 'No matching presets',
  '无需 API Key；应用后自动拉取模型': 'No API key needed; models auto-fetch after apply',
  '注册领取免费 API Key': 'Register to claim a free API key',
  '混合目录：需 Key，部分模型免费': 'Mixed catalog: key required, some models free',
  '本机推理端点（LM Studio、llama.cpp 等）': 'Local inference endpoints (LM Studio, llama.cpp, etc.)',
  '免密钥预设：正在自动拉取模型…': 'No-key preset: fetching models…',
  '导入分级预览': 'Import tier preview',
  '请先保存 Provider 再分级导入': 'Save the provider first, then run the tier import',
  '扫描中...': 'Scanning...',
  '上游拉取失败': 'Upstream fetch failed',
  '定价为 0': 'Zero pricing',
  'id 后缀 free': 'id suffix free',
  '免费标签/白名单': 'Free tag/allowlist',
  '整源免费': 'Whole source free',
  '无免费信号 → 付费': 'No free signal → paid',
  '层级（免费/付费）': 'Tier (free/paid)',
  '当前判定': 'Current decision',
  '手动设置': 'Manually set',
  '新增': 'New',
  '层级变化': 'Tier change',
  '无变化': 'No change',
  '受保护': 'Protected',
  '没有可导入的模型': 'No models to import',
  '个已选': ' selected',
  '已导入 ': 'Imported ',
  ' 个新模型，调整 ': ' new models, adjusted ',
  ' 个层级': ' tiers',
  '请先勾选要导入的模型': 'Select models to import first',
  // §8.3 登录连接（OAuth）
  '登录连接（OAuth）': 'Logins (OAuth)',
  '为需要登录签票的上游（如 Kilo Code）做设备码授权。登录成功后，在 Provider 编辑器的「账号连接」里选中该连接，派发时自动注入 Bearer 与组织头；Token 只存服务端，不写进 Provider。': 'Device-code authorization for upstreams that issue tokens by login (e.g. Kilo Code). After sign-in, choose the connection in the Account connection field of the provider editor; Bearer and org headers are injected at dispatch. Tokens stay server-side, never copied into the provider.',
  '设备码登录': 'Device-code login',
  '选择服务': 'Provider',
  '开始登录': 'Start login',
  '在浏览器中打开下面的地址并输入此代码，然后保持本窗口打开。': 'Open the URL below in your browser, enter this code, then keep this window open.',
  '等待授权...': 'Waiting for authorization...',
  '发起中...': 'Starting...',
  '发起登录失败: ': 'Login start failed: ',
  '代码已复制': 'Code copied',
  '粘贴已有的访问令牌（适合 Kilo 这类不签发刷新令牌的上游；过期后需重新导入或重新登录）。': 'Paste an existing access token (fits upstreams like Kilo that never issue refresh tokens; re-import or re-login when it expires).',
  '服务 *': 'Provider *',
  '邮箱（可选，用于去重）': 'Email (optional, for dedup)',
  'Org ID（可选）': 'Org ID (optional)',
  '保存连接': 'Save connection',
  '服务与 Token 必填': 'Provider and token are required',
  '连接已保存': 'Connection saved',
  '暂无登录连接': 'No logins yet',
  '服务': 'Provider',
  '方式': 'Method',
  '已登录': 'Signed in',
  '需重新登录': 'Re-login required',
  '导入 Token': 'Import Token',
  '可用': 'OK',
  '确定删除该连接？绑定它的 Provider 将不再注入 token。': 'Delete this connection? Bound providers will stop injecting the token.',
  '账号连接（可选）': 'Account connection (optional)',
  '不使用连接': 'No connection',
  '复制': 'Copy',
  '需登录': 'Login required',
  '设备码登录后，在编辑器「账号连接」中选择连接；无需 API Key': 'Sign in via device code, then choose the connection under Account connection in the editor; no API key needed',
  '已填充登录类预设：请在「登录连接（OAuth）」完成设备码登录，再在编辑器「账号连接」选择连接': 'Login preset applied: complete a device-code login under Logins (OAuth), then pick the connection in Account connection',
  '新密钥已生成（点击复制）': 'New key generated (click to copy)',
  '暂无 API 密钥': 'No API keys yet',
  '暂无请求日志': 'No request logs yet',
  '最后使用：': 'Last used: ',
  '模型同步失败': 'Model sync failed',
  '还没有账号': 'No accounts yet',
  '支持 OAuth 登录 / 手动 Token / 批量导入': 'Supports OAuth login / manual token / batch import',
  '用设备码登录或导入已有 Token': 'Sign in with a device code or import an existing token',
  '点击预设快速填充': 'Click a preset to fill these in',
  '确定删除该 Provider？': 'Delete this Provider?',
  '（无可用模型）': '(no models available)',
  '上一页': 'Previous page',
  '下一页': 'Next page',
  ' Tok · ': ' tok · ',
  ' 次': ' req',
  // Provider 编辑器：测试/拉取模型（老接口 404 → 提示重启代理）
  '拉取模型': 'Fetch models',
  '拉取中...': 'Fetching...',
  '搜索模型...': 'Search models...',
  '确认添加': 'Add selected',
  '来自上游 /models 接口；确认后追加到下方「模型 ID」输入框，仍需手动保存。Provider 中已有的模型预勾选且不可再选。失败时先检查 Base URL 与 API Key。': 'From the upstream /models endpoint; confirmed models are appended to the Model IDs field below, then save manually. Models already on this provider are pre-checked and locked. If it fails, check Base URL and API Key first.',
  '未获取到模型：请先检查 Base URL 与 API Key': 'No models returned: check Base URL and API Key first',
  '接口不可用，请重启代理更新到新版本后再试': 'Endpoint unavailable — restart the proxy to load the new version, then retry',
  '没有匹配的模型': 'No matching models',
  '未找到该组合': 'Combo not found',
  '已在 Provider 中，无需重复添加': 'Already in this provider',
  '所选模型均已在 Provider 中': 'All selected models are already in this provider',
  // 组合连通性测试弹窗
  '组合测试': 'Combo Test',
  '按优先级逐条探测；可在结果中直接启停/移除条目，保存更改后才实际落盘，取消即放弃修改。': 'Entries are probed in priority order. Enable/disable or remove entries right in the results — save applies the changes, cancel discards them.',
  '✓ 全部通过': '✓ All passed',
  '保存更改': 'Save Changes',
};
let LANG = 'zh';
const LC = () => LANG === 'en' ? 'en-US' : 'zh-CN';
function detectLang(){
  try { const m = document.cookie.match(/cline_admin_lang=(zh|en)/); if (m) return m[1]; } catch(e){}
  try { const l = localStorage.getItem('cline_admin_lang'); if (l==='en'||l==='zh') return l; } catch(e){}
  return (navigator.language||'').toLowerCase().startsWith('zh') ? 'zh' : 'en';
}
function t(s){ if (LANG==='en' && s && I18N[s]) return I18N[s]; return s; }
// 反向映射：英文 → 中文（用于切回中文时还原静态文本）
const I18N_REV = Object.keys(I18N).reduce((acc, k) => { acc[I18N[k]] = k; return acc; }, {});
function applyLang(){
  if (LANG === 'en') {
    document.title = 'Cline Proxy Admin';
  } else {
    document.title = 'Cline 代理管理面板';
  }
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  const nodes = [];
  while (walker.nextNode()) nodes.push(walker.currentNode);
  const dict = (LANG === 'en') ? I18N : I18N_REV;
  for (const n of nodes){
    const txt = n.nodeValue;
    if (!txt) continue;
    const trimmed = txt.trim();
    if (trimmed && dict[trimmed] && dict[trimmed] !== trimmed){
      n.nodeValue = txt.replace(trimmed, dict[trimmed]);
    }
  }
  document.querySelectorAll('[title]').forEach(el=>{
    const k=(el.getAttribute('title')||'').trim();
    if (k && dict[k]) el.setAttribute('title', dict[k]);
  });
  _('langZh').classList.toggle('active', LANG === 'zh');
  if (_('langEn')) _('langEn').classList.toggle('active', LANG === 'en');
}
function setLang(l){
  LANG = l;
  try { document.cookie = 'cline_admin_lang='+l+';path=/admin;max-age=31536000'; } catch(e){}
  try { localStorage.setItem('cline_admin_lang', l); } catch(e){}
  document.documentElement.lang = (l==='en')?'en':'zh-CN';
  // 即时切换：静态文本 + title 属性翻译 + 重新渲染动态数据，不整页刷新
  applyLang();
  loadStats();
  loadAccounts();
  loadRequestLogs(true);
  if (_('modelsList')) loadModels();
  if (_('settingDefModel')) loadConfig();
  toast(l === 'en' ? 'Language switched to English' : '语言已切换为中文', 'success');
}
LANG = detectLang();
document.documentElement.lang = (LANG==='en')?'en':'zh-CN';
// ===== /i18n =====
const API = '/admin/api';

const _ = id => document.getElementById(id);
const esc = s => { const d=document.createElement('div'); d.textContent=s||''; return d.innerHTML; };
// 属性上下文转义：esc 不处理引号，注入到 onclick / value 等属性时需额外转义引号
const escAttr = s => esc(s).replace(/"/g, '&quot;').replace(/'/g, '&#39;');

const ACTIVE_TAB_KEY = 'cline_admin_tab';

function getActiveTab(){
  try { const h = location.hash.replace(/^#/,''); if (h) return h; } catch(e){}
  try { const v = sessionStorage.getItem(ACTIVE_TAB_KEY); if (v) return v; } catch(e){}
  return 'dashboard';
}
function setActiveTab(name){
  try { sessionStorage.setItem(ACTIVE_TAB_KEY, name); } catch(e){}
}
const formatNumber = n => new Intl.NumberFormat(LC()).format(n || 0);
const formatTokenCount = n => {
  const value = Number(n) || 0;
  if (value < 1000) return String(value);
  const units = [['B', 1e9], ['M', 1e6], ['K', 1e3]];
  for (const [unit, size] of units) {
    if (value >= size) return (value / size).toFixed(1).replace(/\.0$/, '') + unit;
  }
  return String(value);
};

function toast(msg, t) {
  const el = _('toast');
  el.textContent = msg;
  el.className = 'toast ' + t + ' show';
  setTimeout(() => el.classList.remove('show'), 3500);
}

// 格式化冷却倒计时：返回紧凑的 "3.2h" 格式
function formatCooldown(isoTime) {
  const until = new Date(isoTime);
  const diff = until - new Date();
  if (diff <= 0) return t('即将恢复');
  const hours = diff / 3600000;
  if (hours < 1) return Math.ceil(diff / 60000) + 'm';
  return hours.toFixed(1) + 'h';
}

// ========== 导航 ==========
function applyTab(name){
  // 非法/未知标签页（如手改 URL）回退到 dashboard，避免空白页
  if (!name || !_('tab-' + name)) name = 'dashboard';
  document.querySelectorAll('.nav-item').forEach(e => e.classList.toggle('active', e.dataset.tab === name));
  document.querySelectorAll('.tab-panel').forEach(e => e.style.display = 'none');
  const panel = _('tab-' + name);
  if (panel) panel.style.display = 'block';
  try { history.replaceState(null, '', '#' + name); } catch(e){}
  setActiveTab(name);
  if (name === 'dashboard') { applyLang(); loadStats(); loadAccounts(); }
  else if (name === 'accounts') loadAccounts();
  else if (name === 'logs') loadRequestLogs(true);
  else if (name === 'routing') { loadModels().then(() => { loadConfig(); if (routingActiveSub() === 'combos') loadCombos(); }); }
  else if (name === 'upstreams') { loadOcConfig(); loadClineProxyConfig(); loadZenHeaders(); }
  else if (name === 'providers') { loadProviders(); loadConnections(); }
  else if (name === 'security') { loadKeys(); loadConfig(); }
}

function switchTab(name) { applyTab(name); }

document.querySelectorAll('.nav-item').forEach(el => {
  el.addEventListener('click', () => {
    if (el.classList.contains('active')) return;
    applyTab(el.dataset.tab);
  });
});

// 从 URL/SessionStorage 恢复上次打开的标签页（统一在底部初始化时执行）

// ========== 侧栏排序（拖拽） ==========
const NAV_ORDER_KEY = 'cline_admin_nav_order';
function applyNavOrder() {
  let order = [];
  try { order = JSON.parse(localStorage.getItem(NAV_ORDER_KEY) || '[]'); } catch(e) {}
  if (!Array.isArray(order) || !order.length) return;
  const nav = document.querySelector('.nav-section').parentElement;
  const items = {};
  nav.querySelectorAll('.nav-item').forEach(el => { items[el.dataset.tab] = el; });
  // 重排：按保存的顺序把条目移回其分区末尾，未知 tab 忽略
  order.forEach(tab => {
    const el = items[tab];
    if (!el) return;
    const sec = el.closest('.nav-section');
    sec.appendChild(el);
  });
}
function saveNavOrder() {
  const order = Array.from(document.querySelectorAll('.nav-item')).map(el => el.dataset.tab);
  try { localStorage.setItem(NAV_ORDER_KEY, JSON.stringify(order)); } catch(e) {}
}
function initNavDrag() {
  let dragged = null;
  document.querySelectorAll('.nav-item').forEach(el => {
    el.draggable = true;
    el.addEventListener('dragstart', e => { dragged = el; el.classList.add('dragging'); try { e.dataTransfer.setData('text/plain', el.dataset.tab); } catch(err){} e.dataTransfer.effectAllowed = 'move'; });
    el.addEventListener('dragend', () => { el.classList.remove('dragging'); document.querySelectorAll('.nav-item').forEach(x => x.classList.remove('drop-target')); dragged = null; });
    el.addEventListener('dragover', e => {
      e.preventDefault();
      if (!dragged || dragged === el) return;
      e.dataTransfer.dropEffect = 'move';
      document.querySelectorAll('.nav-item').forEach(x => x.classList.remove('drop-target'));
      el.classList.add('drop-target');
    });
    el.addEventListener('dragleave', () => el.classList.remove('drop-target'));
    el.addEventListener('drop', e => {
      e.preventDefault();
      if (!dragged || dragged === el) return;
      const sec = el.closest('.nav-section');
      const rect = el.getBoundingClientRect();
      const after = (e.clientY - rect.top) > rect.height / 2;
      sec.insertBefore(dragged, after ? el.nextSibling : el);
      dragged.classList.remove('dragging');
      document.querySelectorAll('.nav-item').forEach(x => x.classList.remove('drop-target'));
      saveNavOrder();
    });
  });
}
initNavDrag();
applyNavOrder();

// ========== 页内子导航（设置页顶部导航栏，划分功能子页） ==========
const SUBTAB_KEY_PREFIX = 'cline_admin_subtab_';
function switchPageSubTab(page, sub, skipSave) {
  const nav = document.querySelector('.page-subtabs[data-page="' + page + '"]');
  if (!nav) return;
  nav.querySelectorAll('.subtab').forEach(e => e.classList.toggle('active', e.dataset.sub === sub));
  const panel = nav.closest('.tab-panel');
  if (panel) {
    panel.querySelectorAll(':scope > .page-subpage').forEach(g => {
      g.style.display = (g.dataset.subpage === sub) ? '' : 'none';
    });
  }
  if (!skipSave) { try { localStorage.setItem(SUBTAB_KEY_PREFIX + page, sub); } catch(e) {} }
  if (!skipSave) onPageSubTabShown(page, sub);
}

// 子页显示时的按需加载钩子（用户点击切换时触发；首次进入由 applyTab 处理）
function onPageSubTabShown(page, sub) {
  if (page === 'routing' && sub === 'combos') loadCombos();
}

// 读取 routing 页当前激活的子页名（用于顶层标签切换后按需加载）
function routingActiveSub() {
  const nav = document.querySelector('.page-subtabs[data-page="routing"]');
  const el = nav && nav.querySelector('.subtab.active');
  return el ? el.dataset.sub : '';
}
document.querySelectorAll('.page-subtabs').forEach(nav => {
  nav.querySelectorAll('.subtab').forEach(el => {
    el.addEventListener('click', () => switchPageSubTab(nav.dataset.page, el.dataset.sub));
  });
  // 恢复上次选中的子页
  let saved = null;
  try { saved = localStorage.getItem(SUBTAB_KEY_PREFIX + nav.dataset.page); } catch(e) {}
  if (saved && saved !== nav.querySelector('.subtab.active')?.dataset.sub && nav.querySelector('.subtab[data-sub="' + saved + '"]')) {
    switchPageSubTab(nav.dataset.page, saved, true);
  }
});

// 导入子标签
document.querySelectorAll('#importTabs .tab').forEach(el => {
  el.addEventListener('click', () => {
    document.querySelectorAll('#importTabs .tab').forEach(e => e.classList.remove('active'));
    el.classList.add('active');
    document.querySelectorAll('#import-oauth,#import-token,#import-batch').forEach(e => e.classList.remove('active'));
    _('import-' + el.dataset.tab).classList.add('active');
  });
});

// ========== API 请求 ==========
async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
  const res = await fetch(API + path, opts);
  if (res.status === 401) {
    showLogin();
    throw new Error(t('需要登录'));
  }
  const data = await res.json();
  if (!data.success && data.error) throw new Error(data.error);
  return data;
}

// ========== 后台登录 ==========
function showLogin() {
  const ov = _('loginOverlay');
  if (ov && ov.style.display !== 'flex') {
    ov.style.display = 'flex';
    setTimeout(() => _('loginPassword').focus(), 50);
  }
}

async function submitLogin() {
  const pwd = _('loginPassword').value;
  if (!pwd) return;
  _('loginError').textContent = '';
  try {
    const res = await fetch(API + '/login', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: pwd })
    });
    const data = await res.json();
    if (res.ok && data.success) {
      location.reload();
    } else {
      _('loginError').textContent = data.error || t('登录失败');
      _('loginPassword').value = '';
    }
  } catch (e) { _('loginError').textContent = t('网络错误，请重试'); }
}

// 用系统默认浏览器打开外部链接（桌面 WebView 内导航不会跳外部浏览器，需走后端）
async function openExternal(url) {
  try {
    await api('GET', '/open-external?url=' + encodeURIComponent(url));
    toast(t('已在系统浏览器中打开'), 'success');
  } catch (e) { toast(t('打开失败: ') + e.message, 'error'); }
}

// ========== 仪表盘 ==========
async function loadStats() {
  try {
    const d = await api('GET', '/stats');
    const s = d.data;
    _('statTotal').textContent = s.total;
    _('statActive').textContent = s.active;
    _('statCooldown').textContent = s.cooldown;
    _('statExpired').textContent = s.expired;
    _('statPromptTokens').textContent = formatTokenCount(s.promptTokens);
    _('statCompletionTokens').textContent = formatTokenCount(s.completionTokens);
    _('statTotalTokens').textContent = formatTokenCount(s.totalTokens);
    _('statCachedTokens').textContent = formatTokenCount(s.cachedTokens);
    const oc = s.opencodeToday || {};
    _('statOcRequests').textContent = oc.requests != null ? oc.requests : '-';
    _('statOcInputTokens').textContent = formatTokenCount(oc.inputTokens || 0);
    _('statOcOutputTokens').textContent = formatTokenCount(oc.outputTokens || 0);
    _('statOcTotalTokens').textContent = formatTokenCount(oc.totalTokens || 0);
    if (s.version) _('settingVersion').value = s.version;
    // 设置项由 /config 返回为准；这里仅在用户未编辑时回填，避免轮询清掉正在输入的内容
    const fillIfIdle = (id, val) => { const el = _(id); if (el && document.activeElement !== el && !el.dataset.dirty) el.value = val; };
    if (s.strategy) fillIfIdle('settingStrategy', s.strategy);
    fillIfIdle('settingModelChain', (s.modelChain || []).join(', '));
  } catch (e) { toast(t('加载失败') + ': ' + e.message, 'error'); }
}

