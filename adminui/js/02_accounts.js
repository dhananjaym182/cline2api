// ========== 账号管理 ==========
// ========== 账号管理：分页 + 勾选 ==========
let _accPage = 1;
const _accPageSize = 20;
const _checkedAccounts = new Set();
let _lastAccountList = []; // loadAccounts 缓存：全选本页/分页器复用，避免重复请求
let _accSearch = ''; // 邮箱搜索词（不区分大小写）
let _accSort = { key: null, dir: 1 }; // 列排序：key 见 _accSortValue，dir 1/-1

function _accSortValue(a, key) {
  switch (key) {
    case 'email': return (a.email || '').toLowerCase();
    case 'status': return a.status || '';
    case 'usage': return a.usageCount || 0;
    case 'total': return a.totalTokens || 0;
    case 'cached': return a.cachedTokens || 0;
    case 'lastUsed': return a.lastUsed ? new Date(a.lastUsed).getTime() : 0;
    case 'createdAt': return a.createdAt ? new Date(a.createdAt).getTime() : 0;
  }
  return null;
}

// 点击表头排序：同列切换升/降，新列默认升序
function onAccSort(key) {
  if (_accSort.key === key) _accSort.dir = -_accSort.dir;
  else _accSort = { key: key, dir: 1 };
  _accPage = 1;
  loadAccounts();
}

// 按搜索词过滤 + 列排序后的完整列表（分页/全选/计数都基于它）
function filteredAccountList() {
  let list = _lastAccountList;
  if (_accSearch) {
    const q = _accSearch.toLowerCase();
    list = list.filter(a => (a.email || '').toLowerCase().includes(q));
  }
  if (_accSort.key) {
    const key = _accSort.key, dir = _accSort.dir;
    list = list.slice().sort((a, b) => {
      const va = _accSortValue(a, key), vb = _accSortValue(b, key);
      if (va == null || vb == null) return 0;
      return (va < vb ? -1 : va > vb ? 1 : 0) * dir;
    });
  }
  return list;
}

function onAccSearch(v) {
  _accSearch = v.trim();
  _accPage = 1;
  loadAccounts();
}

async function loadAccounts() {
  try {
    if (!_cachedModels.length) {
      try { const dm = await api('GET', '/models'); _cachedModels = dm.data.models || []; } catch (e) {}
    }
    const d = await api('GET', '/accounts');
    const list = d.data.accounts;
    _lastAccountList = list || [];
    if (window.renderAttention) renderAttention();
    const view = filteredAccountList(); // 搜索后的视图
    const tbody = _('accountTableBody');
    const cards = _('accountCards');
    const sc = _('accSearchCount');
    if (sc) sc.textContent = _accSearch ? (t('匹配') + ' ' + view.length + ' / ' + (_lastAccountList.length || 0)) : '';
    // 勾选状态跟随列表清理（已删除的账号从勾选中移除）
    if (list) {
      const ids = new Set(list.map(a => a.accountId));
      [..._checkedAccounts].forEach(id => { if (!ids.has(id)) _checkedAccounts.delete(id); });
    }
    if (!view || view.length === 0) {
      _accPage = 1;
      _('accountPager').style.display = 'none';
      updateSelCount();
      const emptyMsg = _accSearch
        ? (t('没有匹配的账号') + '："' + esc(_accSearch) + '"')
        : (t('还没有账号') + ' — <a href="#" onclick="showAccountLogin();return false" style="color:var(--accent);cursor:pointer;text-decoration:none">' + t('添加账号') + '</a> · ' + t('支持 OAuth 登录 / 手动 Token / 批量导入'));
      tbody.innerHTML = '<tr><td colspan="12" class="empty">' + emptyMsg + '</td></tr>';
      cards.innerHTML = listEmptyState(ICON_ACCOUNTS, emptyMsg);
      return;
    }
    // 分页：前端切片，页码越界时收敛
    const totalPages = Math.max(1, Math.ceil(view.length / _accPageSize));
    if (_accPage > totalPages) _accPage = totalPages;
    if (_accPage < 1) _accPage = 1;
    const start = (_accPage - 1) * _accPageSize;
    const pageList = view.slice(start, start + _accPageSize);
    updateSelCount();
    renderAccountPager(view.length);
    const sn = { active: t('活跃'), cooldown: t('冷却'), expired: t('已过期') };
    // id → catalog entry，用于按真实来源分组（表格子行与移动卡片共用）
    const cat = {};
    (_cachedModels || []).forEach(m => { if (!cat[m.id]) cat[m.id] = m; });
    // key 前缀决定组顺序：Cline 官方 → Zen → 各 Provider → 自定义/内置 → 其他
    const groupInfo = id => {
      const m = cat[id];
      if (!m) return { key: '4|other', label: t('其他') };
      if (m.source === 'remote') return { key: '0|remote', label: t('Cline 官方') };
      if (m.source === 'zen') return { key: '1|zen', label: 'Zen' };
      if (m.source === 'provider') return { key: '2|' + (m.provider || ''), label: 'Provider · ' + (m.provider || t('未命名')) };
      return { key: '3|custom', label: t('自定义/内置') };
    };
    // 模型统计子行（仅 free 模型 + 模型级冷却状态），按模型真实来源分组
    const modelStatsRow = a => {
      const stats = Object.values(a.modelStats || {}).sort((x, y) => y.totalTokens - x.totalTokens);
      const cools = a.modelCooldowns || {};
      const entries = stats.map(st => ({ id: st.modelId, stat: st }));
      Object.keys(cools).filter(m => cools[m] && !(a.modelStats || {})[m]).forEach(m => entries.push({ id: m, coolOnly: true }));
      const groupMap = new Map();
      entries.forEach(e => {
        const g = groupInfo(e.id);
        if (!groupMap.has(g.key)) groupMap.set(g.key, { label: g.label, items: [] });
        groupMap.get(g.key).items.push(e);
      });
      const statRow = st => {
        const cd = cools[st.modelId];
        const cdBadge = cd
          ? '<span class="status cooldown status-cooldown" title="' + t('模型冷却中') + ' · ' + formatCooldown(cd) + '"><span class="cd-icon">⏳</span><span class="cd-time">' + formatCooldown(cd) + '</span></span>'
          : '';
        return '<tr style="background:var(--surface2)">' +
          '<td style="padding-left:44px" class="mono">' + esc(st.modelId) + ' <span class="model-tag free" style="font-size:10px;padding:1px 6px">free</span>' + '</td>' +
          '<td>' + cdBadge + '</td>' +
          '<td>' + formatNumber(st.usageCount) + '</td>' +
          '<td>' + formatTokenCount(st.promptTokens) + '</td>' +
          '<td>' + formatTokenCount(st.completionTokens) + '</td>' +
          '<td>' + formatTokenCount(st.totalTokens) + '</td>' +
          '<td>' + formatTokenCount(st.cachedTokens) + '</td>' +
          '</tr>';
      };
      const coolOnlyRow = m =>
        '<tr style="background:var(--surface2)">' +
          '<td style="padding-left:44px" class="mono">' + esc(m) + '</td>' +
          '<td><span class="status cooldown status-cooldown" title="' + t('模型冷却中') + ' · ' + formatCooldown(cools[m]) + '"><span class="cd-icon">⏳</span><span class="cd-time">' + formatCooldown(cools[m]) + '</span></span></td>' +
          '<td colspan="5"></td>' +
        '</tr>';
      let body = '';
      [...groupMap.keys()].sort().forEach(k => {
        const grp = groupMap.get(k);
        body += '<tr style="background:var(--surface2)"><td colspan="7" style="padding:6px 32px 2px;color:var(--text3);font-size:11px;font-weight:600">' + esc(grp.label) + ' · ' + grp.items.length + '</td></tr>';
        grp.items.forEach(e => { body += e.stat ? statRow(e.stat) : coolOnlyRow(e.id); });
      });
      const totalCooling = Object.keys(cools).length;
      const title = '<tr style="background:var(--surface2)">' +
        '<td colspan="7" style="padding:8px 32px;color:var(--text2);font-size:12px;font-weight:600">' +
          t('按模型统计（仅免费模型，按来源分组）') + (totalCooling ? ' · <span style="color:var(--yellow)">⏳ ' + totalCooling + ' ' + t('模型冷却中') + '</span>' : '') +
        '</td></tr>';
      if (!body) {
        return title + '<tr style="background:var(--surface2)"><td colspan="7" style="padding:6px 32px;color:var(--text3);font-size:12px">' + t('暂无数据') + '</td></tr>';
      }
      return title + body;
    };
    tbody.innerHTML = pageList.map((a, i) => {
      const seq = start + i + 1;
      const statusBadge = acctStatusBadge(a, sn);
      // 始终显示模型统计展开按钮（无数据时子行提示暂无）
      const expander = '<button class="btn btn-sm btn-icon" onclick="toggleModelRow(\'' + a.accountId + '\', this)" title="' + t('展开') + '" aria-label="' + t('展开') + '">▸</button>';
      const selBox = '<input type="checkbox" ' + (_checkedAccounts.has(a.accountId) ? 'checked' : '') + ' onchange="onSelAccount(\'' + a.accountId + '\', this.checked)">';
      return '<tr>' +
        '<td style="text-align:center">' + selBox + '</td>' +
        '<td class="mono" style="font-size:12px;color:var(--text3)">' + seq + '</td>' +
        '<td class="mono" style="font-size:12px" title="' + esc(a.email) + '">' + esc(a.email) + '</td>' +
        '<td>' + statusBadge + '</td>' +
        '<td>' + formatNumber(a.usageCount) + '</td>' +
        '<td>' + formatTokenCount(a.promptTokens) + '</td>' +
        '<td>' + formatTokenCount(a.completionTokens) + '</td>' +
        '<td>' + formatTokenCount(a.totalTokens) + '</td>' +
        '<td>' + formatTokenCount(a.cachedTokens) + '</td>' +
        '<td>' + formatDateTime(a.lastUsed, true) + '</td>' +
        '<td>' + formatDateTime(a.createdAt, true) + '</td>' +
        '<td style="white-space:nowrap">' + expander +
          '<button class="btn btn-sm" onclick="testAccount(\'' + a.accountId + '\',this)" title="' + t('测试') + '" aria-label="' + t('测试') + '">⚡</button> ' +
          '<button class="btn btn-sm" onclick="resetAccount(\'' + a.accountId + '\')" title="' + t('重置') + '" aria-label="' + t('重置') + '">↻</button> ' +
          '<button class="btn btn-sm btn-danger" onclick="deleteAccount(\'' + a.accountId + '\', this)" title="' + t('删除') + '" aria-label="' + t('删除') + '">✕</button>' +
        '</td></tr>' +
        '<tr id="modelRow-' + a.accountId + '" style="display:none"><td colspan="12" style="padding:0">' +
          '<table class="model-subtable" style="width:100%">' + modelStatsRow(a) + '</table></td></tr>';
    }).join('');
    cards.innerHTML = pageList.map(a => {
      const lu = formatDateTime(a.lastUsed);
      const cardStatus = acctStatusBadge(a, sn);
      // 卡片内的模型统计（免费模型 + 冷却状态），按来源分组
      const stats = Object.values(a.modelStats || {}).sort((x, y) => y.totalTokens - x.totalTokens);
      const cools = a.modelCooldowns || {};
      const coolingCount = Object.keys(cools).length;
      const cardEntries = stats.map(st => ({ id: st.modelId, stat: st }));
      Object.keys(cools).filter(m => cools[m] && !(a.modelStats || {})[m]).forEach(m => cardEntries.push({ id: m, coolOnly: true }));
      const cardGroups = new Map();
      cardEntries.forEach(e => {
        const g = groupInfo(e.id);
        if (!cardGroups.has(g.key)) cardGroups.set(g.key, { label: g.label, items: [] });
        cardGroups.get(g.key).items.push(e);
      });
      let items = '';
      [...cardGroups.keys()].sort().forEach(k => {
        const grp = cardGroups.get(k);
        items += '<div style="font-size:10px;color:var(--text3);font-weight:600;padding:6px 0 2px">' + esc(grp.label) + '</div>';
        grp.items.forEach(e => {
          if (e.stat) {
            const st = e.stat;
            const cd = cools[st.modelId];
            items += '<div style="display:flex;justify-content:space-between;gap:8px;padding:5px 0 5px 8px;border-bottom:1px solid var(--border2);font-size:12px">' +
              '<span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + esc(st.modelId) +
                (cd ? ' <span style="color:var(--yellow)">⏳' + formatCooldown(cd) + '</span>' : '') + '</span>' +
              '<span style="white-space:nowrap">' + formatTokenCount(st.totalTokens) + t(' Tok · ') + formatNumber(st.usageCount) + t(' 次') + '</span></div>';
          } else {
            items += '<div style="display:flex;justify-content:space-between;gap:8px;padding:5px 0 5px 8px;border-bottom:1px solid var(--border2);font-size:12px">' +
              '<span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + esc(e.id) + '</span>' +
              '<span style="color:var(--yellow);white-space:nowrap">⏳' + formatCooldown(cools[e.id]) + '</span></div>';
          }
        });
      });
      if (!items) items = '<div style="font-size:12px;color:var(--text3);padding:4px 0">' + t('暂无数据') + '</div>';
      const modelHtml = '<div style="margin:10px 0 0;padding:10px;border-radius:8px;background:var(--surface);border:1px solid var(--border2)">' +
        '<div style="font-size:11px;font-weight:600;color:var(--text2);margin-bottom:4px">' + t('按模型统计（仅免费模型，按来源分组）') +
          (coolingCount ? ' · <span style="color:var(--yellow)">⏳ ' + coolingCount + '</span>' : '') + '</div>' + items + '</div>';
      return '<article class="account-card">' +
        '<div class="account-card-header"><span class="account-email">' + esc(a.email) + '</span>' +
        cardStatus + '</div>' +
        '<div class="account-metrics">' +
          '<div class="account-metric"><span class="account-metric-label">' + t('请求') + '</span><span class="account-metric-value">' + formatNumber(a.usageCount) + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">' + t('总 Token') + '</span><span class="account-metric-value">' + formatTokenCount(a.totalTokens) + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">' + t('缓存') + '</span><span class="account-metric-value">' + formatTokenCount(a.cachedTokens) + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">' + t('输入') + '</span><span class="account-metric-value">' + formatTokenCount(a.promptTokens) + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">' + t('输出') + '</span><span class="account-metric-value">' + formatTokenCount(a.completionTokens) + '</span></div>' +
        '</div>' + modelHtml +
        '<div class="account-card-footer"><span>' + t('最后使用：') + lu + '</span><span class="account-card-actions">' +
          '<button class="btn btn-sm" onclick="testAccount(\'' + a.accountId + '\',this)" title="' + t('测试') + '" aria-label="' + t('测试') + '">⚡</button>' +
          '<button class="btn btn-sm" onclick="resetAccount(\'' + a.accountId + '\')" title="' + t('重置') + '" aria-label="' + t('重置') + '">↻</button>' +
          '<button class="btn btn-sm btn-danger" onclick="deleteAccount(\'' + a.accountId + '\', this)" title="' + t('删除') + '" aria-label="' + t('删除') + '">✕</button>' +
        '</span></div></article>';
    }).join('');
  } catch (e) { toast(t('加载账号失败: ') + e.message, 'error'); }
}

// 分页器：页码按钮 + 总数（total 为缓存列表长度）
function renderAccountPager(total) {
  const pager = _('accountPager');
  if (!pager) return;
  const totalPages = Math.max(1, Math.ceil(total / _accPageSize));
  if (totalPages <= 1 && total <= _accPageSize) { pager.style.display = 'none'; return; }
  pager.style.display = 'flex';
  pager.innerHTML =
    '<span style="font-size:12px;color:var(--text2)">' + t('共') + ' ' + total + ' ' + t('个账号 · 每页') + ' ' + _accPageSize + '</span>' +
    '<div style="display:flex;gap:6px;align-items:center">' +
      '<button class="btn btn-sm" ' + (_accPage <= 1 ? 'disabled' : '') + ' onclick="accGotoPage(' + (_accPage - 1) + ')" title="' + t('上一页') + '" aria-label="' + t('上一页') + '">‹</button>' +
      '<span style="font-size:12px" class="mono">' + _accPage + ' / ' + totalPages + '</span>' +
      '<button class="btn btn-sm" ' + (_accPage >= totalPages ? 'disabled' : '') + ' onclick="accGotoPage(' + (_accPage + 1) + ')" title="' + t('下一页') + '" aria-label="' + t('下一页') + '">›</button>' +
    '</div>';
}

function accGotoPage(p) {
  _accPage = p;
  loadAccounts();
}

// 勾选单个账号（跨页保持）
function onSelAccount(id, checked) {
  if (checked) _checkedAccounts.add(id);
  else _checkedAccounts.delete(id);
  updateSelCount();
}

// 全选/全不选当前页：优先用缓存列表；缓存为空（刚删除/导出后尚未刷新）时
// 退回当前渲染行上的勾选框（从 onchange="onSelAccount('id',...)" 解析账号 ID）。
function toggleAllAccountSel(box) {
  const list = filteredAccountList();
  const totalPages = Math.max(1, Math.ceil(list.length / _accPageSize));
  const p = Math.min(_accPage, totalPages);
  const chunk = list.slice((p - 1) * _accPageSize, p * _accPageSize);
  if (chunk.length) {
    chunk.forEach(a => {
      if (box.checked) _checkedAccounts.add(a.accountId);
      else _checkedAccounts.delete(a.accountId);
    });
    updateSelCount();
    loadAccounts();
    return;
  }
  const boxes = document.querySelectorAll('#accountTableBody input[type="checkbox"][onchange*="onSelAccount"]');
  if (!boxes.length) { loadAccounts(); return; }
  boxes.forEach(cb => {
    const m = (cb.getAttribute('onchange') || '').match(/onSelAccount\('([^']+)'/);
    if (!m) return;
    cb.checked = box.checked;
    if (box.checked) _checkedAccounts.add(m[1]);
    else _checkedAccounts.delete(m[1]);
  });
  updateSelCount();
}

// 勾选计数徽标：同时根据当前页是否全部勾选来同步“全选”框，避免勾上后被强制重置
function updateSelCount() {
  const n = _checkedAccounts.size;
  const sel = _('selCount'), del = _('delCount'), reset = _('resetCount'), all = _('accSelAll');
  if (sel) sel.textContent = n ? ' (' + n + ')' : '';
  if (del) del.textContent = n ? ' (' + n + ')' : '';
  if (reset) reset.textContent = n ? ' (' + n + ')' : '';
  if (all) {
    const list = filteredAccountList();
    const totalPages = Math.max(1, Math.ceil(list.length / _accPageSize));
    const p = Math.min(_accPage, totalPages);
    const chunk = list.slice((p - 1) * _accPageSize, p * _accPageSize);
    all.checked = chunk.length > 0 && chunk.every(a => _checkedAccounts.has(a.accountId));
  }
}

// 导出勾选的账号（POST /accounts/export {ids}，未勾选时提示）
async function exportSelectedAccounts() {
  const ids = [..._checkedAccounts];
  if (!ids.length) { toast(t('请先勾选要导出的账号'), 'info'); return; }
  try {
    const res = await fetch(API + '/accounts/export', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids: ids })
    });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'cline-accounts-export.json';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    toast(t('已导出 ') + ids.length + t(' 个账号'), 'success');
  } catch (e) { toast(t('导出失败: ') + e.message, 'error'); }
}

// 删除勾选的账号（逐个调用现有删除接口，带确认）
// 重置勾选的账号（逐个调用现有重置接口，清除冷却/失败计数）
async function resetSelectedAccounts(btn) {
  const ids = [..._checkedAccounts];
  if (!ids.length) { toast(t('请先勾选要重置的账号'), 'info'); return; }
  if (!(await showConfirm(t('确认重置选中的 ') + ids.length + t(' 个账号？（清除冷却与失败计数）')))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('重置中...'); }
  let ok = 0, fail = 0;
  for (const id of ids) {
    try { await api('POST', '/accounts/reset', { accountId: id }); ok++; }
    catch (e) { fail++; }
  }
  _checkedAccounts.clear();
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  if (fail) toast(t('已重置 ') + ok + t(' 个，失败 ') + fail + t(' 个'), 'error');
  else toast(t('已重置 ') + ok + t(' 个账号'), 'success');
  await loadAccounts();
}

async function deleteSelectedAccounts(btn) {
  const ids = [..._checkedAccounts];
  if (!ids.length) { toast(t('请先勾选要删除的账号'), 'info'); return; }
  if (!(await showConfirm(t('确认删除选中的 ') + ids.length + t(' 个账号？此操作不可恢复')))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('删除中...'); }
  let ok = 0, fail = 0;
  for (const id of ids) {
    try { await api('POST', '/accounts/delete', { accountId: id }); ok++; }
    catch (e) { fail++; }
  }
  _checkedAccounts.clear();
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  if (fail) toast(t('已删除 ') + ok + t(' 个，失败 ') + fail + t(' 个失败'), 'error');
  else toast(t('已删除 ') + ok + t(' 个账号'), 'success');
  await loadAccounts();
  await loadStats();
}

// 统一空值占位（缺失/未知）
function emptyCell() { return '<span style="color:var(--text3)">-</span>'; }

// 统一空状态：图标 + 文案（与密钥页 empty-state 同款视觉）
function listEmptyState(icon, msg) {
  return '<div class="empty-state"><div class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">' + icon + '</svg></div>' + msg + '</div>';
}

// Shared account status badge (table rows and cards): cooldown shows remaining time
function acctStatusBadge(a, sn) {
  return a.status === 'cooldown' && a.cooldownUntil
    ? '<span class="status cooldown status-cooldown" title="' + t('冷却 · 剩余 ') + formatCooldown(a.cooldownUntil) + '"><span class="cd-icon">⏳</span><span class="cd-time">' + formatCooldown(a.cooldownUntil) + '</span></span>'
    : '<span class="status ' + a.status + '"><span class="status-dot ' + a.status + '"></span>' + (sn[a.status] || a.status) + '</span>';
}

// 列表空状态图标（复用各区块标题图标）
const ICON_ACCOUNTS = '<path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a5.5 5.5 0 0 1 0 7.75"/>';
const ICON_COMBOS = '<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>';
const ICON_MODELS = '<rect x="3" y="3" width="18" height="18" rx="2"/><line x1="9" y1="9" x2="15" y2="9"/><line x1="9" y1="15" x2="15" y2="15"/>';
const ICON_PROVIDERS = '<rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/>';
const ICON_LOGINS = '<path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/>';

// 统一日期时间格式化：默认单行；twoLine 仅账号表窄列使用（10% 固定宽 + nowrap，单行会溢出/折行）
function formatDateTime(v, twoLine) {
  if (!v) return emptyCell();
  const d = new Date(v);
  if (twoLine) {
    return '<div style="white-space:nowrap">' + d.toLocaleDateString(LC()) + '</div>' +
      '<div style="white-space:nowrap;font-size:10px;color:var(--text3)">' + d.toLocaleTimeString(LC()) + '</div>';
  }
  return d.toLocaleString(LC());
}

// 日志时间戳：两行块（date 在上，time 含秒在下）。不再走 toLocaleString 单行 + td.mono
// 的省略号裁剪（旧渲染在固定 14% 列里日期时间都被裁掉）——两行各自 nowrap，永不被截断。
function logTimestampHtml(v) {
  if (!v) return emptyCell();
  const d = new Date(v);
  if (isNaN(d.getTime())) return emptyCell();
  return '<span class="log-ts-wrap" title="' + escAttr(d.toLocaleString(LC())) + '">' +
    '<span class="log-ts-line">' + d.toLocaleDateString(LC()) + '</span>' +
    '<span class="log-ts-line log-ts-sub">' + d.toLocaleTimeString(LC()) + '</span>' +
  '</span>';
}

// 展开/收起账号的模型统计子行（表格视图）
function toggleModelRow(id, btn) {
  const row = _('modelRow-' + id);
  if (!row) return;
  const hidden = row.style.display === 'none';
  row.style.display = hidden ? '' : 'none';
  btn.innerHTML = hidden ? '▾' : '▸';
  btn.title = hidden ? t('收起') : t('展开');
}

async function testAccount(id, btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>'; }
  try {
    const d = await api('POST', '/accounts/test', { accountId: id });
    const r = (d.data.results || [])[0];
    if (r && r.ok) {
      const tok = r.inputTokens || r.outputTokens ? t(' · 输入 ') + formatTokenCount(r.inputTokens) + t(' · 输出 ') + formatTokenCount(r.outputTokens) : '';
      toast(t('测试成功：') + esc(r.email) + ' · ' + formatDuration(r.durationMs) + tok, 'success');
    } else {
      toast(t('测试失败：') + esc(r ? r.email : '?') + ' · ' + (r ? r.error : t('未知错误')), 'error');
    }
    loadAccounts(); loadStats();
  } catch (e) { toast(t('测试失败: ') + e.message, 'error'); }
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
}

async function testAllAccounts(btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('测试中...'); }
  toast(t('正在测试全部账号，请稍候...'), 'info');
  try {
    const d = await api('POST', '/accounts/test', {});
    const results = d.data.results || [];
    const ok = results.filter(r => r.ok).length;
    const fail = results.length - ok;
    if (fail === 0) {
      toast(t('全部测试通过：') + ok + '/' + results.length + t(' 个账号正常'), 'success');
    } else {
      const failed = results.filter(r => !r.ok).map(r => esc(r.email) + '(' + r.error + ')').join('，');
      toast(t('测试完成：') + ok + t(' 成功 / ') + fail + t(' 失败 · ') + failed, 'error');
    }
    loadAccounts(); loadStats();
  } catch (e) { toast(t('测试失败: ') + e.message, 'error'); }
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
}

async function deleteAccount(id, btn) {
  if (!(await showConfirm(t('确定删除此账号？')))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('删除中...'); }
  try {
    await api('POST', '/accounts/delete', { accountId: id });
    toast(t('账号已删除'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

async function resetAccount(id) {
  if (!(await showConfirm(t('确定重置此账号？将恢复为活跃状态并刷新 Token，保留历史统计。')))) return;
  try {
    await api('POST', '/accounts/reset', { accountId: id });
    toast(t('账号已重置'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(t('重置失败: ') + e.message, 'error'); }
}

async function deleteAllAccounts() {
  if (!(await showConfirm(t('⚠️ 确定删除所有账号？不可撤销！')))) return;
  try {
    await api('POST', '/accounts/delete-all', {});
    toast(t('全部账号已删除'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

async function refreshAllTokens(btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('刷新中...'); }
  try {
    await api('POST', '/accounts/refresh-all', {});
    toast(t('全部 Token 已刷新'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(t('刷新失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

// ========== OAuth 登录 ==========
async function startOAuth() {
  const btn = _('oauthBtn');
  btn.disabled = true;
  btn.innerHTML = '<span class="loading"></span> ' + t('启动中...');
  _('oauthProgress').style.display = 'block';
  _('oauthResult').style.display = 'none';
  _('oauthStatus').textContent = t('正在连接 WorkOS...');
  try {
    const d = await api('POST', '/oauth/start');
    const s = d.data;
    _('oauthStatus').textContent = t('请在浏览器中打开链接并输入代码');
    const u = _('oauthUrl');
    u.textContent = s.verificationUri;
    u.href = '#';
    u.onclick = async function(e) { e.preventDefault(); await openExternal(s.verificationUri); };
    _('oauthUserCode').textContent = s.userCode;
    let pollFails = 0;
    const poll = setInterval(async () => {
      try {
        const r = await api('GET', '/oauth/status?sessionId=' + s.sessionId);
        pollFails = 0;
        if (r.data.done) {
          clearInterval(poll);
          btn.disabled = false;
          btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>' + t('开始 OAuth 登录');
          if (r.data.success) {
            _('oauthProgress').style.display = 'none';
            _('oauthResult').innerHTML = '<div style="color:var(--green);font-weight:600">✓ ' + t('账号添加成功: ')+ esc(r.data.email) + '</div>';
            _('oauthResult').style.display = 'block';
            loadAccounts(); loadStats();
            toast(t('账号添加成功！'), 'success');
          } else {
            _('oauthStatus').textContent = t('失败: ') + (r.data.error || t('未知错误'));
            toast(t('OAuth 失败'), 'error');
          }
        }
      } catch(e) {
        // 连续 3 次轮询失败视为会话失效：停止轮询、恢复按钮并提示一次（不逐次 toast，避免刷屏）
        pollFails++;
        if (pollFails >= 3) {
          clearInterval(poll);
          btn.disabled = false;
          btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>' + t('开始 OAuth 登录');
          _('oauthStatus').textContent = t('错误: ') + e.message;
          toast(t('OAuth 失败: ') + e.message, 'error');
        }
      }
    }, 2000);
  } catch (e) {
    btn.disabled = false;
    btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>' + t('开始 OAuth 登录');
    _('oauthStatus').textContent = t('错误: ') + e.message;
    toast(t('OAuth 失败: ') + e.message, 'error');
  }
}

// ========== Token 导入 ==========
// 表单内联结果（成功/失败都渲染进 #tokenResult，不再只靠 toast）
function tokenFormResult(ok, html) {
  const el = _('tokenResult');
  if (el) el.innerHTML = '<div style="color:var(' + (ok ? '--green' : '--red') + ');font-size:13px;margin-top:8px">' + (ok ? '✓ ' : '✗ ') + html + '</div>';
}
// 提交后引导：跳到账号列表核对结果
function viewAccountsLink() {
  return '<a href="#" onclick="switchTab(\'accounts\')" style="color:var(--accent);text-decoration:none;margin-left:6px">' + t('查看账号列表') + ' →</a>';
}

async function addByToken(btn) {
  const token = _('tokenInput').value.trim();
  if (!token) { tokenFormResult(false, t('请输入 refreshToken')); toast(t('请输入 refreshToken'), 'error'); return; }
  const el = _('tokenResult'); if (el) el.innerHTML = ''; // 清空上次结果，避免旧成败信息与新提交混淆
  const email = _('tokenEmail').value.trim();
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('添加中...'); }
  try {
    const d = await api('POST', '/accounts/add', { refreshToken: token, email: email || undefined });
    tokenFormResult(true, t('账号添加成功: ') + esc(d.data.email || '') + viewAccountsLink());
    toast(t('账号添加成功: ') + (d.data.email || ''), 'success');
    _('tokenInput').value = '';
    _('tokenEmail').value = '';
    loadAccounts(); loadStats();
  } catch (e) {
    tokenFormResult(false, esc(e.message));
    toast(t('添加失败: ') + e.message, 'error');
  }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

// ========== 导出账号 ==========
async function exportAccounts(btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>'; }
  try {
    const res = await fetch(API + '/accounts/export');
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'cline-accounts-export.json';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    toast(t('账号已导出'), 'success');
  } catch (e) { toast(t('导出失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

// ========== 批量导入 ==========
// 解析导入数据：支持导出格式 {tokens:[...]}、JSON 数组 [...]、单行一个 token 的纯文本
// 解析后按 refreshToken 去重，避免同一账号被重复导入
function parseImportData(text) {
  let parsed;
  try {
    parsed = JSON.parse(text);
  } catch {
    // 纯文本：每行一个 refreshToken
    parsed = text.split('\n').filter(t => t.trim()).map(t => ({ refreshToken: t.trim() }));
  }
  let list;
  // 导出格式 {tokens:[...], exportedAt:...}
  if (parsed && !Array.isArray(parsed) && Array.isArray(parsed.tokens)) {
    list = parsed.tokens;
  // JSON 数组
  } else if (Array.isArray(parsed)) {
    list = parsed;
  // 单个对象
  } else {
    list = [parsed];
  }
  const seen = new Set();
  return list
    .map(item => (item && typeof item === 'object') ? item : { refreshToken: String(item || '').trim() })
    .filter(item => {
      const tok = (item.refreshToken || '').trim();
      if (!tok || seen.has(tok)) return false;
      seen.add(tok);
      return true;
    });
}

// 批量导入表单内联结果
function batchFormResult(ok, html) {
  const el = _('batchResult');
  if (el) el.innerHTML = '<div style="color:var(' + (ok ? '--green' : '--red') + ');font-size:13px;margin-top:8px">' + (ok ? '✓ ' : '✗ ') + html + '</div>';
}

async function batchImport(btn) {
  const raw = _('batchInput').value.trim();
  if (!raw) { batchFormResult(false, t('请输入账号数据')); toast(t('请输入账号数据'), 'error'); return; }
  const el = _('batchResult'); if (el) el.innerHTML = '';
  const tokens = parseImportData(raw);
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('导入中...'); }
  try {
    const d = await api('POST', '/batch-import', { tokens });
    batchFormResult(true, esc(d.message || t('导入完成')) + viewAccountsLink());
    toast(d.message || t('导入完成'), 'success');
    _('batchInput').value = '';
    loadAccounts(); loadStats();
  } catch (e) {
    batchFormResult(false, esc(e.message));
    toast(t('导入失败: ') + e.message, 'error');
  }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

async function handleFileImport(event) {
  const file = event.target.files[0];
  if (!file) return;
  const text = await file.text();
  const tokens = parseImportData(text);
  try {
    const d = await api('POST', '/batch-import', { tokens });
    const imported = (d.data && typeof d.data.imported === 'number') ? d.data.imported : tokens.length;
    toast(d.message || t('导入了 ') + imported + t(' 个账号'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(t('导入失败: ') + e.message, 'error'); }
  event.target.value = '';
}

