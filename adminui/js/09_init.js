// ========== 初始化（必须保持为最后一个 JS 文件：仪表盘/日志等模块均已在此前定义） ==========
applyLang();
// 恢复上次打开的标签页（URL hash 优先，其次 sessionStorage），并加载该页数据
let bootTab = getActiveTab();
// 全新浏览器首次进入落到「快速开始」（对齐 CLIProxyAPI 控制台的 pinned onboarding 页）
try {
  let qsSeen = false;
  try { qsSeen = localStorage.getItem('cline_admin_quickstart_seen') === '1'; } catch (e) {}
  if (!qsSeen && !location.hash && bootTab === 'dashboard' && !sessionStorage.getItem(ACTIVE_TAB_KEY)) bootTab = 'quickstart';
  localStorage.setItem('cline_admin_quickstart_seen', '1');
} catch (e) {}
applyTab(bootTab);
// 与标签页无关的全局预热：密钥列表、模型列表 + 配置
loadKeys();
loadModels().then(() => loadConfig());
setInterval(() => { loadStats(); }, 10000);
// 手动改 hash / 浏览器前进后退时同步切换标签页
window.addEventListener('hashchange', () => applyTab(getActiveTab()));
