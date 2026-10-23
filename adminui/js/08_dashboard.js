// ========== 仪表盘：池容量 / 指标砖 / 需要关注 ==========
function poolRequestTotal() {
  // /stats 无全局请求数：用账号列表 usageCount 求和（账号加载后由 renderAttention 一并刷新）
  return (_lastAccountList || []).reduce((sum, a) => sum + (Number(a.usageCount) || 0), 0);
}

function renderPoolCapacity(s) {
  const total = Number(s.total) || 0;
  const pct = n => (total > 0 ? Math.max(0, Math.min(100, (Number(n) || 0) / total * 100)) : 0);
  const seg = (id, v) => { const el = _(id); if (el) el.style.width = pct(v) + '%'; };
  seg('capSegActive', s.active);
  seg('capSegCooldown', s.cooldown);
  seg('capSegExpired', s.expired);
}

function renderDashboardExtras(s) {
  const totalTokens = Number(s.totalTokens) || 0;
  const cached = Number(s.cachedTokens) || 0;
  const reqs = poolRequestTotal();
  const set = (id, v) => { const el = _(id); if (el) el.textContent = v; };
  set('statPoolRequests', reqs > 0 ? formatNumber(reqs) : '-');
  set('statAvgPerReq', reqs > 0 && totalTokens > 0 ? t('平均 ') + formatNumber(Math.round(totalTokens / reqs)) + t('/请求') : t('平均 -/请求'));
  set('statCacheRate', totalTokens > 0 ? t('缓存率 ') + (cached / totalTokens * 100).toFixed(1) + '%' : t('缓存率 -'));
}

// 需要关注：过期账号 / 冷却中账号 / 模型级冷却 / 需重新登录的连接
let _attentionConnCache = null;
let _attentionConnAt = 0;
async function renderAttention() {
  const box = _('dashAttention');
  if (!box) return;
  const rows = [];
  const row = (tone, icon, label, count, go) => {
    rows.push('<div class="attention-row ' + tone + '">' + icon +
      '<span class="a-label">' + label + '</span>' +
      (count != null ? '<span class="a-count">' + count + '</span>' : '') +
      (go ? '<span class="a-link" onclick="' + go + '">' + t('前往处理') + '</span>' : '') +
      '</div>');
  };
  const icRed = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>';
  const icYellow = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>';
  const icBlue = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg>';
  const list = _lastAccountList || [];
  const expired = list.filter(a => a.status === 'expired').length;
  const cooling = list.filter(a => a.status === 'cooldown').length;
  const now = Date.now();
  const modelCooling = list.filter(a => {
    const cools = a.modelCooldowns || {};
    return Object.keys(cools).some(k => { const ts = new Date(cools[k]).getTime(); return !isNaN(ts) && ts > now; });
  }).length;
  if (expired > 0) row('a-bad', icRed, t('已过期账号 · 需要重新导入或登录'), expired, "switchTab('accounts')");
  if (cooling > 0) row('a-warn', icYellow, t('冷却中账号 · 稍后自动恢复'), cooling, "switchTab('accounts')");
  if (modelCooling > 0) row('a-warn', icYellow, t('存在模型级冷却的账号'), modelCooling, "switchTab('accounts')");
  // 登录连接：60s 内的缓存直接用，失败静默（连接页自己会报错）
  if (!_attentionConnCache || now - _attentionConnAt > 60000) {
    try {
      const d = await api('GET', '/connections');
      _attentionConnCache = (d.data && (d.data.connections || d.data.items || d.data)) || [];
      _attentionConnAt = now;
    } catch (e) { if (!_attentionConnCache) _attentionConnCache = []; }
  }
  const reauth = (Array.isArray(_attentionConnCache) ? _attentionConnCache : []).filter(c => c.status && c.status !== 'active').length;
  if (reauth > 0) row('a-info', icBlue, t('需重新登录的连接'), reauth, "switchTab('providers')");
  if (!rows.length) {
    box.innerHTML = '<div class="attention-empty">' + t('暂无需要处理的项目 · 账号池健康') + '</div>';
    return;
  }
  box.innerHTML = rows.join('');
}

async function loadStats() {
  try {
    const d = await api('GET', '/stats');
    const s = d.data;
    const set = (id, v) => { const el = _(id); if (el) el.textContent = v; };
    set('statTotal', s.total);
    set('statActive', s.active);
    set('statCooldown', s.cooldown);
    set('statExpired', s.expired);
    renderPoolCapacity(s);
    set('statPromptTokens', formatTokenCount(s.promptTokens));
    set('statCompletionTokens', formatTokenCount(s.completionTokens));
    set('statTotalTokens', formatTokenCount(s.totalTokens));
    set('statCachedTokens', formatTokenCount(s.cachedTokens));
    renderDashboardExtras(s);
    const oc = s.opencodeToday || {};
    set('statOcRequests', oc.requests != null ? oc.requests : '-');
    set('statOcInputTokens', formatTokenCount(oc.inputTokens || 0));
    set('statOcOutputTokens', formatTokenCount(oc.outputTokens || 0));
    set('statOcTotalTokens', formatTokenCount(oc.totalTokens || 0));
    if (s.version) _('settingVersion').value = s.version;
    // 设置项由 /config 返回为准；这里仅在用户未编辑时回填，避免轮询清掉正在输入的内容
    const fillIfIdle = (id, val) => { const el = _(id); if (el && document.activeElement !== el && !el.dataset.dirty) el.value = val; };
    if (s.strategy) fillIfIdle('settingStrategy', s.strategy);
    fillIfIdle('settingModelChain', (s.modelChain || []).join(', '));
  } catch (e) { toast(t('加载失败') + ': ' + e.message, 'error'); }
}
