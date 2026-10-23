// ========== 初始化（必须保持为最后一个 JS 文件：仪表盘/日志等模块均已在此前定义） ==========
applyLang();
// 恢复上次打开的标签页（URL hash 优先，其次 sessionStorage），并加载该页数据
const bootTab = getActiveTab();
applyTab(bootTab);
// 与标签页无关的全局预热：密钥列表、模型列表 + 配置
loadKeys();
loadModels().then(() => loadConfig());
setInterval(() => { loadStats(); }, 10000);
// 手动改 hash / 浏览器前进后退时同步切换标签页
window.addEventListener('hashchange', () => applyTab(getActiveTab()));
