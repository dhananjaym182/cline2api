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

// 运行时长格式化：3d 4h 21m / 4h 21m / 21m / 42s
function fmtUptime(sec) {
  sec = Math.max(0, Math.floor(sec || 0));
  const d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60);
  if (d > 0) return d + 'd ' + h + 'h';
  if (h > 0) return h + 'h ' + m + 'm';
  if (m > 0) return m + 'm';
  return sec + 's';
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
    // 运行时长（/stats startedAt/uptimeSeconds）：判断重启是否真正生效的直观信号
    if (s.uptimeSeconds != null) {
      const row = _('aboutUptimeRow');
      if (row) row.hidden = false;
      set('aboutUptime', fmtUptime(s.uptimeSeconds));
      if (s.startedAt) {
        const dt = new Date(s.startedAt);
        if (!isNaN(dt)) set('aboutStartedAt', dt.toLocaleString(LC(), { hour12: false }));
      }
    }
    // 设置项由 /config 返回为准；这里仅在用户未编辑时回填，避免轮询清掉正在输入的内容
    const fillIfIdle = (id, val) => { const el = _(id); if (el && document.activeElement !== el && !el.dataset.dirty) el.value = val; };
    if (s.strategy) fillIfIdle('settingStrategy', s.strategy);
    fillIfIdle('settingModelChain', (s.modelChain || []).join(', '));
  } catch (e) { toast(t('加载失败') + ': ' + e.message, 'error'); }
}

// ========== 仪表盘总览：verdict hero / 吞吐柱状 / 上游分布 ==========
let _dashHours = 24;
let _dashTimer = 0;

function dashWindowLabel(h) {
  if (h === 1) return '1h';
  if (h === 24) return '24h';
  if (h === 72) return '3d';
  if (h === 168) return '7d';
  return h + 'h';
}

function setDashRange(h, btn) {
  _dashHours = h;
  document.querySelectorAll('.dash-ranges .range').forEach(b => b.classList.toggle('active', b === btn));
  loadDashboardOverview();
}

function startDashPolling() {
  if (_dashTimer) return;
  _dashTimer = setInterval(() => {
    if (document.visibilityState !== 'visible') return;
    const el = document.querySelector('.nav-item.active');
    if (!el || el.dataset.tab !== 'dashboard') return;
    loadDashboardOverview();
  }, 60000);
}

function stopDashPolling() {
  if (_dashTimer) { clearInterval(_dashTimer); _dashTimer = 0; }
}

async function loadDashboardOverview() {
  try {
    const d = await api('GET', '/dashboard/overview?hours=' + _dashHours);
    renderDashboardOverview(d.data);
  } catch (e) {
    const hero = _('dashHero');
    if (hero) hero.dataset.tone = 'offline';
    const v = _('dashVerdict');
    if (v) v.textContent = t('无法连接服务');
  }
}

function dashCountUp(el, target) {
  if (!el) return;
  target = Number(target) || 0;
  const prev = Number(String(el.textContent).replace(/[^\d]/g, '')) || 0;
  if (prev === target || window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    el.textContent = formatNumber(target);
    return;
  }
  const t0 = performance.now(), dur = 600;
  const step = now => {
    const p = Math.min(1, (now - t0) / dur);
    const eased = 1 - Math.pow(1 - p, 3);
    el.textContent = formatNumber(Math.round(prev + (target - prev) * eased));
    if (p < 1) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}

function renderDashboardOverview(o) {
  if (!o) return;
  const totals = o.totals || {};
  const reqs = Number(totals.requests) || 0;
  const ok = Number(totals.success) || 0;
  const err = Number(totals.fail) || 0;
  const rate = reqs > 0 ? ok / reqs : 0;
  const hero = _('dashHero');
  const buckets = o.buckets || [];
  const last = buckets[buckets.length - 1] || { ok: 0, err: 0 };
  const live = (last.ok + last.err) > 0;

  // verdict: idle / warning / critical / good; breathing dot only on live traffic
  let verdict, tone;
  if (reqs === 0) { verdict = t('暂无流量'); tone = 'idle'; }
  else if (rate < 0.8 && reqs >= 5) { verdict = t('失败率偏高'); tone = 'critical'; }
  else if (rate < 0.95) { verdict = t('存在失败请求'); tone = 'warning'; }
  else { verdict = t('运行正常'); tone = 'good'; }
  if (hero) hero.dataset.tone = tone;
  const v = _('dashVerdict');
  if (v) v.textContent = verdict;
  const dot = _('dashLiveDot');
  if (dot) dot.classList.toggle('breathing', live && tone !== 'idle');
  const badge = _('dashLiveBadge');
  if (badge) badge.hidden = !live;

  dashCountUp(_('dashReqCount'), reqs);
  const lbl = _('dashCountLabel');
  if (lbl) lbl.textContent = t('窗口内请求 · 最近 ') + dashWindowLabel(Number(o.windowHours) || _dashHours);
  const okPct = reqs > 0 ? (ok / reqs * 100) : 100;
  const ro = _('dashRatioOk'), re = _('dashRatioErr');
  if (ro) ro.style.width = okPct + '%';
  if (re) re.style.width = (100 - okPct) + '%';
  const oc = _('dashOkCount'), ec = _('dashErrCount');
  if (oc) oc.textContent = formatNumber(ok);
  if (ec) ec.textContent = formatNumber(err);
  const rl = _('dashRateLabel');
  if (rl) rl.textContent = reqs > 0 ? (rate * 100).toFixed(1) + '%' : '—';

  // KPI tiles with tone meters
  const setKpi = (numId, meterId, text, ratio, toneClass) => {
    const n = _(numId);
    if (n) n.textContent = text;
    const m = _(meterId);
    if (m) {
      m.style.width = Math.max(0, Math.min(100, (ratio || 0) * 100)) + '%';
      m.className = toneClass || '';
    }
  };
  const lat = Number(totals.avgLatencyMs) || 0;
  const ttft = Number(totals.avgTtftMs) || 0;
  const tk = Number(totals.tokens) || 0;
  const fb = Number(totals.fallbacks) || 0;
  const toneFor = (v, warn, bad) => !v ? '' : (v >= bad ? 'bad' : (v >= warn ? 'warn' : 'ok'));
  setKpi('dashKpiLatency', 'dashMeterLatency', lat ? Math.round(lat) + ' ms' : '—',
    reqs ? Math.min(1, lat / 15000) : 0, toneFor(lat, 3000, 10000));
  setKpi('dashKpiTtft', 'dashMeterTtft', ttft ? Math.round(ttft) + ' ms' : '—',
    reqs ? Math.min(1, ttft / 6000) : 0, toneFor(ttft, 1500, 4000));
  setKpi('dashKpiTokens', 'dashMeterTokens', tk ? formatTokenCount(tk) : '—',
    tk ? Math.min(1, Math.log10(tk + 1) / 7) : 0, tk ? 'ok' : '');
  setKpi('dashKpiFallback', 'dashMeterFallback', fb ? formatNumber(fb) : '—',
    reqs ? Math.min(1, fb / reqs) : 0, toneFor(reqs ? fb / reqs : 0, 0.1, 0.3));

  renderDashChart(buckets, Number(o.bucketSecs) || 900);
  renderDashFleet(o.fleet || []);
  renderDashModels(o.models || []);
  renderDashRuntime(o.runtime || {});
}

function renderDashChart(buckets, bucketSecs) {
  const box = _('dashThroughput');
  if (!box) return;
  const total = buckets.reduce((s, b) => s + (b.ok || 0) + (b.err || 0), 0);
  if (!buckets.length || total === 0) {
    box.innerHTML = '<div class="attention-empty">' + t('窗口内暂无请求流量') + '</div>';
    return;
  }
  const W = 900, H = 110, pad = 4;
  const max = Math.max(1, ...buckets.map(b => (b.ok || 0) + (b.err || 0)));
  const bw = Math.max(3, (W - pad * 2) / buckets.length - 2);
  const gap = (W - pad * 2) / buckets.length;
  const fmtT = ts => {
    const d = new Date(ts * 1000);
    const hm = String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
    return bucketSecs >= 3600 ? (d.getMonth() + 1) + '/' + d.getDate() + ' ' + hm : hm;
  };
  let rects = '';
  buckets.forEach((b, i) => {
    const x = pad + i * gap;
    const okH = (b.ok / max) * (H - 16);
    const errH = (b.err / max) * (H - 16);
    const tip = fmtT(b.t) + ' · ✓' + b.ok + (b.err ? ' ✗' + b.err : '');
    if (b.ok) rects += '<rect class="bar-ok" x="' + x.toFixed(1) + '" y="' + (H - 14 - okH).toFixed(1) + '" width="' + bw.toFixed(1) + '" height="' + okH.toFixed(1) + '" rx="1.5"><title>' + tip + '</title></rect>';
    if (b.err) rects += '<rect class="bar-err" x="' + x.toFixed(1) + '" y="' + (H - 14 - okH - errH).toFixed(1) + '" width="' + bw.toFixed(1) + '" height="' + errH.toFixed(1) + '" rx="1.5"><title>' + tip + '</title></rect>';
    if (!b.ok && !b.err) rects += '<rect class="bar-empty" x="' + x.toFixed(1) + '" y="' + (H - 15) + '" width="' + bw.toFixed(1) + '" height="2" rx="1"></rect>';
  });
  box.innerHTML = '<svg viewBox="0 0 ' + W + ' ' + H + '" preserveAspectRatio="none" class="dash-bar-chart" role="img" aria-label="' + t('吞吐趋势') + '">' +
    '<line x1="0" y1="' + (H - 12) + '" x2="' + W + '" y2="' + (H - 12) + '" class="chart-base"/>' + rects + '</svg>' +
    '<div class="chart-axis"><span>' + fmtT(buckets[0].t) + '</span><span>' + fmtT(buckets[buckets.length - 1].t) + '</span></div>';
}

function dashSparkline(series, w, h) {
  w = w || 88; h = h || 22;
  if (!series || !series.length) return '';
  const max = Math.max(1, ...series);
  const step = w / (series.length - 1 || 1);
  const pts = series.map((v, i) => (i * step).toFixed(1) + ',' + (h - 2 - (v / max) * (h - 4)).toFixed(1)).join(' ');
  return '<svg viewBox="0 0 ' + w + ' ' + h + '" class="dash-spark" aria-hidden="true"><polyline points="' + pts + '" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round"/></svg>';
}

function renderDashFleet(fleet) {
  const box = _('dashFleet');
  if (!box) return;
  if (!fleet.length) {
    box.innerHTML = '<div class="attention-empty">' + t('窗口内暂无请求流量') + '</div>';
    return;
  }
  const maxReqs = Math.max(1, ...fleet.map(f => f.requests || 0));
  box.innerHTML = fleet.map((f, i) => {
    const rate = Number(f.rate) || 0;
    const tone = rate >= 0.98 ? 'ok' : (rate >= 0.9 ? 'warn' : 'bad');
    return '<div class="fleet-row">' +
      '<span class="fleet-rank">' + (i + 1) + '</span>' +
      '<span class="fleet-label" title="' + escAttr(f.key) + '">' + esc(f.label) + '</span>' +
      '<span class="fleet-spark tone-' + tone + '">' + dashSparkline(f.series) + '</span>' +
      '<span class="fleet-count">' + formatNumber(f.requests) + '</span>' +
      '<span class="fleet-meter"><span class="' + tone + '" style="width:' + Math.max(2, (f.requests / maxReqs) * 100) + '%"></span></span>' +
      '<span class="fleet-rate ' + tone + '">' + (rate * 100).toFixed(0) + '%</span>' +
      '</div>';
  }).join('');
}

function renderDashModels(models) {
  const box = _('dashModels');
  if (!box) return;
  if (!models.length) {
    box.innerHTML = '<div class="attention-empty">' + t('窗口内暂无请求流量') + '</div>';
    return;
  }
  const max = Math.max(1, ...models.map(m => m.requests || 0));
  box.innerHTML = models.map(m =>
    '<div class="model-usage-row">' +
      '<span class="model-name" title="' + escAttr(m.model) + '">' + esc(m.model) + '</span>' +
      '<span class="model-meter"><span style="width:' + Math.max(2, (m.requests / max) * 100) + '%"></span></span>' +
      '<span class="model-count">' + formatNumber(m.requests) + '</span>' +
    '</div>').join('');
}

function renderDashRuntime(rt) {
  const box = _('dashRuntime');
  if (!box) return;
  const chips = [];
  if (rt.version) chips.push('<span class="chip">v' + esc(rt.version) + '</span>');
  if (rt.strategy) chips.push('<span class="chip">' + t('策略') + ' ' + esc(rt.strategy) + '</span>');
  if (rt.defaultModel) chips.push('<span class="chip">' + t('默认') + ' ' + esc(rt.defaultModel) + '</span>');
  if (rt.onlyFree) chips.push('<span class="chip ok">' + t('仅免费模型') + '</span>');
  box.innerHTML = chips.join('');
}

