// ========== API 密钥管理 ==========
async function loadKeys() {
  try {
    const d = await api('GET', '/keys');
    const keys = d.data.keys;
    const el = _('keysList');
    if (!keys || keys.length === 0) {
      el.innerHTML = '<div class="empty-state"><div class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg></div>' + t('暂无 API 密钥') + '</div>';
      return;
    }
    el.innerHTML = keys.map(k =>
      '<div class="flex" style="margin-bottom:8px">' +
        '<span class="key-display" style="flex:1" onclick="copyText(\'' + k + '\')" title="' + t('点击复制') + '">' + esc(k) + '</span>' +
        '<button class="btn btn-sm btn-danger" onclick="deleteKey(\'' + k + '\', this)" title="' + t('删除') + '" aria-label="' + t('删除') + '">✕</button>' +
      '</div>'
    ).join('');
  } catch (e) { _('keysList').innerHTML = '<div class="empty">' + t('加载失败') + '</div>'; }
}

async function generateKey(btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('生成中...'); }
  try {
    const d = await api('POST', '/keys/generate');
    const key = d.data.key;
    _('keyGenResult').innerHTML =
      '<div style="background:var(--green-soft);border:1px solid var(--green);border-radius:var(--radius-sm);padding:14px">' +
        '<div style="color:var(--green);font-weight:600;margin-bottom:8px">✓ ' + t('新密钥已生成（点击复制）') + '</div>' +
        '<div class="key-display" onclick="copyText(\'' + key + '\')">' + esc(key) + '</div>' +
      '</div>';
    loadKeys();
    toast(t('密钥已生成'), 'success');
    setTimeout(() => _('keyGenResult').innerHTML = '', 8000);
  } catch (e) { toast(t('生成失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

async function deleteKey(key, btn) {
  if (!(await showConfirm(t('确定删除此密钥？')))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('删除中...'); }
  try {
    await api('POST', '/keys/delete', { key });
    toast(t('密钥已删除'), 'success');
    loadKeys();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

async function deleteAllKeys() {
  if (!(await showConfirm(t('确定删除所有 API 密钥？')))) return;
  try {
    const d = await api('GET', '/keys');
    const keys = d.data.keys || [];
    for (const k of keys) await api('POST', '/keys/delete', { key: k });
    toast(t('全部密钥已删除'), 'success');
    loadKeys();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

function copyText(t) {
  navigator.clipboard.writeText(t).then(() => toast(t('已复制到剪贴板'), 'success')).catch(() => {
    const ta = document.createElement('textarea');
    ta.value = t; document.body.appendChild(ta); ta.select(); document.execCommand('copy'); document.body.removeChild(ta);
    toast(t('已复制到剪贴板'), 'success');
  });
}

// ========== 配置管理 ==========
async function updateConfig() {
  const strategy = _('settingStrategy').value;
  const defaultModel = _('settingDefModel').value;
  const chainField = _('settingModelChain');
  const modelChain = chainField.value.split(',').map(s => s.trim()).filter(Boolean);
  const onlyFree = _('settingOnlyFree') ? _('settingOnlyFree').value === 'true' : undefined;
  try {
    await api('POST', '/config/update', { strategy, defaultModel, modelChain, onlyFree });
    delete chainField.dataset.dirty;
    toast(t('配置已更新'), 'success');
  } catch (e) { toast(t('更新失败: ') + e.message, 'error'); }
}

// ========== 调优参数（retry/stream/affinity/wire/模型策略） ==========
function tuningNum(id) {
  const el = _(id);
  if (!el || el.value === '') return 0;
  const n = parseInt(el.value, 10);
  return isNaN(n) ? 0 : n;
}

// ========== 结构化编辑器：模型别名 / 错误规则 / 排除模型 ==========
const ERROR_RULE_ACTIONS = ['stop', 'stop_and_cooldown', 'continue', 'continue_and_cooldown'];

function ensureModelIdList() {
  const dl = _('modelIdList');
  if (!dl || !_cachedModels.length) return;
  if (dl.dataset.filled === String(_cachedModels.length)) return;
  dl.innerHTML = '';
  const seen = {};
  _cachedModels.forEach(m => {
    const id = m.id || m.ID;
    if (!id || seen[id]) return;
    seen[id] = true;
    const opt = document.createElement('option');
    opt.value = id;
    dl.appendChild(opt);
  });
  dl.dataset.filled = String(_cachedModels.length);
}

function seCell(cls, el) {
  const span = document.createElement('span');
  span.className = cls;
  span.appendChild(el);
  return span;
}

function seInput(cls, placeholder, value, listId) {
  const inp = document.createElement('input');
  inp.type = 'text';
  inp.className = cls;
  inp.placeholder = placeholder;
  inp.value = value || '';
  if (listId) inp.setAttribute('list', listId);
  return inp;
}

function addAliasRow(alias, target, provider) {
  ensureModelIdList();
  const box = _('tunAliasesEditor');
  if (!box) return null;
  const row = document.createElement('div');
  row.className = 'se-row alias-row';
  const a = seInput('se-alias', t('别名'), alias);
  const tgt = seInput('se-target', t('目标模型'), target, 'modelIdList');
  const prov = seInput('se-provider', t('Provider（可选）'), provider);
  const rem = document.createElement('button');
  rem.type = 'button';
  rem.className = 'btn btn-sm btn-danger se-remove';
  rem.textContent = '✕';
  rem.title = t('移除');
  rem.addEventListener('click', () => { row.remove(); checkAliasDupes(); refreshSaveBar(); });
  a.addEventListener('input', checkAliasDupes);
  row.appendChild(a);
  row.appendChild(document.createTextNode(' → '));
  row.appendChild(tgt);
  row.appendChild(document.createTextNode(' @ '));
  row.appendChild(prov);
  row.appendChild(rem);
  box.appendChild(row);
  checkAliasDupes();
  return row;
}

function readAliasRows() {
  const out = [];
  document.querySelectorAll('#tunAliasesEditor .alias-row').forEach(r => {
    const alias = r.querySelector('.se-alias').value.trim();
    const target = r.querySelector('.se-target').value.trim();
    const provider = r.querySelector('.se-provider').value.trim();
    if (!alias && !target) return;
    out.push({ alias: alias, target: target, provider: provider });
  });
  return out;
}

function checkAliasDupes() {
  const seen = {}, dups = {};
  document.querySelectorAll('#tunAliasesEditor .alias-row').forEach(r => {
    const inp = r.querySelector('.se-alias');
    const v = inp.value.trim().toLowerCase();
    if (!v) { inp.classList.remove('dup'); return; }
    if (seen[v]) { inp.classList.add('dup'); dups[v] = true; } else { seen[v] = true; inp.classList.remove('dup'); }
  });
  const warn = _('aliasDupWarn');
  const n = Object.keys(dups).length;
  if (warn) {
    warn.hidden = n === 0;
    warn.textContent = n ? t('别名重复：保存会被拒绝') + '（' + n + '）' : '';
  }
}

function addErrorRuleRow(codes, match, action, regex) {
  const box = _('tunErrorRulesEditor');
  if (!box) return null;
  const row = document.createElement('div');
  row.className = 'se-row rule-row';
  const codesInp = seInput('se-codes', t('状态码 429,503'), (codes || []).join(','));
  codesInp.style.width = '110px';
  const matchInp = seInput('se-match', t('错误文本 / 正则'));
  matchInp.value = match || '';
  const actSel = document.createElement('select');
  actSel.className = 'se-action';
  ERROR_RULE_ACTIONS.forEach(a => {
    const opt = document.createElement('option');
    opt.value = a; opt.textContent = a;
    actSel.appendChild(opt);
  });
  actSel.value = action || 'continue';
  const regexWrap = document.createElement('label');
  regexWrap.className = 'se-regex';
  const regexInp = document.createElement('input');
  regexInp.type = 'checkbox';
  regexInp.checked = !!regex;
  regexWrap.appendChild(regexInp);
  regexWrap.appendChild(document.createTextNode(' regex'));
  const up = document.createElement('button');
  up.type = 'button'; up.className = 'btn btn-sm se-move'; up.textContent = '↑';
  up.title = t('上移');
  up.addEventListener('click', () => { const prev = row.previousElementSibling; if (prev) { row.parentNode.insertBefore(row, prev); refreshSaveBar(); } });
  const down = document.createElement('button');
  down.type = 'button'; down.className = 'btn btn-sm se-move'; down.textContent = '↓';
  down.title = t('下移');
  down.addEventListener('click', () => { const next = row.nextElementSibling; if (next) { row.parentNode.insertBefore(next, row); refreshSaveBar(); } });
  const rem = document.createElement('button');
  rem.type = 'button'; rem.className = 'btn btn-sm btn-danger se-remove'; rem.textContent = '✕';
  rem.title = t('移除');
  rem.addEventListener('click', () => { row.remove(); refreshSaveBar(); });
  row.appendChild(codesInp);
  row.appendChild(matchInp);
  row.appendChild(actSel);
  row.appendChild(regexWrap);
  row.appendChild(up);
  row.appendChild(down);
  row.appendChild(rem);
  box.appendChild(row);
  return row;
}

function readErrorRuleRows() {
  const out = [];
  document.querySelectorAll('#tunErrorRulesEditor .rule-row').forEach(r => {
    const codes = r.querySelector('.se-codes').value.split(',').map(s => parseInt(s.trim(), 10)).filter(n => !isNaN(n));
    const match = r.querySelector('.se-match').value.trim();
    const rule = {
      statusCodes: codes,
      match: match,
      action: r.querySelector('.se-action').value,
      regex: r.querySelector('.se-regex input').checked
    };
    if (!codes.length && !match) return;
    out.push(rule);
  });
  return out;
}

function excludedModelIds() {
  return _cachedModels.map(m => m.id || m.ID).filter(Boolean);
}

function wildcardToRegex(pattern) {
  const escaped = pattern.replace(/[.*+?^${}()|[\]\\]/g, ch => ch === '*' ? '.*' : '\\' + ch);
  return new RegExp('^' + escaped + '$', 'i');
}

function countExcludedMatch(pattern) {
  pattern = pattern.trim();
  if (!pattern) return 0;
  const ids = excludedModelIds();
  if (pattern.indexOf('*') === -1) return ids.filter(id => id.toLowerCase() === pattern.toLowerCase()).length;
  const re = wildcardToRegex(pattern);
  return ids.filter(id => re.test(id)).length;
}

function updateExcludedMatchCounts() {
  const box = _('excludedMatchCounts');
  if (!box) return;
  box.innerHTML = '';
  const raw = (_('tunExcluded').value || '').trim();
  if (!raw || !_cachedModels.length) return;
  raw.split(',').map(s => s.trim()).filter(Boolean).forEach(pattern => {
    const n = countExcludedMatch(pattern);
    const chip = document.createElement('span');
    chip.className = 'excl-count' + (n ? '' : ' none');
    chip.textContent = pattern + ' → ' + n;
    box.appendChild(chip);
  });
}

function toggleExcludedPicker() {
  const p = _('excludedPicker');
  if (!p) return;
  const show = p.hidden;
  p.hidden = !show;
  if (show) renderExcludedPicker();
}

function renderExcludedPicker() {
  const list = _('excludedPickerList');
  if (!list) return;
  list.innerHTML = '';
  ensureModelIdList();
  const current = new Set((_('tunExcluded').value || '').split(',').map(s => s.trim().toLowerCase()).filter(Boolean));
  const q = (_('excludedPickerSearch').value || '').trim().toLowerCase();
  const ids = [...new Set(excludedModelIds())].sort();
  const visible = ids.filter(id => !q || id.toLowerCase().indexOf(q) !== -1).slice(0, 200);
  if (!ids.length) {
    const hint = document.createElement('div');
    hint.className = 'excluded-picker-empty';
    hint.textContent = t('模型列表尚未同步，先前往模型页同步后再勾选');
    list.appendChild(hint);
    return;
  }
  visible.forEach(id => {
    const label = document.createElement('label');
    label.className = 'excluded-picker-item';
    const cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.checked = current.has(id.toLowerCase());
    cb.addEventListener('change', () => {
      const parts = (_('tunExcluded').value || '').split(',').map(s => s.trim()).filter(Boolean);
      const idxLower = parts.map(s => s.toLowerCase());
      const i = idxLower.indexOf(id.toLowerCase());
      if (cb.checked && i === -1) parts.push(id);
      if (!cb.checked && i !== -1) parts.splice(i, 1);
      _('tunExcluded').value = parts.join(', ');
      updateExcludedMatchCounts();
      refreshSaveBar();
    });
    label.appendChild(cb);
    label.appendChild(document.createTextNode(' ' + id));
    list.appendChild(label);
  });
  if (visible.length < ids.filter(id => !q || id.toLowerCase().indexOf(q) !== -1).length) {
    const more = document.createElement('div');
    more.className = 'excluded-picker-empty';
    more.textContent = t('仅显示前 200 项，请缩小搜索范围');
    list.appendChild(more);
  }
}

// ========== 调优参数：脏检查基线 + 变更预览 ==========
function collectTuningPayload() {
  return {
    retry: {
      maxRounds: tuningNum('tunRetryRounds'),
      maxCredentialsPerRound: tuningNum('tunRetryCreds'),
      maxIntervalSeconds: tuningNum('tunRetryInterval'),
      transientCooldownSeconds: tuningNum('tunRetryCooldown'),
      maxFailoverModels: tuningNum('tunRetryFailover'),
    },
    stream: {
      keepaliveSeconds: tuningNum('tunKeepalive'),
      bootstrapRetries: tuningNum('tunBootstrapRetries'),
      firstContentTimeoutSeconds: tuningNum('tunFirstContentTimeout'),
      suppressKeepaliveBeforeFirstToken: _('tunSuppressKeepalive').value === 'true',
    },
    sessionAffinity: {
      enabled: _('tunAffinityEnabled').value === 'true',
      ttlSeconds: tuningNum('tunAffinityTTL'),
    },
    maxRequestBodyBytes: tuningNum('tunMaxBodyMB') * 1048576,
    passthroughHeaders: _('tunPassthrough').value === 'true',
    nonstreamKeepaliveSeconds: tuningNum('tunNonstreamKeepalive'),
    modelAliases: readAliasRows(),
    excludedModels: (_('tunExcluded').value || '').split(',').map(s => s.trim()).filter(Boolean),
    requestErrorRules: readErrorRuleRows(),
  };
}
let _tunBaseJson = '';
let _tunBaseCfg = null;
function tuningDirty() { return _tunBaseJson !== '' && JSON.stringify(collectTuningPayload()) !== _tunBaseJson; }
function discardTuning() { if (_tunBaseCfg) fillTuning(_tunBaseCfg); }
// 扁平化为「点号字段 -> 字符串值」，供 diffFlat 比较（数组/对象统一 fmtDiffVal 序列化）
function flattenTuning(obj, prefix, out) {
  Object.keys(obj || {}).forEach(k => {
    const v = obj[k];
    if (v && typeof v === 'object' && !Array.isArray(v)) flattenTuning(v, prefix + k + '.', out);
    else out[prefix + k] = fmtDiffVal(v);
  });
  return out;
}
const TUN_FIELD_LABELS = {
  'retry.maxRounds': '最大回退轮次',
  'retry.maxCredentialsPerRound': '每轮账号上限',
  'retry.maxIntervalSeconds': '轮间隔上限(秒)',
  'retry.transientCooldownSeconds': '瞬时错误冷却(秒)',
  'retry.maxFailoverModels': '故障转移模型上限',
  'stream.keepaliveSeconds': 'SSE 心跳间隔(秒)',
  'stream.bootstrapRetries': '首包前重试次数',
  'stream.firstContentTimeoutSeconds': '首内容超时(秒)',
  'stream.suppressKeepaliveBeforeFirstToken': '首 token 前抑制心跳',
  'sessionAffinity.enabled': '会话亲和',
  'sessionAffinity.ttlSeconds': '亲和绑定 TTL(秒)',
  'maxRequestBodyBytes': '请求体上限(MB)',
  'passthroughHeaders': '透传上游响应头',
  'nonstreamKeepaliveSeconds': '非流式占位心跳(秒)',
  modelAliases: '模型别名',
  excludedModels: '排除模型',
  requestErrorRules: '错误规则',
};
function tuningDiff() {
  if (_tunBaseJson === '') return [];
  return diffFlat(flattenTuning(JSON.parse(_tunBaseJson), '', {}), flattenTuning(collectTuningPayload(), '', {}))
    .map(ch => {
      let from = ch.from, to = ch.to;
      if (ch.label === 'maxRequestBodyBytes') {
        const mb = v => v === '' ? '' : (parseInt(v, 10) / 1048576) + ' MB';
        from = mb(from); to = mb(to);
      }
      return { label: t(TUN_FIELD_LABELS[ch.label] || ch.label), from: from, to: to };
    });
}

async function saveTuning(btn) {
  const dup = document.querySelector('#tunAliasesEditor .se-alias.dup');
  if (dup) { toast(t('存在重复别名，请先修正'), 'error'); return; }
  const payload = collectTuningPayload();
  const changes = tuningDiff();
  if (changes.length && !(await showDiffModal(changes))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    await api('POST', '/config/update', payload);
    toast(t('调优参数已保存'), 'success');
    await loadConfig();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

// 用 GET /config 的策略字段回填调优表单；0/缺省显示为占位符（=内置默认）
function fillTuning(c) {
  const num = (id, v) => { const el = _(id); if (el) el.value = v ? String(v) : ''; };
  const r = c.retry || {}, s = c.stream || {}, a = c.sessionAffinity || {};
  num('tunRetryRounds', r.maxRounds);
  num('tunRetryCreds', r.maxCredentialsPerRound);
  num('tunRetryInterval', r.maxIntervalSeconds);
  const cooldown = _('tunRetryCooldown');
  if (cooldown) cooldown.value = (r.transientCooldownSeconds === undefined || r.transientCooldownSeconds === 0) ? '' : String(r.transientCooldownSeconds);
  num('tunRetryFailover', r.maxFailoverModels);
  const ka = _('tunKeepalive');
  if (ka) ka.value = (s.keepaliveSeconds === undefined || s.keepaliveSeconds === 0) ? '' : String(s.keepaliveSeconds);
  num('tunBootstrapRetries', s.bootstrapRetries);
  num('tunFirstContentTimeout', s.firstContentTimeoutSeconds);
  num('tunNonstreamKeepalive', c.nonstreamKeepaliveSeconds);
  if (_('tunSuppressKeepalive')) _('tunSuppressKeepalive').value = String(!!s.suppressKeepaliveBeforeFirstToken);
  if (_('tunAffinityEnabled')) _('tunAffinityEnabled').value = String(!!a.enabled);
  num('tunAffinityTTL', a.ttlSeconds);
  if (_('tunMaxBodyMB')) _('tunMaxBodyMB').value = c.maxRequestBodyBytes ? String(Math.round(c.maxRequestBodyBytes / 1048576)) : '';
  if (_('tunPassthrough')) _('tunPassthrough').value = String(!!c.passthroughHeaders);
  const aliasBox = _('tunAliasesEditor');
  if (aliasBox) {
    aliasBox.innerHTML = '';
    (c.modelAliases || []).forEach(al => addAliasRow(al.alias, al.target, al.provider));
  }
  const ruleBox = _('tunErrorRulesEditor');
  if (ruleBox) {
    ruleBox.innerHTML = '';
    (c.requestErrorRules || []).forEach(rl => addErrorRuleRow(rl.statusCodes, rl.match, rl.action, rl.regex));
  }
  if (_('tunExcluded')) _('tunExcluded').value = (c.excludedModels || []).join(', ');
  updateExcludedMatchCounts();
  // 回填完成 → 以此为脏检查基线；注册保存条（host=路由页可见）
  _tunBaseJson = JSON.stringify(collectTuningPayload());
  _tunBaseCfg = c;
  setSaveBarCtx('tuning', {
    host: () => { const el = document.querySelector('.nav-item.active'); return !!el && el.dataset.tab === 'routing'; },
    dirty: tuningDirty,
    diff: tuningDiff,
    discard: discardTuning,
    save: () => saveTuning(null),
  });
}

// 保存监听地址：保存后程序自动重启监听（立即生效）
async function saveListenHost() {
  const host = _('settingListenHost').value;
  if (!host) return;
  try {
    const d = await api('POST', '/config/update', { host });
    const safe = ['127.0.0.1', 'localhost', '::1', '0.0.0.0'].indexOf(host) !== -1;
    if (safe) {
      toast(t('已保存，正在重启监听...'), 'success');
      setTimeout(() => location.reload(), 1500);
    } else {
      toast(t('监听已切换，请通过 ') + (d.data.address || host) + t(' 访问管理后台'), 'success');
    }
    await loadConfig();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
}

// 保存/清除管理后台密码（留空 = 清除）
async function savePassword(btn) {
  const pwd = _('settingPassword').value;
  if (pwd && pwd.length < 4) { toast(t('密码至少 4 位'), 'error'); return; }
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    await api('POST', '/password', { password: pwd });
    _('settingPassword').value = '';
    toast(pwd ? t('密码已设置，后台需要重新登录') : t('已清除密码'), 'success');
    await loadConfig();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

// 清除全部后台登录会话（含当前浏览器）：共享机器上强制重新登录，不动凭据数据
async function clearAdminSessions(btn) {
  if (!(await showConfirm(t('确认清除全部后台登录会话？所有浏览器（包括当前页面）都需要重新登录。')))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>'; }
  try {
    await api('POST', '/admin-sessions/clear');
    toast(t('已清除全部登录会话'), 'success');
    setTimeout(() => location.reload(), 400); // 当前会话同被作废，重载后进入登录页
  } catch (e) {
    toast(t('操作失败: ') + e.message, 'error');
    if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  }
}

function addHeaderRow() {
  const tbody = _('headersTableBody');
  const tr = document.createElement('tr');
  tr.innerHTML =
    '<td><input type="text" class="header-key" placeholder="Header-Name" style="font-size:12px;font-family:ui-monospace,monospace"></td>' +
    '<td><input type="text" class="header-val" placeholder="value" style="font-size:12px;font-family:ui-monospace,monospace"></td>' +
    '<td><button class="btn btn-sm btn-danger" onclick="this.closest(\'tr\').remove()" title="' + t('移除') + '" aria-label="' + t('移除') + '">✕</button></td>';
  tbody.appendChild(tr);
}

// 批量编辑：把表格转成 textarea，一行一个 Key: Value
async function bulkEditHeaders() {
  const tbody = _('headersTableBody');
  const rows = tbody.querySelectorAll('tr');
  const lines = [];
  rows.forEach(tr => {
    const k = tr.querySelector('.header-key'), v = tr.querySelector('.header-val');
    if (k && k.value.trim()) lines.push(k.value.trim() + ': ' + (v ? v.value.trim() : ''));
  });
  const box = document.createElement('div');
  box.innerHTML =
    '<div class="field" style="margin-top:10px"><label>' + t('批量编辑（每行 Key: Value）') + '</label>' +
    '<textarea id="headersBulk" rows="10" style="width:100%;font-family:ui-monospace,monospace;font-size:12px;border:1px solid var(--border2);border-radius:8px;padding:8px;background:var(--surface);color:var(--text)">' + esc(lines.join('\n')) + '</textarea></div>' +
    '<div class="form-actions" style="margin-top:8px"><button class="btn btn-sm btn-primary" onclick="applyBulkHeaders()">' + t('应用并保存') + '</button>' +
    '<button class="btn btn-sm" onclick="loadConfig()">' + t('取消') + '</button></div>';
  tbody.parentElement.parentElement.querySelector('.form-actions').style.display = 'none';
  tbody.parentElement.parentElement.appendChild(box);
}

async function applyBulkHeaders() {
  const text = _('headersBulk').value;
  const headers = {};
  text.split('\n').forEach(line => {
    const idx = line.indexOf(':');
    if (idx <= 0) return;
    const k = line.slice(0, idx).trim();
    const v = line.slice(idx + 1).trim();
    if (k) headers[k] = v;
  });
  try {
    await api('POST', '/config/update', { headers });
    toast(t('请求头已保存') + ' (' + Object.keys(headers).length + ')', 'success');
    loadConfig();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
}

// ========== opencode 请求头 ==========
function parseHeaderText(text) {
  const out = {};
  text.split('\n').forEach(line => {
    const idx = line.indexOf(':');
    if (idx <= 0) return;
    const k = line.slice(0, idx).trim();
    const v = line.slice(idx + 1).trim();
    if (k) out[k] = v;
  });
  return out;
}

async function loadZenHeaders() {
  try {
    const d = await api('GET', '/opencode/config');
    const c = d.data || {};
    const hdrs = c.zenHeaders || {};
    _('zenHeadersBulk').value = Object.entries(hdrs).map(([k, v]) => k + ': ' + v).join('\n');
  } catch (e) { toast(t('加载失败') + ': ' + e.message, 'error'); }
}

async function saveZenHeaders(btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    await api('POST', '/opencode/config/update', { zenHeaders: parseHeaderText(_('zenHeadersBulk').value) });
    toast(t('opencode 请求头已保存'), 'success');
    _('zenHeaderSaveResult').innerHTML = '<div style="color:var(--green);font-size:13px">✓ ' + t('已保存') + '</div>';
    setTimeout(() => _('zenHeaderSaveResult').innerHTML = '', 4000);
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}


// ========== 版本更新检查（关于页；服务端缓存 6h，?force=1 强制刷新） ==========
async function checkForUpdate(force, btn) {
  const el = _('aboutVerCheck');
  if (!el) return;
  if (btn) btn.disabled = true;
  el.textContent = t('检查中...');
  el.style.color = 'var(--text3)';
  try {
    const d = await api('GET', '/version-check' + (force ? '?force=1' : ''));
    const v = d.data || {};
    el.textContent = '';
    const span = document.createElement('span');
    if (v.error && !v.latest) {
      span.textContent = t('检查失败') + ': ' + v.error;
      el.style.color = 'var(--red)';
    } else if (v.updateAvailable) {
      span.textContent = t('发现新版本 ') + v.latest + '（' + t('当前 ') + (v.current || '-') + '）';
      el.style.color = 'var(--yellow)';
    } else if (!/^v?\d/.test(v.current || '')) {
      span.textContent = t('开发版本') + (v.latest ? ' · ' + t('最新 ') + v.latest : '');
      el.style.color = 'var(--text2)';
    } else {
      span.textContent = t('已是最新版本') + ' · ' + v.current;
      el.style.color = 'var(--green)';
    }
    el.appendChild(span);
    if (v.updateAvailable && v.releaseURL) {
      const a = document.createElement('a');
      a.href = '#';
      a.textContent = t('下载更新');
      a.style.cssText = 'color:var(--accent);cursor:pointer;margin-left:8px;font-size:12px';
      a.onclick = function () { openExternal(v.releaseURL); return false; };
      el.appendChild(document.createTextNode(' '));
      el.appendChild(a);
    }
    _verCheckDone = true;
  } catch (e) {
    el.textContent = t('检查失败') + ': ' + e.message;
    el.style.color = 'var(--red)';
  } finally { if (btn) btn.disabled = false; }
}
