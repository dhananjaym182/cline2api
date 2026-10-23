// ========== 请求日志 ==========
let logCursor = '';
let logHasMore = false;
let _logRowSeq = 0;

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

async function loadRequestLogs(reset) {
  if (reset) { logCursor = ''; _logRowSeq = 0; }
  const cursor = logCursor;
  try {
    const path = '/request-logs?limit=50' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '');
    const d = await api('GET', path);
    const page = d.data;
    const items = page.items || [];
    logHasMore = !!page.hasMore;
    logCursor = page.nextCursor || '';
    _('logLoadMore').style.display = logHasMore ? 'block' : 'none';

    const tbody = _('logTableBody');
    const cards = _('logCards');
    if (reset && (!items || items.length === 0)) {
      tbody.innerHTML = '<tr><td colspan="12" class="empty">' + t('暂无请求日志') + '</td></tr>';
      cards.innerHTML = '<div class="empty">' + t('暂无请求日志') + '</div>';
      return;
    }

    const renderRow = l => {
      const st = l.completed
        ? '<span class="log-status ok">' + t('完成') + '</span>'
        : '<span class="log-status fail" title="' + esc(l.error || '') + '">' + t('失败') + '</span>';
      const tk = l.usageAvailable
        ? formatTokenCount(l.inputTokens) + '</td><td>' + formatTokenCount(l.outputTokens) + '</td><td>' + formatTokenCount(l.cachedTokens) + '</td><td>' + formatTokenCount(l.totalTokens)
        : '-</td><td>-</td><td>-</td><td>-';
      const attempts = Array.isArray(l.attempts) ? l.attempts : [];
      const hasAttempts = attempts.length > 0;
      const idx = _logRowSeq++;
      const detailId = 'logDetail' + idx;
      const rowAttr = hasAttempts
        ? ' class="log-row has-attempts" onclick="toggleLogDetail(\'' + detailId + '\')" title="' + t('点击查看尝试链路') + '"'
        : ' class="log-row"';
      return '<tr' + rowAttr + '>' +
        '<td>' + logTimestampHtml(l.startedAt) + '</td>' +
        '<td>' + esc(logUpstreamValue(l)) + '</td>' +
        '<td>' + esc(l.protocol || '-') + '</td>' +
        '<td>' + logModelCell(l) + '</td>' +
        '<td>' + tk + '</td>' +
        '<td>' + formatDuration(l.durationMs) + '</td>' +
        '<td>' + (l.ttftMs ? formatDuration(l.ttftMs) : '-') + '</td>' +
        '<td>' + formatTPS(l.outputTokensPerSecond) + '</td>' +
        '<td>' + st + '</td></tr>' +
        (hasAttempts
          ? '<tr id="' + detailId + '" class="log-detail" style="display:none"><td colspan="12" class="log-detail-cell">' + renderAttempts(attempts) + '</td></tr>'
          : '');
    };
    const renderCard = l => {
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
      return '<article class="account-card">' +
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
      '</article>';
    };

    if (reset) {
      tbody.innerHTML = items.map(renderRow).join('');
      cards.innerHTML = items.map(renderCard).join('');
    } else {
      tbody.insertAdjacentHTML('beforeend', items.map(renderRow).join(''));
      cards.insertAdjacentHTML('beforeend', items.map(renderCard).join(''));
    }
  } catch (e) { toast(t('加载日志失败: ') + e.message, 'error'); }
}
