// ========== 配额与预算 ==========
// 数据源全部现成：/admin/api/providers 的 health 快照（rpm/daily 用量、limits、
// 冷却与恢复时间，providers_health.go）+ /admin/api/stats 的账号 Token 总量。
// 计数在内存，重启后从零学起——页面标注这一点，避免误读为「预算丢失」。
let _quotaData = null;

async function loadQuota(showSkeleton) {
  const listEl = _('quotaList');
  if (!listEl) return;
  if (showSkeleton !== false) {
    listEl.innerHTML = '<div class="sk-stack" style="padding:16px"><span class="skeleton"></span><span class="skeleton w70"></span></div>';
  }
  try {
    const pv = await api('GET', '/providers');
    const st = await api('GET', '/stats');
    _quotaData = { providers: (pv.data && pv.data.providers) || [], health: (pv.data && pv.data.health) || {}, stats: (st && st.data) || {} };
    renderQuota();
  } catch (e) {
    if (showSkeleton !== false) listEl.innerHTML = listEmptyState(ICON_MODELS, t('加载失败') + ': ' + esc(e.message));
    toast(t('加载失败') + ': ' + e.message, 'error');
  }
}

function fmtDurationZh(ms) {
  if (ms <= 0) return '0s';
  const totalMin = Math.floor(ms / 60000);
  const h = Math.floor(totalMin / 60);
  if (h > 0) return h + 'h ' + (totalMin % 60) + 'm';
  if (totalMin > 0) return totalMin + 'm';
  return Math.ceil(ms / 1000) + 's';
}
// 每日预算按 UTC 日计数（providerDaily 以 UTC 日期为键），倒计时到下一 UTC 零点
function utcMidnightCountdown() {
  return (Math.floor(Date.now() / 86400000) + 1) * 86400000 - Date.now();
}

function quotaMeter(label, used, limit) {
  used = Number(used) || 0;
  limit = Number(limit) || 0;
  if (limit > 0) {
    const ratio = Math.min(1, used / limit);
    const left = 1 - ratio;
    const tone = left >= 0.7 ? 'ok' : (left >= 0.3 ? 'warn' : 'bad');
    // 与 Provider 页健康列同一标记结构（.quota-row>.quota-meter>span.ok|warn|bad + .quota-num）
    return '<div class="quota-row quota-metric">' +
      '<span class="quota-label">' + esc(t(label)) + '</span>' +
      '<span class="quota-meter"><span class="' + tone + '" style="width:' + (ratio * 100).toFixed(1) + '%"></span></span>' +
      '<span class="quota-num">' + formatNumber(used) + ' / ' + formatNumber(limit) + '（' + Math.round(ratio * 100) + '%）</span>' +
    '</div>';
  }
  return '<div class="quota-row quota-metric quota-metric-free">' +
    '<span class="quota-label">' + esc(t(label)) + '</span>' +
    '<span class="quota-num">' + formatNumber(used) + ' · ' + esc(t('未设预算')) + '</span>' +
  '</div>';
}

function renderQuotaCard(p, health) {
  const h = health || {};
  const lim = h.limits || {};
  const cds = h.cooldowns || [];
  let cdHtml;
  if (!cds.length) {
    cdHtml = '<div class="quota-cd-none">' + esc(t('无冷却')) + '</div>';
  } else {
    const rows = cds.map(c =>
      '<div class="quota-cd-row"><span class="mono" title="' + escAttr(c.model) + '">' + esc(c.model) + '</span>' +
      '<span class="quota-cd-time">' + esc(formatCooldown(c.until)) + '</span>' +
      '<button class="btn btn-sm" onclick="resetProviderCooldown(\'' + escAttr(p.id) + '\',\'' + escAttr(c.model) + '\',this)">' + esc(t('重置')) + '</button></div>'
    ).join('');
    cdHtml = '<div class="quota-cd-list">' + rows + '</div>' +
      '<button class="btn btn-sm" onclick="resetProviderCooldown(\'' + escAttr(p.id) + '\',\'\',this)">' + esc(t('重置全部冷却')) + '</button>';
  }
  return '<div class="section quota-card">' +
    '<div class="section-title"><span>' + esc(p.name || p.id) + '</span>' +
      (p.enabled ? '' : ' <span class="model-tag">' + esc(t('已停用')) + '</span>') +
    '</div>' +
    '<div class="section-body">' +
      '<div class="quota-meters">' +
        quotaMeter('每分钟请求', h.rpm, lim.rpm) +
        quotaMeter('每日请求', h.daily, lim.daily) +
      '</div>' +
      '<div class="quota-cooldowns">' +
        '<div class="quota-cd-title">' + esc(t('冷却中的模型')) + '<span class="quota-cd-count">' + cds.length + '</span></div>' +
        cdHtml +
      '</div>' +
    '</div>' +
  '</div>';
}

function renderQuota() {
  const listEl = _('quotaList'), sumEl = _('quotaSummary');
  if (!listEl) return;
  const d = _quotaData;
  if (!d) { listEl.innerHTML = ''; return; }
  const st = d.stats || {};
  if (sumEl) {
    sumEl.innerHTML =
      '<div class="quota-stat"><div class="quota-stat-n">' + formatNumber(st.total || 0) + '</div><div class="quota-stat-l">' + esc(t('账号总数')) + '</div></div>' +
      '<div class="quota-stat"><div class="quota-stat-n">' + formatNumber(st.active || 0) + '</div><div class="quota-stat-l">' + esc(t('活跃账号')) + '</div></div>' +
      '<div class="quota-stat"><div class="quota-stat-n">' + formatNumber(st.usageCount || 0) + '</div><div class="quota-stat-l">' + esc(t('总请求')) + '</div></div>' +
      '<div class="quota-stat"><div class="quota-stat-n">' + formatTokenCount(st.totalTokens || 0) + '</div><div class="quota-stat-l">' + esc(t('总 Token')) + '</div></div>' +
      '<div class="quota-stat"><div class="quota-stat-n">' + esc(fmtDurationZh(utcMidnightCountdown())) + '</div><div class="quota-stat-l">' + esc(t('距每日预算 UTC 重置')) + '</div></div>';
  }
  const provs = d.providers || [];
  if (!provs.length) {
    listEl.innerHTML = listEmptyState(ICON_MODELS, t('暂无 Provider，先在「自定义 Provider」页添加'));
    return;
  }
  const health = d.health || {};
  listEl.innerHTML = provs.map(p => renderQuotaCard(p, health[p.id])).join('');
}

async function resetProviderCooldown(id, model, btn) {
  if (btn) btn.disabled = true;
  try {
    const d = await api('POST', '/providers/cooldown-reset', { id: id, model: model || '' });
    const n = (d.data && d.data.removed) || 0;
    toast(t('已重置 ') + n + t(' 条冷却'), 'success');
    loadQuota(false);
  } catch (e) {
    toast(t('重置失败') + ': ' + e.message, 'error');
    if (btn) btn.disabled = false;
  }
}
