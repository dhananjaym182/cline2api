
// ===== i18n =====
const I18N = {
  'Cline 代理': 'Cline Proxy',
  '多账号轮询 · 双协议': 'Multi-account rotation · Dual protocol',
  '管理': 'Admin',
  '仪表盘': 'Dashboard',
  '账号管理': 'Accounts',
  '请求日志': 'Request Logs',
  '设置': 'Settings',
  '关于': 'About',
  '反馈': 'Feedback',
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
  '逗号分隔模型 ID，可用 free 表示内置免费回退链；留空则自动回退到内置免费链。允许填写不在上方列表里的模型 ID（列表外的模型实测仍可用时可直接填入，同步列表只是参考）；失效条目由冷却与故障转移自动跳过': 'Comma-separated model IDs; use free for the built-in free fallback chain, or leave blank to fall back to it automatically. IDs outside the list above are allowed (if an unlisted model still works in practice, add it directly; the synced list is only a reference); dead entries are skipped by cooldown and failover automatically.',
  '轮询 (round_robin)': 'Round robin',
  '填满 (fill)': 'Fill',
  '随机 (random)': 'Random',
  '质量优先 (quality)': 'Quality-first (quality)',
  '加权轮询 (weighted_round_robin)': 'Weighted round robin',
  '调优参数': 'Tuning Parameters',
  '回退轮次、流式心跳/引导缓冲、会话亲和与请求体上限等运行期开关；留空或 0 表示沿用内置默认。保存后即刻生效（写入运行配置并持久化）。': 'Runtime switches: fallback rounds, SSE heartbeat / bootstrap buffer, session affinity, body limits. Blank or 0 keeps built-in defaults; saving applies immediately and persists.',
  '回退与冷却': 'Fallback & Cooldown',
  '最大回退轮次（0=单轮）': 'Max fallback rounds (0 = single pass)',
  '每轮账号上限（0=不限）': 'Max accounts per round (0 = unlimited)',
  '轮间隔上限(秒)': 'Max round interval (s)',
  '瞬时错误冷却(秒，-1=关闭)': 'Transient error cooldown (s, -1 = off)',
  '故障转移模型上限（0=内置3）': 'Max failover models (0 = built-in 3)',
  '流式与心跳': 'Streaming & Heartbeat',
  'SSE 心跳间隔(秒，0=15，-1=关闭)': 'SSE heartbeat interval (s, 0 = 15, -1 = off)',
  '首包前重试次数（0=1）': 'Pre-first-token retries (0 = 1)',
  '首内容超时(秒，0=关闭)': 'First-content timeout (s, 0 = off)',
  '非流式占位心跳(秒，0=关闭)': 'Non-stream placeholder keepalive (s, 0 = off)',
  '首 token 前抑制心跳': 'Suppress heartbeat before first token',
  '关闭（静默期发心跳）': 'Off (heartbeat while silent)',
  '开启（严格引导缓冲）': 'On (strict bootstrap buffer)',
  '会话亲和（同一对话绑定账号）': 'Session affinity (bind conversation to one account)',
  '开启': 'On',
  '亲和绑定 TTL(秒)': 'Affinity binding TTL (s)',
  '请求体与响应头': 'Request Body & Response Headers',
  '请求体上限(MB，0=不限)': 'Max request body (MB, 0 = unlimited)',
  '透传上游响应头（安全白名单外剔除）': 'Passthrough upstream response headers (unsafe ones stripped)',
  '模型别名 / 排除 / 错误规则': 'Model Aliases / Exclusions / Error Rules',
  '模型别名（请求名 -> 实际模型，可选限定 Provider）': 'Model aliases (requested name -> actual model, optional provider scope)',
  '错误规则（命中上游错误后执行的调度动作，按顺序匹配）': 'Error rules (routing action when an upstream error matches, evaluated in order)',
  '排除模型（逗号分隔，支持 * 通配；对全部来源生效）': 'Excluded models (comma-separated, * wildcard; applies to all sources)',
  // 结构化编辑器
  '别名': 'Alias',
  '目标模型': 'Target model',
  'Provider（可选）': 'Provider (optional)',
  '状态码 429,503': 'Status codes 429,503',
  '错误文本 / 正则': 'Error text / regex',
  '+ 添加别名': '+ Add alias',
  '+ 添加规则': '+ Add rule',
  '别名重复：保存会被拒绝': 'Duplicate aliases: saving will be rejected',
  '存在重复别名，请先修正': 'Duplicate aliases exist — fix them first',
  '搜索模型…': 'Search models…',
  '模型列表尚未同步，先前往模型页同步后再勾选': 'Model list not synced yet — sync it on the Models page first',
  '仅显示前 200 项，请缩小搜索范围': 'Showing first 200 only — narrow the search',
  '保存调优参数': 'Save Tuning',
  '非法别名/规则会整次拒绝并提示，不会部分生效': 'Invalid aliases/rules reject the whole save with an error; no partial apply',
  '调优参数已保存': 'Tuning parameters saved',
  '出口代理（空=继承全局；- 表示直连）': 'Egress proxy (blank = inherit global; - = direct)',
  'http://127.0.0.1:7890 或 -': 'http://127.0.0.1:7890 or -',
  '权重（加权轮询；0=按1，-1=排除）': 'Weight (weighted round robin; 0 = 1, -1 = excluded)',
  '引擎版本': 'Engine Version',
  '账号文件': 'Account File',
  '刷新当前页': 'Refresh this page',
  '构建信息': 'Build info',
  'API 地址': 'API address',
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
  '在「账号与连接」页展开「登录与导入」分区，通过 OAuth 登录或手动输入 refreshToken 添加账号。支持批量导入。': 'On the Accounts page, expand the Cline Login and Import section and add via OAuth or a pasted refreshToken. Batch import supported.',
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
  '主导航': 'Main navigation',
  '语言': 'Language',
  '搜索邮箱': 'Search email',
  'API Key: <生成的密钥>': 'API Key: <generated key>',
  '搜索模型': 'Search models',
  '搜索预设': 'Search presets',
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
  '新增模型': 'Added models',
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
  '按模型统计': 'Per-model usage',
  'Cline 官方': 'Cline official',
  '未命名': 'Unnamed',
  '自定义/内置': 'Custom or built-in',
  '连接池轮转派发': 'Connection pool round-robin',
  '用量': 'Usage',
  '总量': 'Total tokens',
  '已选': 'selected',
  '展开': 'Expand',
  '收起': 'Collapse',
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
  '自定义请求头（每行 Key: Value，如 HTTP-Referer: https://mysite.com）': 'Custom headers (one Key: Value per line, e.g. HTTP-Referer: https://mysite.com)',
  '请求头（每行一个，格式 Key: Value）': 'Headers (one per line, format Key: Value)',
  '保存 opencode 请求头': 'Save opencode Headers',
  'opencode 请求头已保存': 'opencode headers saved',
  '附加到发往 opencode zen 的请求。特殊值：': 'Attached to requests sent to opencode zen. Special values: ',
  '附加到发往 opencode zen 的请求。特殊值：$session / $request / $project / $client 会注入每请求的动态身份；留空删除该头。': 'Attached to requests sent to opencode zen. Special values: $session / $request / $project / $client inject per-request dynamic identity; an empty value deletes the header.',
  '接入任意 OpenAI 兼容上游（OpenRouter / Groq / Cerebras / Gemini / vLLM 等），含设备码登录连接；模型命中时优先走该上游，失败按回退链降级，最终兜底 Cline 池': 'Connect any OpenAI-compatible upstream (OpenRouter / Groq / Cerebras / Gemini / vLLM, etc.), including device-code logins. Matching models route to the provider first; failures fall back along the chain and finally to the Cline pool.',
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
  '模型与路由': 'Models & Routing',
  '账户与安全': 'Accounts & Security',
  '账户': 'Accounts',
  '配置': 'Configuration',
  '系统': 'System',
  '路由与模型': 'Routing & Models',
  '上游服务': 'Upstreams',
  '策略与回退一览': 'Strategies and fallbacks at a glance',
  '本页「轮询策略」= 从 Cline 账号池取号的方式（quality 同时参与自定义 Provider 评分排序）；全局「回退模型链」是组合失败的最终兜底。其余两处策略选择器在别的页面：': 'Polling strategy (this page) = how accounts are picked from the Cline pool (quality also feeds custom provider scoring); the global fallback model chain is the last resort when combos fail. The other two strategy selectors live elsewhere:',
  '上游服务页：opencode / Cline 出口代理策略 = 多个出口代理地址之间的选择 →': 'Upstreams page: opencode / Cline egress proxy strategy = choice among proxy addresses →',
  '模型组合页：组合条目顺序与紧急回退 →': 'Model Combos page: combo entry order and emergency fallback →',
  '组合的回退顺序与紧急回退在「模型组合」页管理 →': 'Combo fallback order and emergency fallback are managed on the Model Combos page →',
  '全局回退链与账号轮询策略在「路由与模型」页管理 →': 'Global fallback chain and account rotation strategy are managed on the Routing & Models page →',
  '仅决定多个出口代理地址之间的选择；Cline 账号轮询与 Provider 评分在': 'Only picks among egress proxy addresses; Cline account rotation and provider scoring live on',
  '「路由与模型」': 'Routing and Models',
  '密钥与安全': 'Keys & Security',
  '默认模型、轮询策略、回退链与可用模型': 'Default model, rotation strategy, fallback chain & models',
  'opencode zen、Cline 出口代理与请求头': 'opencode zen, Cline egress proxies & headers',
  'Cline 请求头': 'Cline Request Headers',
  'opencode 请求头': 'OpenCode Request Headers',
  '请求头配置': 'Headers',
  '自定义 Provider': 'Providers',
  'API 密钥、访问控制与危险操作': 'API keys, access control & danger zone',
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
  '国内直连 Cline 上游会被跨区限制；配置后所有发往 Cline 的请求（对话、登录/令牌刷新、模型同步）经代理池轮询出去。支持 http / https / socks5 / socks5h，每行一个，如 socks5://127.0.0.1:1080。回环 / 内网地址（本机 Ollama 等自定义 Provider）始终直连；未配置时依次尝试环境变量代理、直连。': 'Cline upstream is region-restricted from mainland China; once configured, all requests to Cline (chat, login/token refresh, model sync) egress through a rotating proxy pool. Supports http / https / socks5 / socks5h, one per line, e.g. socks5://127.0.0.1:1080. Loopback / private addresses (local Ollama and other custom providers) always connect directly; without config, env proxies then direct connection are tried.',
  '保存 Cline 代理': 'Save Cline proxies',
  'Cline 代理配置已保存': 'Cline proxy config saved',
  // 模型分组
  '用户自定义': 'User Custom',
  '点击展开/折叠': 'Click to expand/collapse',
  // 模型管理（提供商分组 + 启停开关）
  '路由配置': 'Routing',
  '模型管理': 'Models',
  '管理从 Cline / opencode 同步的模型与自定义模型。停用的模型不会出现在客户端模型列表（/v1/models）中，直接请求也会被拒绝。': 'Manage models synced from Cline / opencode and custom models. Disabled models are hidden from the client model list (/v1/models) and rejected when requested.',
  '全部计费': 'All billing',
  '全部状态': 'All statuses',
  '全部启用': 'Enable all',
  '展开全部': 'Expand all',
  '折叠全部': 'Collapse all',
  '测试失败': 'Test failed',
  '启用当前筛选': 'Enable filtered',
  '停用当前筛选': 'Disable filtered',
  '上下文': 'Context',
  '确定启用当前筛选的全部模型？': 'Enable all models in the current filter?',
  '确定停用当前筛选的全部模型？': 'Disable all models in the current filter?',
  '全部停用': 'Disable all',
  '无匹配模型': 'No matching models',
  '点击启用该模型': 'Click to enable this model',
  '点击停用该模型': 'Click to disable this model',
  '自定义': 'Custom',
  '其他': 'Other',
  '＋ 添加模型': '＋ Add Model',
  // 模型组合（Combo）
  '模型组合': 'Model Combos',
  '新建组合': 'New Combo',
  '编辑组合': 'Edit Combo',
  '查看账号列表': 'View accounts',
  '基础连接': 'Connection',
  '请求调优': 'Request tuning',
  '上下文压缩': 'Context compaction',
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
  '扫描中...': 'Scanning...',
  '上游拉取失败': 'Upstream fetch failed',
  '定价为 0': 'Zero pricing',
  '上游 free 标志': 'Upstream free flag',
  'id 含 free 段': 'id contains free segment',
  'id 后缀 free': 'id suffix free',
  '免费标签/白名单': 'Free tag/allowlist',
  '整源免费': 'Whole source free',
  '上游定价非零': 'Nonzero upstream pricing',
  '模型名含 free': 'Model name contains free',
  '无免费信号 → 付费': 'No free signal → paid',
  '层级（免费/付费）': 'Tier (free/paid)',
  '当前判定': 'Current decision',
  '自动判定层级': 'Auto-decided tier',
  '手动设置': 'Manually set',
  '新增': 'New',
  '层级变化': 'Tier change',
  '无变化': 'No change',
  '受保护': 'Protected',
  '没有可导入的模型': 'No models to import',
  '本批判定依据：': 'Tier basis for this batch: ',
  '依据: ': 'Basis: ',
  '按操作过滤': 'Filter by action',
  '上游返回 prompt/completion 价格为 0': 'Upstream prompt/completion pricing is 0',
  '上游返回 free 标志或 mode=free': 'Upstream returned a free flag or mode=free',
  '模型 id 以 :free 或 -free 结尾': 'Model id ends with :free or -free',
  '模型 id 含独立的 free 词段': 'Model id contains a standalone free segment',
  '模型显示名含 free 词段': 'Model display name contains a free word',
  '免费标签或内置白名单命中': 'Free tag or built-in allowlist match',
  '整个 Provider 标记为免费（无更精确的逐模型信号）': 'Whole provider flagged free (no per-model signal)',
  '上游定价非零 → 付费': 'Nonzero upstream pricing → paid',
  '没有任何免费信号，保守归为付费': 'No free signal at all → conservatively paid',
  '按「新增 / 层级变化 / 无变化 / 受保护」分组，每行标注判定依据（悬停看规则）；只有新增与层级变化可勾选。': 'Grouped by New / Tier change / No change / Protected. Each row shows the tier basis (hover for the rule); only New and Tier change rows are selectable.',
  '个已选': ' selected',
  '已导入 ': 'Imported ',
  ' 个新模型，调整 ': ' new models, adjusted ',
  ' 个层级': ' tiers',
  '请先勾选要导入的模型': 'Select models to import first',
  // §8.3 登录连接（OAuth）
  '设备码登录': 'Device-code login',
  '选择服务': 'Provider',
  '开始登录': 'Start login',
  '在浏览器中打开下面的地址并输入此代码，然后保持本窗口打开。': 'Open the URL below in your browser, enter this code, then keep this window open.',
  '在浏览器中打开下面的链接并完成授权，然后保持本窗口打开。': 'Open the link below and approve in your browser, then keep this window open.',
  '等待授权...': 'Waiting for authorization...',
  '发起中...': 'Starting...',
  '发起登录失败: ': 'Login start failed: ',
  '代码已复制': 'Code copied',
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
  '已连接': 'Connected',
  '未连接': 'Not connected',
  '个账号': 'accounts',
  '个连接': 'connections',
  '连接': 'Connect',
  '导入 Token': 'Import Token',
  '可用': 'OK',
  '确定删除该连接？绑定它的 Provider 将不再注入 token。': 'Delete this connection? Bound providers will stop injecting the token.',
  '账号与连接': 'Accounts and Connections',
  '分开管理 Cline 账号池与 OAuth 登录连接，免费模型用量按来源汇总查看': 'Accounts and OAuth logins on separate subtabs, with pool-wide free-model usage grouped by source',
  '账号池': 'Accounts',
  '连接池': 'Connections',
  '模型用量': 'Model Usage',
  '汇总全部 Cline 账号服务过的免费模型，按来源分组展示请求与 Token 用量。': 'Free models served by all Cline accounts, grouped by source with request and token totals.',
  '暂无用量数据': 'No usage data yet',
  ' 个账号冷却中': ' accounts cooling',
  '免费模型用量见「模型用量」汇总': 'See the Model Usage summary for per-model totals',
  '展开模型用量': 'Open Model Usage',
  'Cline 账号池': 'Cline Account Pool',
  'OAuth 连接池': 'OAuth Connection Pool',
  '所有需要登录签票的上游账号（OAuth 设备码 / 粘贴 Token）统一在此管理：每个连接独立统计自己服务过的模型与用量，并按 Provider 分组。点击下方 Provider 卡片直接登录；已连接的 Provider 可继续「添加账号」，多个账号会一起参与轮转派发。登录后在 Provider 编辑器的「账号连接池」里勾选即可参与派发；Token 只存服务端，不写进 Provider。': 'All upstream accounts that need a login (OAuth device code / pasted token) are managed here: each connection tracks the models and usage it served, grouped by provider. Click a provider card below to sign in; connected providers also offer "Add Account", and multiple accounts join dispatch rotation together. After login, tick the connection in the provider editor account pool to include it; tokens stay server-side and are never copied into providers.',
  '登录与导入（Cline 账号）': 'Cline Login and Import',
  '为上方「Cline 账号池」添加账号：OAuth 浏览器登录、粘贴 refreshToken 或批量文件。第三方服务（Kilo、GitHub Copilot 等）的登录请使用「OAuth 连接池」。': 'Add accounts to the Cline pool above: OAuth browser login, a pasted refreshToken, or a batch file. For third-party services (Kilo, GitHub Copilot, etc.) use the OAuth connection pool above.',
  '账号连接池': 'Account connection pool',
  '勾选多个登录连接后轮转派发，被限流/待重登的连接自动跳过由兄弟连接顶上；不勾选 = 仅用 API Key。连接的增删在「账号与连接」页管理。': 'Tick multiple logins for round-robin dispatch; rate-limited or reauth-pending ones are skipped while siblings take over. Tick none to use the API key only. Connections are added and removed on the Accounts and Connections page.',
  '复制': 'Copy',
  '需登录': 'Login required',
  '设备码连接：在「账号与连接」添加，编辑器「账号连接池」勾选即可；无需 API Key': 'Device-code connection: add it under Accounts and Connections, then tick it in the editor pool; no API key needed',
  '已填充登录类预设：请到「账号与连接」完成设备码登录，再在编辑器「账号连接池」勾选连接': 'Login preset applied: complete the device-code login under Accounts and Connections, then tick the connection in the editor pool',
  '新密钥已生成（点击复制）': 'New key generated (click to copy)',
  '暂无 API 密钥': 'No API keys yet',
  '暂无请求日志': 'No request logs yet',
  '最后使用：': 'Last used: ',
  '模型同步失败': 'Model sync failed',
  '还没有账号': 'No accounts yet',
  '支持 OAuth 登录 / 手动 Token / 批量导入': 'Supports OAuth login / manual token / batch import',
  '用设备码登录或导入已有 Token': 'Sign in with a device code or import an existing token',
  '确定删除该 Provider？': 'Delete this Provider?',
  '（无可用模型）': '(no models available)',
  '上一页': 'Previous page',
  '下一页': 'Next page',
  ' 次': ' req',
  // Provider 编辑器：测试/拉取模型（老接口 404 → 提示重启代理）
  '拉取模型': 'Fetch models',
  '拉取中...': 'Fetching...',
  '搜索模型...': 'Search models...',
  '确认添加': 'Add selected',
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
  '未保存的更改': 'Unsaved changes',
  '放弃': 'Discard',
  '变更预览': 'Change Preview',
  '仅以下字段发生变化，确认后保存：': 'Only these fields changed — save after confirming:',
  '确认保存': 'Confirm Save',
  '敏感值已隐藏': 'sensitive value hidden',
  '有未保存的修改，确定放弃？': 'You have unsaved changes. Discard them?',
  '最大回退轮次': 'Max fallback rounds',
  '每轮账号上限': 'Max credentials per round',
  '瞬时错误冷却(秒)': 'Transient cooldown (s)',
  '故障转移模型上限': 'Max failover models',
  'SSE 心跳间隔(秒)': 'SSE keepalive (s)',
  '首包前重试次数': 'Bootstrap retries',
  '首内容超时(秒)': 'First-content timeout (s)',
  '会话亲和': 'Session affinity',
  '请求体上限(MB)': 'Max body (MB)',
  '透传上游响应头': 'Passthrough headers',
  '非流式占位心跳(秒)': 'Non-stream keepalive (s)',
  '模型别名': 'Model Aliases',
  '排除模型': 'Excluded Models',
  '错误规则': 'Error Rules',
  '检查中...': 'Checking...',
  '检查失败': 'Check failed',
  '发现新版本 ': 'New version available: ',
  '当前 ': 'current ',
  '开发版本': 'Dev build',
  '最新 ': 'latest ',
  '已是最新版本': 'Up to date',
  '检查更新': 'Check for updates',
  '更新检查': 'Update Check',
  '未检查': 'Not checked',
  '分钟请求上限': 'RPM Limit',
  '每日请求上限': 'Daily Limit',
  '出口代理': 'Egress Proxy',
  '权重': 'Weight',
  '自定义请求头': 'Custom Headers',
  '免费来源': 'Free Source',
  // 仪表盘改版：池容量 / 关注清单 / 主题与侧栏收起
  '账号池容量与用量总览': 'Pool capacity and usage at a glance',
  // 仪表盘总览（hero / 吞吐 / 上游分布）
  '窗口内请求': 'Requests in window',
  '窗口内请求 · 最近 ': 'requests · last ',
  '加载中…': 'Loading…',
  '成功失败占比': 'Success / failure ratio',
  '时间窗口': 'Time window',
  '平均时延': 'Avg latency',
  '首字延迟': 'Time to first token',
  '窗口 Token': 'Tokens in window',
  '降级切换': 'Fallbacks',
  '吞吐趋势': 'Throughput',
  '上游分布': 'Upstream Fleet',
  '热门模型': 'Top Models',
  '无法连接服务': 'Cannot reach server',
  '暂无流量': 'No traffic yet',
  '失败率偏高': 'High failure rate',
  '存在失败请求': 'Some requests failed',
  '运行正常': 'All good',
  '窗口内暂无请求流量': 'No request traffic in this window',
  '策略': 'Strategy',
  '默认': 'Default',
  '仅免费模型': 'Free models only',
  // 请求日志增强：筛选 / 跟随 / 详情 / 下载 / 清空
  '跟随新日志': 'Follow new logs',
  '已暂停跟随': 'Follow paused',
  '点击恢复自动跟随': 'Click to resume auto-follow',
  '已暂停跟随 · ': 'Paused · ',
  ' 条新日志': ' new logs',
  '恢复跟随': 'Resume following',
  '自动跟随新日志': 'Automatically follow new logs',
  '下载': 'Download',
  '下载当前筛选结果': 'Download current filtered results',
  '全屏': 'Fullscreen',
  '清空全部日志': 'Clear all logs',
  '详情': 'Details',
  '请求详情': 'Request details',
  '复制 JSON': 'Copy JSON',
  '关闭弹窗': 'Close',
  '全部协议': 'All protocols',
  '协议筛选': 'Protocol filter',
  '快捷筛选': 'Quick filters',
  '搜索模型 / 账号 / 请求 ID…': 'Search model / account / request ID…',
  '流式': 'Streaming',
  '暂无匹配日志': 'No logs match the filters',
  '双击复制 JSON': 'Double-click to copy JSON',
  '确定清空所有请求日志？此操作不可恢复。': 'Clear all request logs? This cannot be undone.',
  '清空中...': 'Clearing...',
  '日志已清空': 'Logs cleared',
  '清空失败: ': 'Clear failed: ',
  '当前无可下载日志': 'Nothing to download',
  '已下载 ': 'Downloaded ',
  ' 条日志': ' logs',
  '下载失败: ': 'Download failed: ',
  '输入/输出/缓存/总': 'Input/Output/Cached/Total',
  '错误': 'Error',
  '原始 JSON': 'Raw JSON',
  // Provider/账号健康：配额 / 冷却 / 热度 / 到期
  '健康': 'Health',
  '每日已用 ': 'Daily used ',
  '每分钟已用 ': 'Per-minute used ',
  '暂无样本': 'No samples',
  '成功率': 'Success rate',
  '样本': 'Samples',
  '即将恢复': 'Recovering soon',
  '剩 ': 'left ',
  '点击清除该 Provider 全部冷却': 'Click to clear all cooldowns for this provider',
  '冷却中的 Provider · 按最早恢复排序': 'Cooling providers · soonest recovery first',
  '已清除 ': 'Cleared ',
  ' 条冷却': ' cooldowns',
  '重置失败: ': 'Reset failed: ',
  'Token 已过期': 'Token expired',
  'Token 到期 ': 'Token expires ',
  '最近 ': 'Last ',
  ' 次成功 ': ' successes',
  '账号池容量': 'Pool Capacity',
  '累计请求': 'Total requests',
  '总数': 'Total',
  '需要关注': 'Needs Attention',
  '前往处理': 'Review',
  '已过期账号 · 需要重新导入或登录': 'Expired accounts — re-import or re-login needed',
  '冷却中账号 · 稍后自动恢复': 'Accounts cooling down — they recover automatically',
  '存在模型级冷却的账号': 'Accounts with model-level cooldowns',
  '需重新登录的连接': 'Connections needing re-login',
  '暂无需要处理的项目 · 账号池健康': 'Nothing needs attention — pool is healthy',
  '平均 ': 'Avg ',
  '/请求': '/req',
  '平均 -/请求': 'Avg -/req',
  '缓存率 ': 'Cache rate ',
  '缓存率 -': 'Cache rate -',
  '收起侧栏': 'Collapse sidebar',
  '展开侧栏': 'Expand sidebar',
  '浅色': 'Light',
  '深色': 'Dark',
  '主题': 'Theme',
  // 抽屉编辑器 + 组合独立页
  '基本信息': 'Basics',
  '调度与限额': 'Scheduling & Limits',
  '模型与请求头': 'Models & Headers',
  '行为与连接': 'Behavior & Connections',
  '把多个模型按优先级编排成回退链：命中组合按序尝试，失败（限流/不可用）自动切到下一个，停用条目直接跳过': 'Arrange models into a priority-ordered fallback chain: a matched combo is tried in order; on failure (rate-limit/unavailable) it moves to the next; disabled entries are skipped',
  '个组合': 'combos',
  '启用中': 'enabled',
  '条目总数': 'entries in total',
  '添加后用行内 ⚙ 可设置上下文窗口与层级': 'After adding, use the inline gear to set context window and tier',
  // W3-C 上游自动检测
  '最近检测': 'Last check',
  '未检测': 'Not checked',
  '重新检测': 'Re-check',
  '检测中...': 'Checking...',
  '检测失败': 'Check failed',
  '待审': 'pending',
  '个新模型待审': ' new model(s) pending review',
  '查看审阅': 'Review',
  '全部忽略': 'Ignore all',
  '已忽略全部待审批次': 'All pending batches ignored',
  '忽略失败': 'Ignore failed',
  '后台每 6 小时自动检测已启用 Provider 的上游模型清单': 'Background check of enabled provider upstream lists every 6 hours',
  '刚刚': 'just now',
  '分钟前': 'min ago',
  '小时前': 'h ago',
  '天前': 'd ago',
  '请先填写 Base URL': 'Fill in the Base URL first',
  '所选模型已并入，正在保存 Provider...': 'Selected models merged, saving the provider...',
  '批次不存在或已过期': 'Batch missing or expired',
  '确定忽略全部待审的新模型批次？': 'Ignore all pending new-model batches?',
  ' 个模型': ' models',
  '登录成功': 'Logged in',
  '全部': 'All',
  '未选择': 'None selected',
  '手动输入模型 ID（高级）': 'Type model IDs manually (advanced)',
  '从上游目录勾选（免费 / 付费分组），确认前不写入': 'Pick from the upstream catalog (grouped free / paid); nothing is written until you confirm',
  '来自上游 /models 接口，按免费 / 付费分组；确认后写入模型列表，仍需手动保存。Provider 中已有的模型预勾选且不可再选。失败时先检查 Base URL 与 API Key。': 'From the upstream /models endpoint, grouped by free / paid; confirmed picks go into the model list, saving is still manual. Models already on this provider are pre-checked and locked. If it fails, check Base URL and API Key first.',
  // ===== wave-5: audited static gaps =====
  '重置选中': 'Reset selected',
  '只显示免费模型': 'Free models only',
  '关闭（返回全部模型）': 'Off (returns all models)',
  '开启（/models 仅返回免费模型）': 'On (/models returns free models only)',
  '影响 /models、/v1/models 对客户端返回的模型列表，不影响管理页模型管理': 'Affects the model lists clients get from /models and /v1/models; admin model management is unaffected',
  '更多操作 ▾': 'More ▾',
  '批准导入所选': 'Approve selected imports',
  '粘贴已有访问令牌作为连接（Bearer 注入），适合 Kilo 这类不签发刷新令牌的上游，或任何无设备码流程、但你已持有 token 的服务；过期后需重新导入或登录。': 'Paste an existing access token as a connection (injected as Bearer). Fits upstreams like Kilo that never issue refresh tokens, or any service without a device-code flow where you already hold a token; re-import or re-login once it expires.',
  '模型 ID': 'Model ID',
  '确认': 'Confirm',
  '池容量分布': 'Pool capacity distribution',
  '搜索邮箱...': 'Search email...',
  '点击排序': 'Click to sort',
  '搜索模型 ID...': 'Search model ID...',
  '模型 ID，如 deepseek/deepseek-v4-flash': 'Model ID, e.g. deepseek/deepseek-v4-flash',
  'sk-...（本地服务器可留空）': 'sk-... (can be left blank for local servers)',
  'kilocode / 任意 provider key': 'kilocode / any provider key',
  '粘贴令牌（只存服务端，不外泄）': 'Paste token (stored server-side only, never leaked)',
  '访问密码': 'Access password',
  '优先级': 'Priority',
  'CodeBuddy CN（腾讯）': 'CodeBuddy CN (Tencent)',
  '多个 Provider 提供同名模型时，可在 Model 后追加 @Provider名 钉选来源，如 deepseek-chat@我的Provider（失败只报错、不悄悄换源）。': 'When several providers serve the same model id, append @ProviderName to Model to pin the source, e.g. deepseek-chat@MyProvider. A pinned request fails loudly instead of silently switching providers.',
  // §MC 快速开始 / 配额与预算 / 错误日志下载 / 会话清除 / 差异预览
  '快速开始': 'Quick Start',
  '配额与预算': 'Quota & Budget',
  '三步接入：添加账号 → 生成密钥 → 配置客户端': 'Three steps: add an account → generate an API key → configure your client',
  '接入步骤': 'Setup Steps',
  '前往账号与连接': 'Go to Accounts',
  '前往密钥与安全': 'Go to Keys & Security',
  '按下方客户端示例填入 Base URL 与密钥即可。': 'Fill in the Base URL and key from the client examples below.',
  '客户端配置示例': 'Client Configuration Examples',
  'Base URL 中的端口与监听地址以「关于」页 API 地址为准；多 Provider 同名模型可用': 'Use the port and listen address shown on the About page as the Base URL; for same-named models across providers use',
  '模型@Provider名': 'model@ProviderName',
  '钉选来源（失败只报错、不悄悄换源）。': 'to pin the source (fails loudly, never silently switches).',
  'Claude Code（Anthropic 协议）': 'Claude Code (Anthropic API)',
  'Cline / Roo Code（VS Code 插件）': 'Cline / Roo Code (VS Code extension)',
  '通用 OpenAI 兼容客户端': 'Generic OpenAI-compatible client',
  'curl 冒烟测试': 'curl smoke test',
  '下一步': 'Next Steps',
  '接入更多免费上游（OpenCode / Zen）': 'Connect more free upstreams (OpenCode / Zen)',
  '添加自定义 Provider': 'Add a custom Provider',
  '配置模型组合自动换源': 'Set up model combos with auto-fallback',
  '查看配额与预算': 'View Quota & Budget',
  'Provider 每分钟 / 每日预算用量、冷却恢复与账号 Token 总量': 'Per-provider RPM / daily budget usage, cooldown recovery and account token totals',
  '运行时长': 'Uptime',
  '· 启动于': '· started at',
  '添加账号 → 生成 API Key → 客户端配置': 'Add account → generate API key → configure client',
  '。完整步骤与各客户端示例见「快速开始」页。': '. Full steps and per-client examples live on the Quick Start page.',
  '打开快速开始': 'Open Quick Start',
  '下载错误日志': 'Download Error Logs',
  '仅下载失败请求': 'Failed requests only',
  '当前无失败日志': 'No failed logs to download',
  '后台登录会话': 'Admin Login Sessions',
  '清除全部登录会话': 'Clear all login sessions',
  '强制所有浏览器（包括当前页面）重新登录，凭据数据不受影响': 'Forces every browser (including this page) to log in again; credential data is untouched',
  '确认清除全部后台登录会话？所有浏览器（包括当前页面）都需要重新登录。': 'Clear all admin login sessions? Every browser, including this one, will need to log in again.',
  '已清除全部登录会话': 'All admin sessions cleared',
  '操作失败: ': 'Operation failed: ',
  '查看差异': 'View Diff',
  '无法生成差异预览': 'Cannot build diff preview',
  '每分钟请求': 'Requests / minute',
  '每日请求': 'Requests / day',
  '未设预算': 'no budget set',
  '无冷却': 'No cooldowns',
  '冷却中的模型': 'Models in cooldown',
  '重置全部冷却': 'Reset all cooldowns',
  '账号总数': 'Accounts',
  '活跃账号': 'Active',
  '总请求': 'Total requests',
  '总 Token': 'Total tokens',
  '距每日预算 UTC 重置': 'Until daily budget UTC reset',
  '暂无 Provider，先在「自定义 Provider」页添加': 'No providers yet — add one on the Custom Providers page first',
  '已重置 ': 'Reset ',
  ' 条冷却': ' cooldown(s)',
  '重置失败': 'Reset failed',
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
  // Wave-5: placeholder/aria-label carry user-visible copy too, so they follow
  // the same exact-match dictionary as text nodes and [title].
  document.querySelectorAll('[title],[placeholder],[aria-label]').forEach(el=>{
    for (const attr of ['title','placeholder','aria-label']) {
      const k=(el.getAttribute(attr)||'').trim();
      if (k && dict[k]) el.setAttribute(attr, dict[k]);
    }
  });
  _('langZh').classList.toggle('active', LANG === 'zh');
  if (_('langEn')) _('langEn').classList.toggle('active', LANG === 'en');
  const lp = LANG === 'en' ? 'false' : 'true';
  if (_('langZh')) _('langZh').setAttribute('aria-pressed', lp);
  if (_('langEn')) _('langEn').setAttribute('aria-pressed', LANG === 'en' ? 'true' : 'false');
  if (typeof syncNavCollapseBtn === 'function') syncNavCollapseBtn(); // 侧栏按钮 title 随语言更新
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

// §9.3 tier-signal provenance labels, shared by the provider import preview
// and the per-row badges on the Models page.
const _tierSignals = {
  'pricing-zero': '定价为 0', 'flag-free': '上游 free 标志', 'suffix-free': 'id 后缀 free', 'name-token-free': 'id 含 free 段', 'allowlist': '免费标签/白名单',
  'provider-free': '整源免费', 'display-name-free': '模型名含 free', 'pricing-paid': '上游定价非零', 'paid-default': '无免费信号 → 付费',
};

// Merge ids into a picker's checked-order array, skipping ids already present
// (selection order equals attempt priority).
function pickerMergeIds(order, ids) {
  for (const id of ids) if (order.indexOf(id) === -1) order.push(id);
}

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
// Wave-5: the standalone Import tab now lives as a collapsible section inside
// Accounts; old #import deep links and switchTab('import') callers resolve here.
function normalizeTabName(name) {
  return name === 'import' ? 'accounts' : name;
}

function toggleLoginImport(forceOpen) {
  const body = _('loginImportBody'), btn = _('loginImportToggle');
  if (!body || !btn) return;
  const open = (forceOpen === undefined) ? body.style.display === 'none' : !!forceOpen;
  body.style.display = open ? '' : 'none';
  btn.textContent = open ? '▾' : '▸';
  const label = open ? '收起' : '展开';
  btn.title = label;
  btn.setAttribute('aria-label', label);
  btn.setAttribute('aria-expanded', open ? 'true' : 'false');
  applyLang();
}

function toggleModelUsage(forceOpen) {
  const body = _('modelUsageBody'), btn = _('modelUsageToggle');
  if (!body || !btn) return;
  const open = (forceOpen === undefined) ? body.style.display === 'none' : !!forceOpen;
  body.style.display = open ? '' : 'none';
  btn.textContent = open ? '▾' : '▸';
  const label = open ? '收起' : '展开';
  btn.title = label;
  btn.setAttribute('aria-label', label);
  btn.setAttribute('aria-expanded', open ? 'true' : 'false');
  applyLang();
}

function showAccountLogin(subtab) {
  applyTab('accounts');
  switchPageSubTab('accounts', 'pool', true);
  toggleLoginImport(true);
  if (subtab) {
    const tab = document.querySelector('#importTabs .tab[data-tab="' + subtab + '"]');
    if (tab) tab.click();
  }
  const sec = _('loginImportSection');
  if (sec) setTimeout(() => sec.scrollIntoView({ behavior: 'smooth', block: 'start' }), 50);
}

function applyTab(name){
  const fromImport = (name === 'import');
  name = normalizeTabName(name);
  // 非法/未知标签页（如手改 URL）回退到 dashboard，避免空白页
  if (!name || !_('tab-' + name)) name = 'dashboard';
  document.querySelectorAll('.nav-item').forEach(e => {
    const on = e.dataset.tab === name;
    e.classList.toggle('active', on);
    if (on) e.setAttribute('aria-current', 'page'); else e.removeAttribute('aria-current');
  });
  document.querySelectorAll('.tab-panel').forEach(e => e.style.display = 'none');
  const panel = _('tab-' + name);
  if (panel) {
    panel.style.display = 'block';
    panel.classList.remove('enter');
    void panel.offsetWidth; // 强制重排以重放入场动画
    panel.classList.add('enter');
  }
  try { history.replaceState(null, '', '#' + name); } catch(e){}
  setActiveTab(name);
  if (name !== 'dashboard' && typeof stopDashPolling === 'function') stopDashPolling();
  if (name !== 'logs' && typeof stopLogPolling === 'function') stopLogPolling();
  if (name === 'dashboard') { applyLang(); loadStats(); loadAccounts(); loadDashboardOverview(); startDashPolling(); }
  else if (name === 'accounts') { loadAccounts(); loadConnections(); }
  if (fromImport) toggleLoginImport(true);
  else if (name === 'logs') { loadRequestLogs(true); startLogPolling(); }
  else if (name === 'routing') { loadModels().then(() => { loadConfig(); }); }
  else if (name === 'combos') { loadCombos(); }
  else if (name === 'upstreams') { loadOcConfig(); loadClineProxyConfig(); loadZenHeaders(); }
  else if (name === 'providers') { loadProviders(); loadConnections(); }
  else if (name === 'security') { loadKeys(); loadConfig(); }
  else if (name === 'quota') { loadQuota(true); }
  else if (name === 'about') { if (typeof checkForUpdate === 'function' && !_verCheckDone) checkForUpdate(false); }
  refreshSaveBar();
}

function switchTab(name) { applyTab(name); }

// 顶栏一键刷新：重新加载当前页数据（对齐 CLIProxyAPI 面板的 header refresh）
function refreshAll(btn) {
  const el = document.querySelector('.nav-item.active');
  if (!el || !el.dataset.tab) return;
  if (btn) {
    btn.classList.add('spinning');
    setTimeout(() => btn.classList.remove('spinning'), 800);
  }
  applyTab(el.dataset.tab);
}

document.querySelectorAll('.nav-item').forEach(el => {
  el.addEventListener('click', () => {
    if (el.classList.contains('active')) return;
    applyTab(el.dataset.tab);
  });
});

// 从 URL/SessionStorage 恢复上次打开的标签页（统一在底部初始化时执行）

// ========== 侧栏排序（拖拽） ==========
// 排序持久化按布局版本隔离：body.html 侧栏重新分组后 bump NAV_LAYOUT_VERSION，
// 旧版本（含最早的裸数组格式）保存的顺序直接作废——否则旧顺序会在新分组上
// 逐条重演，把重排效果抵消掉（表现为「改了布局但侧栏看起来没变」）。
const NAV_ORDER_KEY = 'cline_admin_nav_order';
const NAV_LAYOUT_VERSION = '3';
function applyNavOrder() {
  let saved = null;
  try { saved = JSON.parse(localStorage.getItem(NAV_ORDER_KEY) || 'null'); } catch(e) {}
  if (!saved || saved.v !== NAV_LAYOUT_VERSION || !Array.isArray(saved.order) || !saved.order.length) {
    try { localStorage.removeItem(NAV_ORDER_KEY); } catch(e) {} // 清掉旧版本/裸数组遗留
    return;
  }
  const order = saved.order;
  const nav = document.querySelector('.nav-section').parentElement;
  const items = {};
  nav.querySelectorAll('.nav-section .nav-item').forEach(el => { items[el.dataset.tab] = el; });
  // 重排：按保存的顺序把条目移回其分区末尾，未知 tab 忽略（固定项不参与排序）
  order.forEach(tab => {
    const el = items[tab];
    if (!el) return;
    const sec = el.closest('.nav-section');
    if (!sec) return;
    sec.appendChild(el);
  });
}
function saveNavOrder() {
  const order = Array.from(document.querySelectorAll('.nav-section .nav-item')).map(el => el.dataset.tab);
  try { localStorage.setItem(NAV_ORDER_KEY, JSON.stringify({ v: NAV_LAYOUT_VERSION, order: order })); } catch(e) {}
}
function initNavDrag() {
  let dragged = null;
  // 固定项（.nav-pinned，如「快速开始」）不参与拖拽排序
  document.querySelectorAll('.nav-section .nav-item').forEach(el => {
    el.draggable = true;
    el.addEventListener('dragstart', e => {
      if (document.documentElement.dataset.nav === 'collapsed') { e.preventDefault(); return; } // 轨道图标态拖拽目标靠猜，禁用
      dragged = el; el.classList.add('dragging'); try { e.dataTransfer.setData('text/plain', el.dataset.tab); } catch(err){} e.dataTransfer.effectAllowed = 'move';
    });
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
function onPageSubTabShown(page, sub) {}
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

// ========== 主题（浅色 / 深色） ==========
const THEME_KEY = 'cline_admin_theme';
function applyTheme(theme) {
  document.documentElement.dataset.theme = theme;
  document.querySelectorAll('#themeSwitch button').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.themeOpt === theme);
  });
}
function setTheme(theme) {
  if (theme !== 'light' && theme !== 'dark') return;
  try { localStorage.setItem(THEME_KEY, theme); } catch (e) {}
  applyTheme(theme);
}
applyTheme(document.documentElement.dataset.theme || 'light');

// ========== 抽屉（右侧滑出编辑器） ==========
const _openDrawers = [];
function openDrawer(name) {
  const ov = _(name + 'Overlay'), dr = _(name);
  if (!dr) return;
  if (ov) ov.classList.add('open');
  dr.classList.add('open');
  if (!_openDrawers.includes(name)) _openDrawers.push(name);
  refreshSaveBar();
}
const _drawerGuards = {};
function setDrawerGuard(name, fn) { if (fn) _drawerGuards[name] = fn; else delete _drawerGuards[name]; }
async function closeDrawer(name) {
  const g = _drawerGuards[name];
  if (g) {
    let ok = true;
    try { ok = await g(); } catch (e) { ok = true; }
    if (!ok) return;
  }
  const ov = _(name + 'Overlay'), dr = _(name);
  if (ov) ov.classList.remove('open');
  if (dr) dr.classList.remove('open');
  const i = _openDrawers.indexOf(name);
  if (i >= 0) _openDrawers.splice(i, 1);
  refreshSaveBar();
}
// ESC closes the topmost drawer, but only when no modal layer is open on top
// of it (modal Esc handling lives in 06_models and runs on the same event).
const _ESC_MODAL_IDS = ['confirmModal','diffModal','modelMetaEditor','provPickerModal','comboModelPicker','presetPickerModal','importPreviewModal','connLoginModal','connImportModal','comboTestModal','modelSyncOverlay','logDetailModal'];
function _modalLayerOpen() {
  return _ESC_MODAL_IDS.some(id => { const el = _(id); return el && el.style.display === 'flex'; });
}
document.addEventListener('keydown', e => {
  if (e.key === 'Escape' && _openDrawers.length && !_modalLayerOpen()) closeDrawer(_openDrawers[_openDrawers.length - 1]);
});

// ========== 未保存更改：悬浮保存条 + 变更预览弹窗 ==========
var _verCheckDone = false; // 「关于」页更新检查只自动跑一次（checkForUpdate 在 03_settings 定义）
// 每个面（provider 抽屉 / 路由页调优卡）注册一个 ctx；同一时刻只展示优先级最高且脏的那个。
const _saveBarCtxs = {};
const _SAVE_BAR_ORDER = ['provider', 'tuning'];
function setSaveBarCtx(surface, ctx) {
  if (ctx) _saveBarCtxs[surface] = ctx; else delete _saveBarCtxs[surface];
  refreshSaveBar();
}
function _currentSaveBarCtx() {
  if (_modalLayerOpen()) return null; // 弹窗（含变更预览/确认）盖住时收起保存条
  for (const k of _SAVE_BAR_ORDER) {
    const c = _saveBarCtxs[k];
    if (c && c.host() && c.dirty()) return c;
  }
  return null;
}
function refreshSaveBar() {
  const bar = _('saveDiscardBar');
  if (!bar) return;
  const ctx = _currentSaveBarCtx();
  bar.classList.toggle('show', !!ctx);
  const diffBtn = _('saveBarDiff');
  if (diffBtn) diffBtn.hidden = !(ctx && typeof ctx.diff === 'function');
  // 有未保存更改时，关闭/刷新页面前提示（浏览器统一展示原生确认文案）
  if (ctx) window.onbeforeunload = e => { e.preventDefault(); return ''; };
  else window.onbeforeunload = null;
}
function discardSaveBar() { const c = _currentSaveBarCtx(); if (c) c.discard(); }
function saveViaBar() { const c = _currentSaveBarCtx(); if (c) c.save(); }
async function diffViaBar() {
  const c = _currentSaveBarCtx();
  if (!c || typeof c.diff !== 'function') return;
  let changes = [];
  try { changes = (await c.diff()) || []; } catch (e) { toast(t('无法生成差异预览'), 'error'); return; }
  showDiffModal(changes);
}
// 表单任意输入都会冒泡到这里，统一做脏检查（事件委托，兼容动态生成的行）
document.addEventListener('input', refreshSaveBar, true);
document.addEventListener('change', refreshSaveBar, true);

function fmtDiffVal(v) {
  if (v === undefined || v === null) return '';
  if (Array.isArray(v)) v = v.map(x => (x && typeof x === 'object') ? JSON.stringify(x) : String(x)).join(', ');
  else if (typeof v === 'object') v = JSON.stringify(v);
  else v = String(v);
  if (v.length > 90) v = v.slice(0, 90) + '…';
  return v;
}
// 两个同构浅对象（值可为标量/数组）→ [{label, from, to}]；值应已经 fmtDiffVal 字符串化
function diffFlat(base, cur) {
  const out = [];
  const keys = Object.keys(base);
  Object.keys(cur).forEach(k => { if (keys.indexOf(k) === -1) keys.push(k); });
  for (const k of keys) {
    const a = base[k] === undefined ? '' : base[k];
    const b = cur[k] === undefined ? '' : cur[k];
    if (String(a) === String(b)) continue;
    out.push({ label: k, from: String(a), to: String(b) });
  }
  return out;
}
const SENSITIVE_LABEL_RE = /(key|token|secret|password|authorization)/i;
let _diffResolve = null;
function showDiffModal(changes) {
  return new Promise(resolve => {
    const list = _('diffList');
    if (list) list.textContent = '';
    (changes || []).forEach(ch => {
      const sens = SENSITIVE_LABEL_RE.test(ch.label);
      const row = document.createElement('div'); row.className = 'diff-row';
      const lab = document.createElement('span'); lab.className = 'diff-label'; lab.textContent = ch.label;
      const from = document.createElement('span'); from.className = 'diff-from';
      const to = document.createElement('span'); to.className = 'diff-to';
      if (sens) {
        from.textContent = ch.from ? '••••' : '';
        to.textContent = ch.to ? '••••' : '';
        if (ch.from || ch.to) from.title = to.title = t('敏感值已隐藏');
      } else {
        from.textContent = ch.from;
        to.textContent = ch.to;
      }
      const arrow = document.createElement('span'); arrow.className = 'diff-arrow'; arrow.textContent = '→';
      row.appendChild(lab); row.appendChild(from); row.appendChild(arrow); row.appendChild(to);
      if (list) list.appendChild(row);
    });
    const m = _('diffModal');
    if (m) m.style.display = 'flex';
    _diffResolve = resolve;
  });
}
function resolveDiff(val) {
  const m = _('diffModal');
  if (m) m.style.display = 'none';
  if (_diffResolve) { const r = _diffResolve; _diffResolve = null; r(!!val); }
}

// ========== 侧栏收起 ==========
const NAV_COLLAPSE_KEY = 'cline_admin_nav';
// 轨道提示单例元素引用：必须在 syncNavCollapseBtn() 首次顶层调用之前初始化，
// 否则 _navTipHide 读取 TDZ 中的 let 会抛错并中断后续脚本（悬停监听器将不会注册）。
let _navRailTipEl = null;
function syncNavCollapseBtn() {
  const collapsed = document.documentElement.dataset.nav === 'collapsed';
  const label = collapsed ? t('展开侧栏') : t('收起侧栏');
  // 头部按钮（仅窄屏可见）与边缘悬浮钮（仅桌面可见）共用文案；图标翻转由 CSS 完成，
  // 不再重写 innerHTML
  document.querySelectorAll('.nav-collapse-btn,.nav-edge-toggle').forEach(btn => {
    btn.title = label; btn.setAttribute('aria-label', label);
  });
  _navTipHide(); // 状态切换后旧提示文案即失效
}
function toggleNav() {
  const collapsed = document.documentElement.dataset.nav === 'collapsed';
  if (collapsed) delete document.documentElement.dataset.nav;
  else document.documentElement.dataset.nav = 'collapsed';
  try { localStorage.setItem(NAV_COLLAPSE_KEY, collapsed ? 'expanded' : 'collapsed'); } catch (e) {}
  syncNavCollapseBtn();
}
syncNavCollapseBtn();

// ========== 收起态轨道提示（控制台 nav-tooltip 等价物） ==========
// 单例固定元素；仅 data-nav=collapsed 且宽屏时显示，文案取 .nav-label 文本节点
// （applyLang 已保证语言正确），因此无需额外的提示词条映射。
function _navTipHide() { if (_navRailTipEl) _navRailTipEl.classList.remove('show'); }
function _navTipShow(text, centerY) {
  if (window.innerWidth <= 760) return;
  if (!_navRailTipEl) {
    _navRailTipEl = document.createElement('div');
    _navRailTipEl.className = 'nav-rail-tip';
    document.body.appendChild(_navRailTipEl);
  }
  const tip = _navRailTipEl;
  tip.textContent = text;
  tip.style.display = 'block';
  const h = tip.getBoundingClientRect().height;
  tip.style.left = '72px';
  tip.style.top = Math.max(6, Math.min(centerY - h / 2, window.innerHeight - h - 6)) + 'px';
  tip.classList.add('show');
  tip.style.display = '';
}
(function initNavRailTip() {
  const nav = document.querySelector('.sidebar-nav');
  if (!nav) return;
  const collapsed = () => document.documentElement.dataset.nav === 'collapsed';
  const itemOf = e => {
    const it = e.target && e.target.closest ? e.target.closest('.nav-item') : null;
    return it && nav.contains(it) ? it : null;
  };
  const showForItem = e => {
    if (!collapsed()) return;
    const it = itemOf(e);
    if (!it) return;
    const lab = it.querySelector('.nav-label');
    if (!lab || !lab.textContent) return;
    const r = it.getBoundingClientRect();
    _navTipShow(lab.textContent, r.top + r.height / 2);
  };
  nav.addEventListener('mouseover', showForItem);
  nav.addEventListener('mouseout', e => { if (itemOf(e)) _navTipHide(); });
  nav.addEventListener('click', _navTipHide);
  nav.addEventListener('scroll', _navTipHide);
  const edge = document.querySelector('.nav-edge-toggle');
  if (edge) {
    const show = () => {
      const label = document.documentElement.dataset.nav === 'collapsed' ? t('展开侧栏') : t('收起侧栏');
      _navTipShow(label + ' · Ctrl+B', 56 + 12);
    };
    edge.addEventListener('mouseenter', show);
    edge.addEventListener('mouseleave', _navTipHide);
    edge.addEventListener('focus', show);
    edge.addEventListener('blur', _navTipHide);
  }
})();

// Ctrl/Cmd+B 收起/展开侧栏（输入控件聚焦时不拦截）
document.addEventListener('keydown', e => {
  if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && (e.key === 'b' || e.key === 'B')) {
    const evTarget = e.target;
    if (evTarget && (evTarget.tagName === 'INPUT' || evTarget.tagName === 'TEXTAREA' || evTarget.isContentEditable)) return;
    e.preventDefault();
    toggleNav();
  }
});

