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

function parseAliasLines(text) {
  const out = [];
  (text || '').split('\n').forEach(line => {
    line = line.trim();
    if (!line) return;
    const idx = line.indexOf('->');
    if (idx < 0) return;
    let target = line.slice(idx + 2).trim();
    let provider = '';
    const at = target.lastIndexOf(' @');
    if (at >= 0) { provider = target.slice(at + 2).trim(); target = target.slice(0, at).trim(); }
    out.push({ alias: line.slice(0, idx).trim(), target: target, provider: provider });
  });
  return out;
}

function parseRuleLines(text) {
  const out = [];
  (text || '').split('\n').forEach(line => {
    line = line.trim();
    if (!line) return;
    const rule = { statusCodes: [], match: '', action: '', regex: false };
    let known = false;
    line.split(';').forEach(token => {
      token = token.trim();
      if (!token) return;
      const eq = token.indexOf('=');
      if (eq < 0) return;
      const key = token.slice(0, eq).trim().toLowerCase();
      const val = token.slice(eq + 1).trim();
      if (key === 'codes') {
        rule.statusCodes = val.split(',').map(s => parseInt(s.trim(), 10)).filter(n => !isNaN(n));
        known = true;
      } else if (key === 'match') {
        rule.match = val;
        known = true;
      } else if (key === 'action') {
        rule.action = val;
        known = true;
      } else if (key === 'regex') {
        rule.regex = val === '1' || val.toLowerCase() === 'true';
        known = true;
      }
    });
    if (known) out.push(rule);
  });
  return out;
}

async function saveTuning(btn) {
  const payload = {
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
    modelAliases: parseAliasLines(_('tunAliases').value),
    excludedModels: (_('tunExcluded').value || '').split(',').map(s => s.trim()).filter(Boolean),
    requestErrorRules: parseRuleLines(_('tunErrorRules').value),
  };
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
  if (_('tunAliases')) _('tunAliases').value = (c.modelAliases || []).map(al =>
    al.alias + ' -> ' + al.target + (al.provider ? ' @' + al.provider : '')).join('\n');
  if (_('tunExcluded')) _('tunExcluded').value = (c.excludedModels || []).join(', ');
  if (_('tunErrorRules')) _('tunErrorRules').value = (c.requestErrorRules || []).map(rl => {
    const parts = [];
    if (rl.statusCodes && rl.statusCodes.length) parts.push('codes=' + rl.statusCodes.join(','));
    if (rl.match) parts.push('match=' + rl.match);
    if (rl.action && rl.action !== 'continue') parts.push('action=' + rl.action);
    if (rl.regex) parts.push('regex=1');
    return parts.join('; ');
  }).join('\n');
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

