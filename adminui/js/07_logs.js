// ========== 请求日志 ==========
let logCursor = '';
let logHasMore = false;
let _logRowSeq = 0;
let _logItems = [];       // 已加载日志（新→旧），筛选/渲染都基于它
let _logIds = new Set();  // 已知 ID 集合，跟随轮询去重用
let _logFilter = 'all';   // all|ok|fail|stream|combo|fallback
let _logSearch = '';
let _logPending = [];     // 暂停跟随时缓冲的新日志
// 跟随新日志为客户端实时视图开关：默认开，用户手动停用后持久化（跨标签页/刷新保留）
const LOG_FOLLOW_KEY = 'cline_admin_log_follow';
let _logFollow = false;
try { _logFollow = localStorage.getItem(LOG_FOLLOW_KEY) !== '0'; } catch (e) { _logFollow = true; }
let _logPollTimer = 0;
let _logDetailIdx = -1;
let _logSearchT = 0;
let _logDetailMap = {};   // log id -> 展开行 DOM id（避免把 UI 状态写进日志对象）

const formatDuration = ms => {
  if (!ms || ms <= 0) return '-';
  if (ms < 1000) return ms + 'ms';
  return (ms / 1000).toFixed(1) + 's';
};
const formatTPS = v => (!v || v <= 0) ? '-' : v.toFixed(1);

// 尝试链路结果标签（成功/失败/跳过）
function attemptOutcomeLabel(o) {
  if (o === 'success') return t('成功');
  if (o === 'skipped') return t('跳过');
  return t('失败');
}

// 上游展示名：自定义 provider 上游时用后端回传的 providerName 显示真实名称，
// 旧日志条目（无 providerName）与 cline/opencode 上游回落为原通用文案。
function attemptUpstreamLabel(a) {
  return (a.upstream === 'provider' && a.providerName) ? 'Provider › ' + a.providerName : (a.upstream || '');
}

// 表格「账号」列 / 卡片标题：账号邮箱优先；无邮箱时展示上游（provider 上游带真实名称）
function logUpstreamValue(l) {
  if (l.accountEmail) return l.accountEmail;
  return attemptUpstreamLabel(l) || '-';
}

// 尝试链路渲染：序号 / 模型 / 上游 / 结果 / HTTP 状态 / 耗时 / 原因
function renderAttempts(attempts) {
  return '<ol class="log-attempts">' + attempts.map((a, i) => {
    const outcome = a.outcome || 'failed';
    const oc = (outcome === 'success' || outcome === 'failed' || outcome === 'skipped') ? outcome : 'failed';
    const meta = [];
    const upLabel = attemptUpstreamLabel(a);
    if (upLabel) meta.push(esc(upLabel));
    if (a.status) meta.push('HTTP ' + esc(String(a.status)));
    if (typeof a.durationMs === 'number' && a.durationMs >= 0) meta.push(formatDuration(a.durationMs));
    return '<li>' +
      '<span class="combo-entry-idx">' + (i + 1) + '</span>' +
      '<span class="attempt-model">' + esc(a.model || '-') + '</span>' +
      '<span class="attempt-outcome ' + oc + '">' + esc(attemptOutcomeLabel(outcome)) + '</span>' +
      (meta.length ? '<span class="attempt-meta">' + meta.join(' · ') + '</span>' : '') +
      (a.reason ? '<span class="attempt-reason">' + esc(a.reason) + '</span>' : '') +
    '</li>';
  }).join('') + '</ol>';
}

// 日志模型单元格：服务模型 + 组合徽章 + 回退计数（requestedModel 与服务模型不同时一并显示）
function logModelCell(l) {
  let html = '<span class="mono" style="font-size:11px">' + esc(l.model || '-') + '</span>';
  if (l.requestedModel && l.requestedModel !== l.model) {
    html += '<span class="log-req-model">' + t('请求: ') + esc(l.requestedModel) + '</span>';
  }
  if (l.comboId) {
    html += '<span class="log-combo-badge">' + t('组合') + ': ' + esc(l.comboName || l.comboId) + '</span>';
  }
  if (l.fallbackUsed) {
    const n = Number(l.fallbackCount) || 0;
    html += '<span class="log-fallback-badge">' + t('回退') + (n > 0 ? ' ' + n : '') + '</span>';
  }
  return '<div class="log-model-cell">' + html + '</div>';
}

// 表格行展开/收起尝试链路详情行
function toggleLogDetail(id) {
  const el = _(id);
  if (!el) return;
  el.style.display = (el.style.display === 'none' || !el.style.display) ? '' : 'none';
}

// ---------- 筛选 ----------
const LOG_FILTERS = [
  { key: 'all', label: '全部', fn: () => true },
  { key: 'ok', label: '成功', fn: l => l.completed && !l.error },
  { key: 'fail', label: '失败', fn: l => !l.completed || !!l.error },
  { key: 'stream', label: '流式', fn: l => !!l.stream },
  { key: 'combo', label: '组合', fn: l => !!l.comboId },
  { key: 'fallback', label: '回退', fn: l => !!l.fallbackUsed }
];

function logMatches(l) {
  const f = LOG_FILTERS.find(x => x.key === _logFilter) || LOG_FILTERS[0];
  if (!f.fn(l)) return false;
  const sel = _('logFilterProtocol');
  if (sel && sel.value && (l.protocol || '') !== sel.value) return false;
  if (_logSearch) {
    const q = _logSearch.toLowerCase();
    const hay = [l.model, l.requestedModel, l.accountEmail, l.connectionEmail, l.providerName, l.id,
      (l.comboName || ''), (l.error || '')].filter(Boolean).join(' ').toLowerCase();
    if (hay.indexOf(q) === -1) return false;
  }
  return true;
}

function updateLogChips() {
  const box = _('logFilterChips');
  if (!box) return;
  box.innerHTML = LOG_FILTERS.map(f => {
    const n = _logItems.filter(f.fn).length;
    return '<button type="button" class="chip log-chip' + (_logFilter === f.key ? ' active' : '') + '" data-filter="' + f.key + '" onclick="setLogFilter(\'' + f.key + '\')">' +
      t(f.label) + ' <b>' + n + '</b></button>';
  }).join('');
  // 协议下拉：用已加载数据补充选项，保留当前选择
  const sel = _('logFilterProtocol');
  if (sel) {
    const protos = [...new Set(_logItems.map(l => l.protocol).filter(Boolean))].sort();
    const cur = sel.value;
    sel.innerHTML = '<option value="">' + t('全部协议') + '</option>' +
      protos.map(p => '<option value="' + escAttr(p) + '"' + (p === cur ? ' selected' : '') + '>' + esc(p) + '</option>').join('');
  }
}

function setLogFilter(key) {
  _logFilter = key;
  updateLogChips();
  renderLogList();
}

function onLogProtoFilterChange() { renderLogList(); }

function onLogSearchInput() {
  clearTimeout(_logSearchT);
  _logSearchT = setTimeout(() => {
    _logSearch = (_('logSearch').value || '').trim();
    renderLogList();
  }, 200);
}

// ---------- 渲染 ----------
function renderLogList() {
  const tbody = _('logTableBody');
  const cards = _('logCards');
  if (!tbody || !cards) return;
  const shown = _logItems.filter(logMatches);
  if (!shown.length) {
    const msg = _logItems.length ? t('暂无匹配日志') : t('暂无请求日志');
    tbody.innerHTML = '<tr><td colspan="13" class="empty">' + msg + '</td></tr>';
    cards.innerHTML = _logItems.length ? '' : '<div class="empty">' + t('暂无请求日志') + '</div>';
    _('logLoadMore').style.display = (_logItems.length && logHasMore) ? 'block' : 'none';
    return;
  }

  const renderRow = (l, idx) => {
    const st = l.completed
      ? '<span class="log-status ok">' + t('完成') + '</span>'
      : '<span class="log-status fail" title="' + escAttr(l.error || '') + '">' + t('失败') + '</span>';
    const tk = l.usageAvailable
      ? formatTokenCount(l.inputTokens) + '</td><td>' + formatTokenCount(l.outputTokens) + '</td><td>' + formatTokenCount(l.cachedTokens) + '</td><td>' + formatTokenCount(l.totalTokens)
      : '-</td><td>-</td><td>-</td><td>-';
    const attempts = Array.isArray(l.attempts) ? l.attempts : [];
    const hasAttempts = attempts.length > 0;
    const detailId = 'logDetail' + (_logRowSeq++);
    _logDetailMap[l.id] = detailId;
    const rowCls = 'log-row' + (hasAttempts ? ' has-attempts' : '');
    const rowTitle = hasAttempts ? ' title="' + escAttr(t('点击查看尝试链路')) + ' · ' + t('双击复制 JSON') + '"' : ' title="' + escAttr(t('双击复制 JSON')) + '"';
    return '<tr class="' + rowCls + '" data-idx="' + idx + '"' + rowTitle + '>' +
      '<td>' + logTimestampHtml(l.startedAt) + '</td>' +
      '<td>' + esc(logUpstreamValue(l)) + '</td>' +
      '<td>' + esc(l.protocol || '-') + '</td>' +
      '<td>' + logModelCell(l) + '</td>' +
      '<td>' + tk + '</td>' +
      '<td>' + formatDuration(l.durationMs) + '</td>' +
      '<td>' + (l.ttftMs ? formatDuration(l.ttftMs) : '-') + '</td>' +
      '<td>' + formatTPS(l.outputTokensPerSecond) + '</td>' +
      '<td>' + st + '</td>' +
      '<td><button type="button" class="btn btn-sm log-detail-btn" data-idx="' + idx + '">' + t('详情') + '</button></td></tr>' +
      (hasAttempts ? '<tr id="' + detailId + '" class="log-detail" style="display:none"><td colspan="13" class="log-detail-cell">' + renderAttempts(attempts) + '</td></tr>' : '');
  };

  const renderCard = (l, idx) => {
    const st = l.completed ? t('完成') : t('失败');
    const tk = l.usageAvailable
      ? t('输入 ') + formatTokenCount(l.inputTokens) + t(' · 输出 ') + formatTokenCount(l.outputTokens) + t(' · 缓存 ') + formatTokenCount(l.cachedTokens) + t(' · 总 ') + formatTokenCount(l.totalTokens)
      : t('Token 未知');
    const attempts = Array.isArray(l.attempts) ? l.attempts : [];
    let extra = '';
    if (l.comboId) extra += '<span class="log-combo-badge">' + t('组合') + ': ' + esc(l.comboName || l.comboId) + '</span>';
    if (l.fallbackUsed) {
      const n = Number(l.fallbackCount) || 0;
      extra += '<span class="log-fallback-badge">' + t('回退') + (n > 0 ? ' ' + n : '') + '</span>';
    }
    const extraBlock = extra ? '<div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-bottom:6px">' + extra + '</div>' : '';
    const reqLine = (l.requestedModel && l.requestedModel !== l.model)
      ? '<div class="log-req-model" style="margin-bottom:6px">' + t('请求: ') + esc(l.requestedModel) + '</div>' : '';
    const attemptsBlock = attempts.length
      ? '<details class="log-card-attempts"><summary>' + t('尝试链路') + ' (' + attempts.length + ')</summary>' + renderAttempts(attempts) + '</details>'
      : '';
    return '<article class="account-card log-card" data-idx="' + idx + '">' +
      '<div class="account-card-header"><span class="account-email">' + esc(logUpstreamValue(l)) + '</span><span class="log-status ' + (l.completed ? 'ok' : 'fail') + '">' + st + '</span></div>' +
      '<div class="account-metrics">' +
        '<div class="account-metric"><span class="account-metric-label">' + t('协议') + '</span><span class="account-metric-value">' + esc(l.protocol || '-') + '</span></div>' +
        '<div class="account-metric"><span class="account-metric-label">' + t('耗时') + '</span><span class="account-metric-value">' + formatDuration(l.durationMs) + '</span></div>' +
        '<div class="account-metric"><span class="account-metric-label">TTFT</span><span class="account-metric-value">' + (l.ttftMs ? formatDuration(l.ttftMs) : '-') + '</span></div>' +
        '<div class="account-metric"><span class="account-metric-label">tok/s</span><span class="account-metric-value">' + formatTPS(l.outputTokensPerSecond) + '</span></div>' +
      '</div>' +
      extraBlock + reqLine +
      '<div style="font-size:12px;color:var(--text2);margin-bottom:8px">' + tk + '</div>' +
      '<div class="account-card-footer">' + logTimestampHtml(l.startedAt) + '<span class="mono" style="font-size:11px">' + esc(l.model || '-') + '</span></div>' +
      attemptsBlock +
      '<div style="margin-top:8px"><button type="button" class="btn btn-sm log-detail-btn" data-idx="' + idx + '">' + t('详情') + '</button></div>' +
    '</article>';
  };

  const pairs = [];
  for (let i = 0; i < _logItems.length; i++) { if (logMatches(_logItems[i])) pairs.push([_logItems[i], i]); }
  tbody.innerHTML = pairs.map(p => renderRow(p[0], p[1])).join('');
  cards.innerHTML = pairs.map(p => renderCard(p[0], p[1])).join('');
  _('logLoadMore').style.display = logHasMore ? 'block' : 'none';
}

function ingestLogItems(items, reset) {
  if (reset) {
    _logItems = items.slice();
    _logIds = new Set(_logItems.map(l => l.id));
    _logRowSeq = 0;
  } else {
    items.forEach(l => { if (!_logIds.has(l.id)) { _logItems.push(l); _logIds.add(l.id); } });
  }
}

async function loadRequestLogs(reset) {
  if (reset) { logCursor = ''; _logRowSeq = 0; _logPending = []; hideLogResumeBar(); }
  const cursor = logCursor;
  try {
    const path = '/request-logs?limit=50' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '');
    const d = await api('GET', path);
    const page = d.data;
    const items = page.items || [];
    logHasMore = !!page.hasMore;
    logCursor = page.nextCursor || '';
    ingestLogItems(items, reset);
    updateLogChips();
    renderLogList();
  } catch (e) { toast(t('加载日志失败: ') + e.message, 'error'); }
}

// ---------- 跟随新日志 ----------
// 「实时端」必须相对最新一行判定，而不是窗口滚动顶部：列表上方还有页头与
// 工具栏（约 200–300px），正常观看位置 scrollTop 早已超过固定阈值，会把正常
// 观看误判成翻阅历史——resume 条常驻、点击后又跳回页顶（列表反而离开视野），
// 看起来就是「点了没反应 / 一直显示」。
function newestLogEl() {
  const row = document.querySelector('#logTableBody tr.log-row');
  if (row && row.getClientRects().length) return row;
  const card = document.querySelector('#logCards .log-card');
  if (card && card.getClientRects().length) return card;
  return row || card || null;
}
function logAtLiveEdge() {
  const se = document.scrollingElement || document.documentElement;
  const first = newestLogEl();
  if (!first) return se.scrollTop <= 140;
  // 最新行的文档内绝对位置（含页头/工具栏/resume 条自身高度的实时影响）；
  // 只要还没向下翻过它 240px 以上，就仍在实时端。
  const rowTopDoc = first.getBoundingClientRect().top + se.scrollTop;
  return se.scrollTop <= rowTopDoc + 240;
}

function hideLogResumeBar() {
  const bar = _('logResumeBar');
  if (bar) bar.hidden = true;
}

function startLogPolling() {
  refreshLogFollowBtn();
  if (_logPollTimer) return;
  _logPollTimer = setInterval(logPollTick, 8000);
}

function stopLogPolling() {
  if (_logPollTimer) { clearInterval(_logPollTimer); _logPollTimer = 0; }
}

// 跟随按钮视觉与 _logFollow 持久态对齐（进入日志页时刷新，默认开/手动停后保持）
function refreshLogFollowBtn() {
  const btn = _('logFollowBtn');
  if (!btn) return;
  btn.classList.toggle('btn-primary', _logFollow);
  btn.textContent = _logFollow ? t('跟随新日志') : t('已暂停跟随');
  btn.title = _logFollow ? t('自动跟随新日志') : t('点击恢复自动跟随');
}

// 恢复跟随 = 把最新一行带回视口顶部（不是跳页面顶）：回到实时端后下一次轮询
// 才会自动置顶新日志，而不是再次弹出 resume 条
function scrollLogToLiveEdge() {
  const first = newestLogEl();
  if (first && first.scrollIntoView) { first.scrollIntoView({ block: 'start' }); return; }
  window.scrollTo(0, 0);
}

async function logPollTick() {
  if (document.visibilityState !== 'visible' || !_logFollow) return;
  const el = document.querySelector('.nav-item.active');
  if (!el || el.dataset.tab !== 'logs') return;
  try {
    const d = await api('GET', '/request-logs?limit=50');
    const fresh = (d.data.items || []).filter(l => l.id && !_logIds.has(l.id));
    if (!fresh.length) return;
    if (!logAtLiveEdge()) {
      // 用户向下翻阅历史：新日志缓冲不打断，显示"恢复跟随 (N 新)"
      fresh.forEach(l => { if (!_logPending.some(p => p.id === l.id)) _logPending.unshift(l); });
      _logPending.sort((a, b) => new Date(b.startedAt) - new Date(a.startedAt));
      const bar = _('logResumeBar'), txt = _('logResumeText');
      if (bar) bar.hidden = false;
      if (txt) txt.textContent = t('已暂停跟随 · ') + _logPending.length + t(' 条新日志');
      return;
    }
    fresh.forEach(l => { _logItems.unshift(l); _logIds.add(l.id); });
    updateLogChips();
    renderLogList();
  } catch (e) { /* 静默：轮询失败不打扰 */ }
}

function resumeLogFollow() {
  if (_logPending.length) {
    _logPending.forEach(l => { if (!_logIds.has(l.id)) { _logItems.unshift(l); _logIds.add(l.id); } });
    _logPending = [];
  }
  hideLogResumeBar();
  updateLogChips();
  renderLogList();
  scrollLogToLiveEdge(); // 回到顶部实时端，避免下一次轮询再次弹出 resume 条
}

function toggleLogFollow(btn) {
  _logFollow = !_logFollow;
  try { localStorage.setItem(LOG_FOLLOW_KEY, _logFollow ? '1' : '0'); } catch (e) {}
  refreshLogFollowBtn();
  if (_logFollow && _logPending.length) resumeLogFollow();
}

// ---------- 复制 / 详情弹窗 ----------
function copyLogJson(idx) {
  const l = _logItems[idx];
  if (!l) return;
  copyText(JSON.stringify(l, null, 2));
}

function openLogDetail(idx) {
  const l = _logItems[idx];
  if (!l) return;
  _logDetailIdx = idx;
  const modal = _('logDetailModal');
  if (!modal) return;
  _('logDetailId').textContent = l.id || '';
  const bits = [];
  if (l.startedAt) bits.push(new Date(l.startedAt).toLocaleString());
  if (l.protocol) bits.push(l.protocol);
  if (l.stream) bits.push(t('流式'));
  if (l.model) bits.push(l.model);
  _('logDetailSub').textContent = bits.join(' · ');
  const rows = [
    [t('账号'), logUpstreamValue(l)],
    [t('状态'), l.completed && !l.error ? t('完成') : t('失败')],
    [t('耗时'), formatDuration(l.durationMs)],
    ['TTFT', l.ttftMs ? formatDuration(l.ttftMs) : '-'],
    ['tok/s', formatTPS(l.outputTokensPerSecond)],
    [t('输入/输出/缓存/总'), l.usageAvailable
      ? formatTokenCount(l.inputTokens) + ' / ' + formatTokenCount(l.outputTokens) + ' / ' + formatTokenCount(l.cachedTokens) + ' / ' + formatTokenCount(l.totalTokens)
      : t('Token 未知')]
  ];
  if (l.error) rows.push([t('错误'), l.error]);
  let html = '<div class="log-detail-grid">' + rows.map(r =>
    '<span class="k">' + esc(r[0]) + '</span><span class="v">' + esc(String(r[1])) + '</span>').join('') + '</div>';
  const attempts = Array.isArray(l.attempts) ? l.attempts : [];
  if (attempts.length) html += '<div class="log-detail-sep">' + t('尝试链路') + '</div>' + renderAttempts(attempts);
  html += '<div class="log-detail-sep">' + t('原始 JSON') + '</div><pre class="log-detail-json">' + esc(JSON.stringify(l, null, 2)) + '</pre>';
  _('logDetailBody').innerHTML = html;
  modal.style.display = 'flex';
}

function closeLogDetail() {
  const modal = _('logDetailModal');
  if (modal) modal.style.display = 'none';
  _logDetailIdx = -1;
}

function copyLogDetailJson(btn) {
  if (_logDetailIdx < 0) return;
  copyLogJson(_logDetailIdx);
}

// ---------- 下载 / 清空 / 全屏 ----------
function downloadLogs(btn) {
  const shown = _logItems.filter(logMatches);
  if (!shown.length) { toast(t('当前无可下载日志'), 'error'); return; }
  if (btn) btn.disabled = true;
  try {
    downloadLogsTSV(shown, 'cline2api-logs.txt', btn);
  } finally { if (btn) btn.disabled = false; }
}

// 仅失败请求（与行内「失败」判定一致），不受当前筛选影响
function downloadErrorLogs(btn) {
  const failed = _logItems.filter(l => !(l.completed && !l.error));
  if (!failed.length) { toast(t('当前无失败日志'), 'success'); return; }
  if (btn) btn.disabled = true;
  try {
    downloadLogsTSV(failed, 'cline2api-logs-errors.txt', btn);
  } finally { if (btn) btn.disabled = false; }
}

function downloadLogsTSV(list, filename, btn) {
  const orig = btn ? btn.innerHTML : '';
  try {
    const lines = [
      'time\tstatus\tprotocol\tmodel\taccount\tinput\toutput\tcached\ttotal\tdurationMs\tttftMs\ttok/s\terror\tid'
    ];
    list.forEach(l => {
      lines.push([
        l.startedAt, (l.completed && !l.error) ? 'ok' : 'fail', l.protocol || '', l.model || '', logUpstreamValue(l),
        l.usageAvailable ? l.inputTokens : '-', l.usageAvailable ? l.outputTokens : '-',
        l.usageAvailable ? l.cachedTokens : '-', l.usageAvailable ? l.totalTokens : '-',
        l.durationMs || 0, l.ttftMs || 0, l.outputTokensPerSecond || 0,
        (l.error || '').replace(/[\t\n\r]/g, ' '), l.id || ''
      ].join('\t'));
    });
    const blob = new Blob([lines.join('\n')], { type: 'text/plain;charset=utf-8' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = filename;
    document.body.appendChild(a); a.click(); document.body.removeChild(a);
    setTimeout(() => URL.revokeObjectURL(a.href), 4000);
    toast(t('已下载 ') + list.length + t(' 条日志'), 'success');
  } catch (e) { toast(t('下载失败: ') + e.message, 'error'); }
  if (btn) btn.innerHTML = orig;
}

async function clearRequestLogs(btn) {
  if (!(await showConfirm(t('确定清空所有请求日志？此操作不可恢复。')))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('清空中...'); }
  try {
    await api('POST', '/request-logs/clear');
    toast(t('日志已清空'), 'success');
    loadRequestLogs(true);
  } catch (e) { toast(t('清空失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

function toggleLogFullscreen(btn) {
  const tab = document.getElementById('tab-logs');
  if (!tab) return;
  const on = tab.classList.toggle('log-fullscreen');
  if (btn) btn.classList.toggle('btn-primary', on);
}

// ---------- 事件委托（避免行内拼接动态字符串） ----------
(function wireLogEvents() {
  // 表格行：单击展开尝试链路，双击复制 JSON
  const tbody = _('logTableBody');
  if (tbody) {
    tbody.addEventListener('click', e => {
      const tr = e.target.closest('tr.log-row');
      if (!tr || e.target.closest('.log-detail-btn')) return;
      if (!tr.classList.contains('has-attempts')) return;
      const l = _logItems[Number(tr.dataset.idx)];
      if (l && _logDetailMap[l.id]) toggleLogDetail(_logDetailMap[l.id]);
    });
    tbody.addEventListener('dblclick', e => {
      const tr = e.target.closest('tr.log-row');
      if (tr) copyLogJson(Number(tr.dataset.idx));
    });
  }
  const cards = _('logCards');
  if (cards) {
    cards.addEventListener('dblclick', e => {
      const art = e.target.closest('.log-card');
      if (art) copyLogJson(Number(art.dataset.idx));
    });
  }
  document.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeLogDetail();
  });
})();
