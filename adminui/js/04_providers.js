// ========== 自定义 Provider ==========
let _editingProviderId = '';
// 从免费类预设新建并保存后，自动触发一次分级导入预览（用户勾选后才写入）
let _previewAfterSave = false;

async function loadProviders() {
  try {
    const [d, sd] = await Promise.all([
      api('GET', '/providers'),
      api('GET', '/providers/sync-status').catch(() => ({ data: {} })),
    ]);
    _provSync = (sd && sd.data && sd.data.providers) || {};
    const list = d.data.providers || [];
    const box = _('providersList');
    renderProviderSyncBanner(list);
    if (!list.length) { box.innerHTML = listEmptyState(ICON_PROVIDERS, t('暂无自定义 Provider')); return; }
    box.innerHTML = '<table class="action-table"><thead><tr><th>' + t('名称') + '</th><th>Base URL</th><th>' + t('模型') + '</th><th class="num-col">' + t('优先级') + '</th><th>' + t('状态') + '</th><th>' + t('最近检测') + '</th><th>' + t('操作') + '</th></tr></thead><tbody>' +
      list.map(p =>
        '<tr>' +
          '<td>' + esc(p.name) + (p.free ? ' <span style="color:var(--green);font-size:11px">free</span>' : '') + (function () { const n = (p.connectionIds || []).length || (p.connectionId ? 1 : 0); return n ? ' <span style="color:var(--accent);font-size:11px" title="' + t('连接池轮转派发') + '">token×' + n + '</span>' : ''; })() + '</td>' +
          '<td class="mono" style="font-size:11px;max-width:220px;overflow:hidden;text-overflow:ellipsis">' + esc(p.baseURL) + '</td>' +
          '<td style="font-size:11px;max-width:200px;overflow:hidden;text-overflow:ellipsis">' + esc((p.modelIds || []).join(', ')) + '</td>' +
          '<td class="num-col">' + p.priority + '</td>' +
          '<td>' + (p.enabled
            ? '<span class="status active"><span class="status-dot active"></span>' + t('已启用') + '</span>'
            : '<span class="status disabled"><span class="status-dot disabled"></span>' + t('已停用') + '</span>') + '</td>' +
          '<td style="white-space:nowrap">' + (p.enabled ? provSyncCellHTML(p.id) : '<span style="font-size:11px;color:var(--text3)">—</span>') + '</td>' +
          '<td><button class="btn btn-sm" onclick="editProvider(\'' + p.id + '\')">' + t('编辑') + '</button> ' +
              '<button class="btn btn-sm" onclick=\'testProviderById("' + p.id + '", this)\'>' + t('测试') + '</button> ' +
              '<span class="prov-row-test-out"></span> ' +
              '<button class="btn btn-sm" onclick=\'recheckProvider("' + p.id + '", this)\'>' + t('重新检测') + '</button> ' +
              '<span class="prov-row-recheck-out"></span> ' +
              '<button class="btn btn-sm" onclick=\'toggleProvider("' + p.id + '", ' + !p.enabled + ', this)\'>' + (p.enabled ? t('停用') : t('启用')) + '</button> ' +
              '<button class="btn btn-sm btn-danger" onclick=\'deleteProvider("' + p.id + '")\' title="' + t('删除') + '" aria-label="' + t('删除') + '">✕</button></td>' +
        '</tr>'
      ).join('') + '</tbody></table>';
  } catch (e) {
    const box = _('providersList');
    if (box) box.innerHTML = '<div class="empty" style="padding:10px;color:var(--text3)">' + t('加载失败') + ': ' + esc(e.message) + '</div>';
  }
}

// ========== §W3-C 上游自动检测：列表状态列 + 待审横幅 ==========
let _provSync = {};

function fmtAgo(ms) {
  if (!ms) return '';
  const s = Math.max(0, Math.floor((Date.now() - ms) / 1000));
  if (s < 60) return t('刚刚');
  if (s < 3600) return Math.floor(s / 60) + ' ' + t('分钟前');
  if (s < 86400) return Math.floor(s / 3600) + ' ' + t('小时前');
  return Math.floor(s / 86400) + ' ' + t('天前');
}

function provSyncCellHTML(id) {
  const st = _provSync[id] || {};
  const when = st.lastCheckedMs
    ? '<span style="font-size:11px;color:var(--text3)" title="' + esc(new Date(st.lastCheckedMs).toLocaleString(LC())) + '">' + fmtAgo(st.lastCheckedMs) + '</span>'
    : '<span style="font-size:11px;color:var(--text3)">' + t('未检测') + '</span>';
  const err = st.lastError
    ? ' <span style="font-size:11px;color:var(--red)" title="' + esc(st.lastError) + '">✗</span>' : '';
  let flow = '';
  if (st.newQueued && st.batchId) flow += ' <span class="a-link" style="font-size:11px" onclick="openAutoBatch(\'' + esc(st.batchId) + '\')">+' + st.newQueued + ' ' + t('待审') + '</span>';
  if (st.delistedMarked) flow += ' <span style="font-size:11px;color:var(--text3)" title="' + t('已下架') + '">' + st.delistedMarked + ' ↓</span>';
  return when + err + flow;
}

async function recheckProvider(id, btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('检测中...'); }
  try {
    const d = await api('POST', '/providers/recheck', { id: id });
    const st = d.data || {};
    const out = btn ? btn.parentNode.querySelector('.prov-row-recheck-out') : null;
    if (st.lastError && !st.lastCheckedMs) {
      if (out) out.innerHTML = renderTestResultHTML(false, friendlyTestError(new Error(st.lastError)));
    } else {
      const bits = [];
      if (st.newQueued) bits.push('+' + st.newQueued + ' ' + t('待审'));
      if (st.delistedMarked) bits.push(st.delistedMarked + ' ' + t('已下架'));
      if (out) out.innerHTML = renderTestResultHTML(true, bits.length ? bits.join(' · ') : t('无变化'));
    }
    loadProviders();
  } catch (e) {
    toast(t('检测失败') + ': ' + e.message, 'error');
    if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  }
}

function renderProviderSyncBanner(list) {
  const box = _('providerSyncBanner');
  if (!box) return;
  let delisted = 0, queued = 0;
  (list || []).forEach(p => {
    const st = _provSync[p.id] || {};
    delisted += st.delistedMarked || 0;
    if (st.newQueued && st.batchId) queued += st.newQueued;
  });
  if (!queued && !delisted) { box.style.display = 'none'; box.innerHTML = ''; return; }
  box.style.display = 'flex';
  box.innerHTML = '<span style="font-size:12px;color:var(--text2)">' +
    (queued ? '<span style="color:var(--accent);font-weight:600">' + queued + t('个新模型待审') + '</span>' : '') +
    (queued && delisted ? ' · ' : '') +
    (delisted ? '<span style="color:var(--text3)">' + delisted + ' ' + t('已下架') + '</span>' : '') +
    '</span>' +
    '<span style="font-size:11px;color:var(--text3);margin-left:8px">' + t('后台每 6 小时自动检测已启用 Provider 的上游模型清单') + '</span>' +
    '<span style="margin-left:auto;display:flex;gap:8px">' +
    (queued ? '<button class="btn btn-sm btn-primary" onclick="openNewestAutoBatch()">' + t('查看审阅') + '</button>' +
             '<button class="btn btn-sm" onclick="discardAutoBatches(this)">' + t('全部忽略') + '</button>' : '') +
    '</span>';
}

async function openNewestAutoBatch() {
  try {
    const d = await api('GET', '/models/import/pending');
    const autos = ((d.data || {}).batches || []).filter(b => b.auto);
    if (!autos.length) { toast(t('没有可导入的模型'), 'info'); return; }
    openAutoBatch(autos[0].id);
  } catch (e) { toast(t('加载失败') + ': ' + esc(e.message), 'error'); }
}

async function openAutoBatch(batchId) {
  try {
    const d = await api('GET', '/models/import/batch?id=' + encodeURIComponent(batchId));
    const r = d.data || {};
    if (!r.batch) { toast(t('批次不存在或已过期'), 'info'); loadProviders(); return; }
    _importBatch = r.batch;
    renderImportPreviewList();
    _('importPreviewModal').style.display = 'flex';
  } catch (e) { toast(t('加载失败') + ': ' + esc(e.message), 'error'); }
}

async function discardAutoBatches(btn) {
  if (!(await showConfirm(t('确定忽略全部待审的新模型批次？')))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; }
  try {
    const d = await api('GET', '/models/import/pending');
    const autos = ((d.data || {}).batches || []).filter(b => b.auto);
    for (const b of autos) { await api('POST', '/models/import/discard', { batchId: b.id }); }
    toast(t('已忽略全部待审批次'), 'success');
    loadProviders();
  } catch (e) { toast(t('忽略失败') + ': ' + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

// toggleProvider 快速切换 provider 启停（不打开编辑表单）：拉取最新配置，
// 翻转 enabled 后原样回存，其余字段不动。停用即时退出路由与模型同步。
async function toggleProvider(id, enable, btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    const d = await api('GET', '/providers');
    const p = (d.data.providers || []).find(x => x.id === id);
    if (!p) { toast(t('Provider 未找到'), 'error'); return; }
    p.enabled = !!enable;
    p.modelIds = p.modelIds || [];
    await api('POST', '/providers/save', p);
    toast(t(enable ? '已启用' : '已停用') + ': ' + p.name, 'success');
    loadProviders();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

function showProviderEditor(p) {
  _editingProviderId = p ? p.id : '';
  _previewAfterSave = false;
  const adv = _('provModelsAdv');
  if (adv) adv.style.display = 'none';
  const title = _('providerEditorTitle');
  if (title) title.textContent = p ? t('编辑 Provider') : t('添加 Provider');
  _('provName').value = p ? p.name : '';
  _('provBaseURL').value = p ? p.baseURL : '';
  _('provKey').value = p ? (p.apiKey || '') : '';
  _('provPriority').value = p ? p.priority : 10;
  _('provTimeout').value = p ? (p.timeoutSec || 300) : 300;
  _('provRpmLimit').value = p ? (p.rpmLimit || 0) : 0;
  _('provDailyLimit').value = p ? (p.dailyRequestLimit || 0) : 0;
  _('provModels').value = p ? (p.modelIds || []).join(', ') : '';
  _('provHeaders').value = p ? Object.entries(p.headers || {}).map(([k, v]) => k + ': ' + v).join('\n') : '';
  _('provEnabled').value = p ? String(p.enabled !== false) : 'true';
  // 手动新建默认付费：整源标 free 会把付费模型误标为免费（审计 §9.2）；free 来源应由预设/导入按模型判定
  _('provFree').value = p ? String(!!p.free) : 'false';
  initConnPick(p ? (p.connectionIds && p.connectionIds.length ? p.connectionIds : (p.connectionId ? [p.connectionId] : [])) : []);
  if (!_connList.length) loadConnections();
  const testOut = _('provTestResult');
  if (testOut) testOut.innerHTML = '';
  renderProvModelChips();
  openDrawer('providerEditor');
}

function hideProviderEditor() { closeDrawer('providerEditor'); }

// 模型 ID 输入实时回显为芯片（真值仍是输入框文本，保存逻辑不变）
function renderProvModelChips() {
  const box = _('provModelsChips');
  const ids = _('provModels').value.split(',').map(s => s.trim()).filter(Boolean);
  const cnt = _('provModelsCount');
  if (cnt) cnt.textContent = ' · ' + (ids.length ? ids.length + t(' 个模型') : t('未选择'));
  if (!box) return;
  if (!ids.length) { box.style.display = 'none'; box.innerHTML = ''; return; }
  box.style.display = '';
  box.innerHTML = ids.map(id => '<span class="model-tag mono">' + esc(id) +
    ' <a href="javascript:void(0)" onclick="removeProvModel(\'' + escAttr(id) + '\')" title="' + t('移除') + '" aria-label="' + t('移除') + '" style="color:var(--text3);text-decoration:none">×</a></span>').join('');
}

function removeProvModel(id) {
  const kept = _('provModels').value.split(',').map(s => s.trim()).filter(Boolean).filter(x => x !== id);
  _('provModels').value = kept.join(', ');
  renderProvModelChips();
}

function toggleProvModelsAdv() {
  const d = _('provModelsAdv');
  if (!d) return;
  const open = d.style.display !== 'none';
  d.style.display = open ? 'none' : '';
  if (!open) { try { _('provModels').focus(); } catch (e) {} }
}

async function saveProvider(btn) {
  const payload = {
    id: _editingProviderId,
    name: _('provName').value.trim(),
    baseURL: _('provBaseURL').value.trim(),
    apiKey: _('provKey').value.trim(),
    priority: parseInt(_('provPriority').value, 10) || 10,
    timeoutSec: parseInt(_('provTimeout').value, 10) || 300,
    rpmLimit: parseInt(_('provRpmLimit').value, 10) || 0,
    dailyRequestLimit: parseInt(_('provDailyLimit').value, 10) || 0,
    modelIds: _('provModels').value.split(',').map(s => s.trim()).filter(Boolean),
    headers: parseHeaderText(_('provHeaders').value),
    enabled: _('provEnabled').value === 'true',
    free: _('provFree').value === 'true',
    connectionIds: _provConnSel.slice(),
    connectionId: '',
  };
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  const isNew = !payload.id;
  try {
    const d = await api('POST', '/providers/save', payload);
    toast(t('Provider 已保存'), 'success');
    hideProviderEditor();
    loadProviders();
    // 从免费/免密钥预设新建后，自动跑一次分级预览，弹出勾选导入（仍受控，不静默写入）
    if (isNew && _previewAfterSave) {
      const newId = d && d.data && d.data.provider && d.data.provider.id;
      if (newId) {
        _previewAfterSave = false;
        try {
          const pd = await api('POST', '/models/import/preview', { id: newId });
          const pr = pd.data || {};
          if (!pr.error && pr.batch) {
            _importBatch = pr.batch;
            renderImportPreviewList();
            _('importPreviewModal').style.display = 'flex';
          }
        } catch (e) {}
      }
    }
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

function editProvider(id) {
  api('GET', '/providers').then(d => {
    const p = (d.data.providers || []).find(x => x.id === id);
    if (p) showProviderEditor(p);
  });
}

async function deleteProvider(id) {
  if (!(await showConfirm(t('确定删除该 Provider？')))) return;
  try {
    await api('POST', '/providers/delete', { id });
    toast(t('已删除'), 'success');
    loadProviders();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

// 内联测试结果统一渲染：绿 ✓ + 摘要 / 红 ✗ + 原因（一律就地展示，不弹 toast-only）
function renderTestResultHTML(ok, text) {
  return ok
    ? '<span style="color:var(--green)">✓ ' + esc(text) + '</span>'
    : '<span style="color:var(--red)">✗ ' + esc(text) + '</span>';
}

// 老接口/网关异常的降级文案：404「page not found」/ 非 JSON 响应（旧版本进程）→ 提示重启代理；
// 其余业务错误（如「无启用条目」）原样透出。
function friendlyTestError(e) {
  const msg = (e && e.message || '').toString();
  if (!msg || /unexpected token|json|404|not found|not allowed/i.test(msg)) {
    return t('接口不可用，请重启代理更新到新版本后再试');
  }
  return t('测试失败: ') + msg;
}

// 列表行「测试」：结果写入所在行 td 的 .prov-row-test-out（此前写到编辑器内的结果位，编辑器关闭时不可见）
async function testProviderById(id, cell) {
  const out = cell && cell.closest ? cell.closest('td').querySelector('.prov-row-test-out') : null;
  const orig = cell ? cell.innerHTML : '';
  if (cell) { cell.disabled = true; cell.innerHTML = '<span class="loading"></span> ' + t('测试中...'); }
  if (out) out.innerHTML = '';
  try {
    const d = await api('POST', '/providers/test', { id });
    const r = d.data || {};
    if (out) out.innerHTML = renderTestResultHTML(r.ok, r.ok ? (r.durationMs + 'ms') : (r.error || t('失败')));
  } catch (e) {
    if (out) out.innerHTML = renderTestResultHTML(false, friendlyTestError(e));
  } finally {
    if (cell) { cell.disabled = false; cell.innerHTML = orig; }
  }
}

// 编辑器内「测试连通性」：已有 Provider → {id, model?}；新建（未保存）→ 直接用
// 表单的未保存值（baseURL/apiKey/headers），不再「先保存再按名称匹配」。
// 结果一律内联展示在按钮旁的 #provTestResult。
async function testProvider(btn) {
  const out = _('provTestResult');
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('测试中...'); }
  if (out) out.innerHTML = '<span style="color:var(--text3)"><span class="loading"></span> ' + t('测试中...') + '</span>';
  // 表单里的「模型 ID」是逗号分隔列表，探测取第一个（显式给出的模型按原样使用）
  const model = _('provModels').value.split(',').map(s => s.trim()).filter(Boolean)[0] || '';
  const body = _editingProviderId
    ? { id: _editingProviderId, model: model }
    : { baseURL: _('provBaseURL').value.trim(), apiKey: _('provKey').value.trim(), headers: parseHeaderText(_('provHeaders').value), model: model };
  try {
    const d = await api('POST', '/providers/test', body);
    const r = d.data || {};
    if (out) out.innerHTML = renderTestResultHTML(r.ok, r.ok ? (r.durationMs + 'ms' + (r.model ? ' · ' + r.model : '')) : (r.error || t('失败')));
  } catch (e) {
    if (out) out.innerHTML = renderTestResultHTML(false, friendlyTestError(e));
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  }
}

// ========== Provider：模型多选弹窗（免费/付费分组 + §9.3 分级信号） ==========
let _provPickerModels = [];   // 上游返回的全部模型 id（服务端已去重排序，保留原顺序）
let _provPickerTiers = {};    // id → 'free' | 其他（来自分级预览的有序信号判定）
let _provPickerOrder = [];    // 勾选顺序（确认后即追加顺序）
let _provPickerExisting = {}; // 编辑器中已有的模型 id（预勾选且禁用，确认时跳过）

// 已有 Provider 用 {id}（服务端补全 baseURL/key/headers）；新建直接传表单未保存值
function provEditorPayload() {
  return _editingProviderId
    ? { id: _editingProviderId }
    : { baseURL: _('provBaseURL').value.trim(), apiKey: _('provKey').value.trim(), headers: parseHeaderText(_('provHeaders').value) };
}

async function fetchProviderModels(btn) {
  const payload = provEditorPayload();
  if (!_editingProviderId && !payload.baseURL) { toast(t('请先填写 Base URL'), 'info'); return; }
  if (!_editingProviderId) {
    payload.free = _('provFree').value === 'true';
    payload.name = _('provName').value.trim();
  }
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('拉取中...'); }
  const out = _('provTestResult');
  if (out) out.innerHTML = '';
  try {
    // 分级预览端点一次给回 id + free/paid 判定（pricing/后缀/白名单/整源免费有序信号）
    const d = await api('POST', '/models/import/preview', payload);
    const r = d.data || {};
    const items = ((r.batch && r.batch.items) || []).filter(it => it && it.modelId);
    if (!items.length) {
      // 上游失败（HTTP 200 + {error}）：不是 API 错误，就地提示排查 Base URL / Key
      if (out) out.innerHTML = renderTestResultHTML(false,
        t('未获取到模型：请先检查 Base URL 与 API Key') + (r.error ? ' — ' + r.error : ''));
      return;
    }
    _provPickerModels = Array.from(new Set(items.map(it => it.modelId)));
    _provPickerTiers = {};
    items.forEach(it => { _provPickerTiers[it.modelId] = it.tier; });
    _provPickerOrder = [];
    _provPickerExisting = {};
    _('provModels').value.split(',').map(s => s.trim()).filter(Boolean).forEach(id => { _provPickerExisting[id] = true; });
    _('provPickerSearch').value = '';
    renderProvPickerList();
    _('provPickerModal').style.display = 'flex';
    try { _('provPickerSearch').focus(); } catch (e) {}
  } catch (e) {
    if (out) out.innerHTML = renderTestResultHTML(false, friendlyTestError(e));
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  }
}

function closeProvModelPicker() {
  const m = _('provPickerModal');
  if (m) m.style.display = 'none';
  _provPickerOrder = [];
}

function provPickerVisible() {
  const q = (_('provPickerSearch').value || '').trim().toLowerCase();
  const rx = q.replace(/[.*+\-?^${}()|[\]\\]/g, '\\$&');
  return _provPickerModels.filter(id => !rx || id.toLowerCase().search(rx) !== -1);
}

function provPickerToggle(id, checked) {
  const idx = _provPickerOrder.indexOf(id);
  if (checked && idx === -1) _provPickerOrder.push(id);
  if (!checked && idx !== -1) _provPickerOrder.splice(idx, 1);
  renderProvPickerList();
}

// 工具条「全选」：把当前筛选结果里所有可勾选的模型补进勾选序列（已有的跳过，不可再选）
function provPickerSelectAllShown() {
  pickerMergeIds(_provPickerOrder, provPickerVisible().filter(id => !_provPickerExisting[id]));
  renderProvPickerList();
}

// 分组「全选」：只把当前筛选结果里该层级（free / 非 free）可勾选的模型补进序列
function provPickerSelectAllTier(tier) {
  pickerMergeIds(_provPickerOrder, provPickerVisible().filter(id =>
    ((tier === 'free') === (_provPickerTiers[id] === 'free')) && !_provPickerExisting[id]));
  renderProvPickerList();
}

function provPickerClear() { _provPickerOrder = []; renderProvPickerList(); }

function renderProvPickerList() {
  const box = _('provPickerList');
  if (!box) return;
  const scrollTop = box.scrollTop;
  const visible = provPickerVisible();
  if (!visible.length) {
    box.innerHTML = '<div class="empty">' + t('没有匹配的模型') + '</div>';
    renderProvPickerCount();
    return;
  }
  const row = id => {
    const rank = _provPickerOrder.indexOf(id);
    const used = !!_provPickerExisting[id];
    const checked = used || rank !== -1;
    return '<label class="combo-pick-row' + (used ? ' combo-pick-used' : '') + '">' +
      '<input type="checkbox"' + (checked ? ' checked' : '') + (used ? ' disabled' : '') + ' onchange="provPickerToggle(\'' + escAttr(id) + '\', this.checked)">' +
      '<span class="combo-pick-id">' + esc(id) + '</span>' +
      (used ? '<span class="model-source-badge" title="' + t('已在 Provider 中，无需重复添加') + '">' + t('已添加') + '</span>' : '') +
      (rank !== -1 ? '<span class="combo-pick-rank" title="' + t('勾选顺序即尝试优先级') + '">#' + (rank + 1) + '</span>' : '') +
    '</label>';
  };
  const groupTitle = (label, n, tier) =>
    '<div class="preset-group-title">' + label + ' · ' + n +
    '<button type="button" class="btn btn-sm" style="float:right;padding:1px 8px;font-size:11px" onclick="event.preventDefault();provPickerSelectAllTier(\'' + tier + '\')">' + t('全选') + '</button></div>';
  const freeIds = visible.filter(id => _provPickerTiers[id] === 'free');
  const paidIds = visible.filter(id => _provPickerTiers[id] !== 'free');
  box.innerHTML =
    (freeIds.length ? groupTitle(t('免费'), freeIds.length, 'free') + freeIds.map(row).join('') : '') +
    (paidIds.length ? groupTitle(t('付费'), paidIds.length, 'pass') + paidIds.map(row).join('') : '');
  box.scrollTop = scrollTop;
  renderProvPickerCount();
}

function renderProvPickerCount() {
  const el = _('provPickerCount');
  if (!el) return;
  const nFree = _provPickerOrder.filter(id => _provPickerTiers[id] === 'free').length;
  el.textContent = t('已选 ') + _provPickerOrder.length + ' / ' + _provPickerModels.length + t(' 个模型') +
    (nFree ? ' · ' + nFree + ' ' + t('免费') : '');
}

// 确认：写入「模型 ID」真值（隐藏的高级输入框），芯片即时回显
function provPickerConfirm() {
  if (!_provPickerOrder.length) { toast(t('请先勾选至少一个模型'), 'error'); return; }
  const cur = _('provModels').value.split(',').map(s => s.trim()).filter(Boolean);
  const seen = {};
  cur.forEach(id => { seen[id] = true; });
  let added = 0;
  for (const id of _provPickerOrder) {
    if (seen[id]) continue; // 防重复
    cur.push(id);
    seen[id] = true;
    added++;
  }
  closeProvModelPicker();
  if (added) {
    _('provModels').value = cur.join(', ');
    renderProvModelChips();
    toast(t('已按勾选顺序追加 ') + added + t(' 个模型'), 'success');
  } else {
    toast(t('所选模型均已在 Provider 中'), 'info');
  }
  try { _('provModelsGroup').scrollIntoView({ block: 'nearest' }); } catch (e) {}
}

// ========== 预设选择弹窗（/providers/presets，按接入成本分组） ==========
let _presetList = [];
let _presetFilter = ''; // ''=全部 | kind | 'login'（需登录）
const _presetKinds = [
  {kind:'free',     label:'免密钥直连', desc:'无需 API Key；应用后自动拉取模型',        badge:'免密钥',  cls:'model-source-badge custom'},
  {kind:'freeTier', label:'免费额度',   desc:'注册领取免费 API Key',                    badge:'免费额度', cls:'model-source-badge zen'},
  {kind:'hasFree',  label:'含免费模型', desc:'混合目录：需 Key，部分模型免费',           badge:'含免费',  cls:'model-source-badge provider'},
  {kind:'local',    label:'本地服务',   desc:'本机推理端点（LM Studio、llama.cpp 等）', badge:'本地',    cls:'model-source-badge'},
  {kind:'other',    label:'其他',       desc:'',                                       badge:'',       cls:'model-source-badge'},
];

async function loadProviderPresets() {
  try {
    if (!_presetList.length) {
      const d = await api('GET', '/providers/presets');
      _presetList = d.data.presets || [];
    }
    if (!_presetList.length) { toast(t('加载失败'), 'error'); return; }
    _('presetSearch').value = '';
    _presetFilter = '';
    renderPresetFilterChips();
    renderPresetPickerList();
    _('presetPickerModal').style.display = 'flex';
    try { _('presetSearch').focus(); } catch (e) {}
  } catch (e) {
    toast(t('加载失败') + ': ' + esc(e.message), 'error');
  }
}

function closePresetPicker() {
  _('presetPickerModal').style.display = 'none';
}

function renderPresetFilterChips() {
  const box = _('presetFilterChips');
  if (!box) return;
  const defs = [{ f: '', label: '全部' }]
    .concat(_presetKinds.map(g => ({ f: g.kind, label: g.label })))
    .concat([{ f: 'login', label: '需登录' }]);
  box.innerHTML = defs.map(d =>
    '<span class="chip chip-btn' + (_presetFilter === d.f ? ' active' : '') + '" onclick="setPresetFilter(\'' + d.f + '\')">' + t(d.label) + '</span>').join('');
}

function setPresetFilter(f) {
  _presetFilter = (_presetFilter === f && f !== '') ? '' : f;
  renderPresetFilterChips();
  renderPresetPickerList();
}

function presetRowHTML(p) {
  const g = _presetKinds.find(x => x.kind === (p.kind || 'other')) || _presetKinds[_presetKinds.length - 1];
  return '<div class="preset-row" onclick="applyPreset(\'' + esc(p.key) + '\')" title="' + esc(p.baseURL || '') + '">' +
    '<span class="preset-name">' + esc(p.name) + '</span>' +
    '<span class="preset-key">' + esc(p.baseURL || p.key) + '</span>' +
    '<span style="display:inline-flex;gap:6px;align-items:center;flex-shrink:0">' +
    (p.loginFlow ? '<span class="model-source-badge zen" title="' + t('设备码连接：在「账号与连接」添加，编辑器「账号连接池」勾选即可；无需 API Key') + '">' + t('需登录') + '</span>' : (g.badge ? '<span class="' + g.cls + '">' + t(g.badge) + '</span>' : '')) +
    (p.modelIDs && p.modelIDs.length ? '<span class="model-source-badge">' + p.modelIDs.length + ' ' + t('个模型') + '</span>' : '') +
    '</span></div>';
}

function renderPresetPickerList() {
  const q = (_('presetSearch').value || '').trim().toLowerCase();
  const list = _presetList.filter(p => {
    if (_presetFilter === 'login') { if (!p.loginFlow) return false; }
    else if (_presetFilter && (p.kind || 'other') !== _presetFilter) return false;
    return !q || p.key.toLowerCase().includes(q) || (p.name || '').toLowerCase().includes(q) || (p.baseURL || '').toLowerCase().includes(q);
  });
  _('presetCount').textContent = list.length + ' / ' + _presetList.length;
  if (!list.length) {
    _('presetPickerList').innerHTML = '<div class="empty">' + t('没有匹配的预设') + '</div>';
    return;
  }
  const byName = (a, b) => (a.name || '').localeCompare(b.name || '');
  if (_presetFilter === 'login') {
    _('presetPickerList').innerHTML = '<div class="preset-group-title">' + t('需登录') + '</div>' +
      list.slice().sort(byName).map(presetRowHTML).join('');
    return;
  }
  _('presetPickerList').innerHTML = _presetKinds.map(g => {
    const items = list.filter(p => (p.kind || 'other') === g.kind).sort(byName);
    if (!items.length) return '';
    return '<div class="preset-group-title">' + t(g.label) +
      (g.desc ? '<span class="preset-group-desc">· ' + t(g.desc) + '</span>' : '') + '</div>' +
      items.map(presetRowHTML).join('');
  }).join('');
}

function applyPreset(key) {
  const p = _presetList.find(x => x.key === key);
  if (!p) return;
  closePresetPicker();
  showProviderEditor(null);
  _('provName').value = p.name;
  _('provBaseURL').value = p.baseURL;
  _('provHeaders').value = Object.entries(p.headers || {}).map(([k, v]) => k + ': ' + v).join('\n');
  _('provModels').value = (p.modelIDs || []).join(', ');
  renderProvModelChips();
  // hasFree/other 是混合目录：整源标 free 会把付费模型误标为免费（审计 §9.2 缺口），默认标为付费。
  // 混合目录（freeTier/hasFree/other）整源标 free 会把付费模型误标为免费（审计 §9.2 缺口），
  // 仅免密钥/本地整源免费；逐模型层级交给 §9.3 分级预览判定。
  const wholeFree = p.kind === 'free' || p.kind === 'local';
  _('provFree').value = wholeFree ? 'true' : 'false';
  // 带种子模型或目录端点的预设：新建保存后自动开分级预览（勾选后才写入，绝不静默导入）
  _previewAfterSave = !p.loginFlow && (!!p.modelIDs || !!p.modelsFetcher || wholeFree);
  if (p.loginFlow) {
    toast(t('已填充登录类预设：请到「账号与连接」完成设备码登录，再在编辑器「账号连接池」勾选连接'), 'info');
  } else if (p.kind === 'free' || p.kind === 'local') {
    // 免密钥/本地源：直接走上游探测，弹出模型多选（免费/付费分组）
    toast(t('免密钥预设：正在自动拉取模型…'), 'info');
    fetchProviderModels(_('provFetchModelsBtn'));
  } else {
    toast(t('已填充预设，请补 API Key 和模型 ID'), 'info');
  }
}

// ========== §9.3 受控导入：preview → 勾选 → apply ==========
let _importBatch = null;

async function previewModelImport(btn) {
  const body = _editingProviderId
    ? { id: _editingProviderId }
    : { baseURL: _('provBaseURL').value.trim(), apiKey: _('provKey').value.trim(),
        headers: parseHeaderText(_('provHeaders').value),
        free: _('provFree').value === 'true', name: _('provName').value.trim() };
  if (!_editingProviderId && !body.baseURL) { toast(t('请先填写 Base URL'), 'info'); return; }
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('扫描中...'); }
  try {
    const d = await api('POST', '/models/import/preview', body);
    const r = d.data || {};
    if (r.error) { toast(t('上游拉取失败') + ': ' + r.error, 'error'); return; }
    _importBatch = r.batch;
    renderImportPreviewList();
    _('importPreviewModal').style.display = 'flex';
  } catch (e) {
    toast(t('加载失败') + ': ' + esc(e.message), 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  }
}

function closeImportPreview() {
  _('importPreviewModal').style.display = 'none';
}

function renderImportPreviewList() {
  const items = (_importBatch && _importBatch.items) || [];
  const cnt = a => items.filter(x => x.action === a).length;
  _('importPreviewProv').textContent = _importBatch ? _importBatch.providerName : '';
  _('importPreviewSummary').textContent =
    t('新增') + ' ' + cnt('add') + ' · ' + t('层级变化') + ' ' + cnt('tier_change') +
    ' · ' + t('无变化') + ' ' + cnt('nochange') + ' · ' + t('受保护') + ' ' + cnt('protected');
  if (!items.length) {
    _('importPreviewList').innerHTML = '<div class="empty">' + t('没有可导入的模型') + '</div>';
    _('importPreviewCount').textContent = '';
    return;
  }
  // Fresh batch: clear any filter from the previous preview.
  const searchEl = _('importPreviewSearch');
  if (searchEl) searchEl.value = '';
  const matchEl = _('importPreviewMatch');
  if (matchEl) matchEl.textContent = '';
  _('importPreviewList').innerHTML = items.map(it => {
    const selectable = it.action === 'add' || it.action === 'tier_change';
    const tierBadge = it.tier === 'free'
      ? '<span class="model-source-badge custom">' + t('免费') + '</span>'
      : '<span class="model-source-badge provider">' + t('付费') + '</span>';
    let action;
    if (it.action === 'add') action = '<span class="model-source-badge zen">' + t('新增') + '</span>';
    else if (it.action === 'tier_change') action = '<span class="model-source-badge provider">' + t('层级变化') + ': ' + esc(it.oldTier === 'free' ? t('免费') : t('付费')) + ' → ' + esc(it.tier === 'free' ? t('免费') : t('付费')) + '</span>';
    else if (it.action === 'protected') action = '<span class="model-source-badge" title="' + esc(it.reason || '') + '">' + t('受保护') + '</span>';
    else action = '<span class="model-source-badge" style="opacity:.6">' + t('无变化') + '</span>';
    const sig = '<span style="font-size:11px;color:var(--text3)">' + esc(t(_tierSignals[it.signal] || it.signal)) + '</span>';
    return '<label class="combo-pick-row" style="display:flex;align-items:center;gap:8px;padding:7px 10px;border-bottom:1px solid var(--border2);font-size:12px">' +
      '<input type="checkbox" class="import-approve" onchange="updateImportCount()" value="' + esc(it.modelId) + '" ' + (it.action === 'add' ? 'checked' : '') + (selectable ? '' : ' disabled') + '>' +
      '<span class="combo-pick-id">' + esc(it.modelId) + '</span>' + tierBadge + sig + action + '</label>';
  }).join('');
  updateImportCount();
}

function updateImportCount() {
  const n = document.querySelectorAll('#importPreviewList .import-approve:checked').length;
  _('importPreviewCount').textContent = n + ' ' + t('个已选');
}

// Hide/show preview rows by id substring without re-rendering (checkbox state stays in the DOM).
function filterImportPreview() {
  const q = ((_('importPreviewSearch') || {}).value || '').trim().toLowerCase();
  let shown = 0;
  document.querySelectorAll('#importPreviewList .combo-pick-row').forEach(row => {
    const idEl = row.querySelector('.combo-pick-id');
    const hit = !q || (idEl && idEl.textContent.toLowerCase().indexOf(q) !== -1);
    row.style.display = hit ? 'flex' : 'none';
    if (hit) shown++;
  });
  const total = (_importBatch && _importBatch.items.length) || 0;
  const matchEl = _('importPreviewMatch');
  if (matchEl) matchEl.textContent = q ? shown + ' / ' + total : '';
}

// Toggle every enabled + currently visible approval checkbox (select all / clear all).
function importPreviewSelectAll(on) {
  document.querySelectorAll('#importPreviewList .import-approve').forEach(cb => {
    if (cb.disabled) return;
    const row = cb.closest('.combo-pick-row');
    if (row && row.style.display === 'none') return;
    cb.checked = !!on;
  });
  updateImportCount();
}

async function importPreviewApply(btn) {
  if (!_importBatch) return;
  const approve = Array.from(document.querySelectorAll('#importPreviewList .import-approve:checked')).map(el => el.value);
  if (!approve.length) { toast(t('请先勾选要导入的模型'), 'info'); return; }
  // 草稿批次（未保存 Provider）：服务端无 provider 可写 → 所选并入表单模型列表并触发保存
  if (!_importBatch.providerId) {
    const input = _('provModels');
    const seen = {};
    const merged = input.value.split(',').map(s => s.trim()).filter(Boolean)
      .concat(approve)
      .filter(id => !seen[id] && (seen[id] = true));
    input.value = merged.join(', ');
    renderProvModelChips();
    closeImportPreview();
    _importBatch = null;
    toast(t('所选模型已并入，正在保存 Provider...'), 'info');
    await saveProvider(null);
    return;
  }
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('导入中...'); }
  try {
    const d = await api('POST', '/models/import/apply', { batchId: _importBatch.id, approve: approve });
    const r = d.data || {};
    toast(t('已导入 ') + (r.added || 0) + t(' 个新模型，调整 ') + (r.changed || 0) + t(' 个层级'), 'success');
    closeImportPreview();
    _importBatch = null;
    loadProviders();
    loadModels();
  } catch (e) {
    toast(t('导入失败: ') + esc(e.message), 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  }
}

// ========== §8.3 登录连接：设备码登录 / 粘贴 token / 列表·测试·删除 ==========
let _connFlows = [];
let _connList = [];
let _loginSessionId = '';
let _loginPollTimer = null;

// 流程 key → 用户可读名（未知 key 回落原样显示）
const _flowLabels = {
  kilocode: 'Kilo Code', github: 'GitHub Copilot', qwen: 'Qwen Chat',
  'kimi-coding': 'Kimi for Coding', 'codebuddy-cn': 'CodeBuddy CN（腾讯）',
  'codebuddy-intl': 'CodeBuddy Intl', workbuddy: 'WorkBuddy',
};
function flowLabel(p) { return t(_flowLabels[p] || p); }

async function loadConnections() {
  try {
    const d = await api('GET', '/connections');
    const r = d.data || {};
    _connFlows = r.flows || [];
    _connList = r.connections || [];
  } catch (e) {
    _connFlows = []; _connList = [];
  }
  renderConnectionsList();
  const dl = _('connProviderList');
  if (dl) dl.innerHTML = _connFlows.map(p => '<option value="' + esc(p) + '" label="' + esc(flowLabel(p)) + '">').join('');
  const ed = _('providerEditor');
  if (ed && ed.classList.contains('open')) renderConnPick();
}

// Wave-4: connection rows live on the merged Accounts page (账号与连接),
// grouped by provider service, each with its own usage + per-model stats.
function renderConnectionsList() {
  const box = _('connectionsList');
  if (!box) return;
  if (!_connList.length) { box.innerHTML = listEmptyState(ICON_LOGINS, t('暂无登录连接') + ' · ' + t('用设备码登录或导入已有 Token')); return; }
  const sorted = _connList.slice().sort((a, b) =>
    String(a.provider).localeCompare(String(b.provider)) || String(a.email || '').localeCompare(String(b.email || '')));
  let lastProv = null;
  const rows = sorted.map(c => {
    let groupRow = '';
    if (c.provider !== lastProv) {
      lastProv = c.provider;
      groupRow = '<tr class="conn-group-row"><td colspan="6">' + esc(flowLabel(c.provider)) +
        ' <span class="mono" style="font-size:11px;color:var(--text3);font-weight:400">' + esc(c.provider) + '</span></td></tr>';
    }
    const stats = Object.values(c.modelStats || {}).sort((x, y) => y.totalTokens - x.totalTokens);
    const statRows = stats.length
      ? '<tr style="background:var(--surface2)"><td colspan="6" style="padding:8px 32px;color:var(--text2);font-size:12px;font-weight:600">' + t('按模型统计') + ' · ' + stats.length + ' ' + t('个模型') + '</td></tr>' +
        stats.map(st =>
          '<tr style="background:var(--surface2)">' +
            '<td style="padding-left:32px" class="mono">' + esc(st.modelId) +
              ' <span class="model-tag' + (st.cost === 'free' ? ' free' : '') + '" style="font-size:10px;padding:1px 6px">' + esc(st.cost || 'paid') + '</span></td>' +
            '<td></td>' +
            '<td>' + formatNumber(st.usageCount) + '</td>' +
            '<td>' + formatTokenCount(st.promptTokens) + '</td>' +
            '<td>' + formatTokenCount(st.totalTokens) + '</td>' +
            '<td>' + formatTokenCount(st.cachedTokens) + '</td>' +
          '</tr>').join('')
      : '<tr style="background:var(--surface2)"><td colspan="6" style="padding:6px 32px;color:var(--text3);font-size:12px">' + t('暂无数据') + '</td></tr>';
    return groupRow + '<tr>' +
      '<td><button class="btn btn-sm btn-icon" onclick="toggleConnStats(\'' + escAttr(c.id) + '\', this)" title="' + t('展开') + '" aria-label="' + t('展开') + '">▸</button></td>' +
      '<td style="font-size:11px;color:var(--text3)">' + esc(c.authType === 'device_code' ? t('设备码登录') : c.authType === 'import_token' ? t('导入 Token') : (c.authType || '')) + '</td>' +
      '<td style="font-size:12px">' + esc(c.email || (c.providerSpecificData && c.providerSpecificData.orgId ? 'org ' + c.providerSpecificData.orgId : '—')) + '</td>' +
      '<td>' + (c.status === 'active'
        ? '<span class="status active"><span class="status-dot active"></span>' + t('已登录') + '</span>'
        : '<span class="status disabled"><span class="status-dot disabled"></span>' + t('需重新登录') + '</span>') + '</td>' +
      '<td style="font-size:12px">' + (c.usageCount
        ? formatNumber(c.usageCount) + ' ' + t('请求') + ' · ' + formatTokenCount(c.totalTokens || 0) + ' token'
        : '<span style="color:var(--text3)">—</span>') + '</td>' +
      '<td><button class="btn btn-sm" onclick=\'testConnection("' + c.id + '", this)\'>' + t('测试') + '</button> ' +
          '<span class="conn-row-test-out"></span> ' +
          '<button class="btn btn-sm btn-danger" onclick=\'deleteConnection("' + c.id + '")\' title="' + t('删除') + '" aria-label="' + t('删除') + '">✕</button></td>' +
      '</tr>' +
      '<tr id="connStatsRow-' + escAttr(c.id) + '" style="display:none"><td colspan="6" style="padding:0">' +
        '<table class="model-subtable" style="width:100%">' +
          '<tr><th style="padding-left:32px;text-align:left">' + t('模型') + '</th><th></th><th style="text-align:left">' + t('请求') + '</th><th style="text-align:left">' + t('输入') + '</th><th style="text-align:left">' + t('总量') + '</th><th style="text-align:left">' + t('缓存') + '</th></tr>' +
          statRows +
        '</table></td></tr>';
  }).join('');
  box.innerHTML = '<table class="action-table"><thead><tr><th style="width:34px"></th><th>' + t('方式') + '</th><th>' + t('账号') + '</th><th>' + t('状态') + '</th><th>' + t('用量') + '</th><th>' + t('操作') + '</th></tr></thead><tbody>' + rows + '</tbody></table>';
}

function toggleConnStats(id, btn) {
  const row = _('connStatsRow-' + id);
  if (!row) return;
  const hidden = row.style.display === 'none';
  row.style.display = hidden ? '' : 'none';
  btn.innerHTML = hidden ? '▾' : '▸';
  btn.title = hidden ? t('收起') : t('展开');
}

function openDeviceLogin() {
  const sel = _('connLoginProvider');
  if (sel) {
    sel.innerHTML = _connFlows.length
      ? _connFlows.map(p => '<option value="' + esc(p) + '">' + esc(flowLabel(p)) + '</option>').join('')
      : '<option value="kilocode">Kilo Code</option>';
  }
  _('connLoginStep1').style.display = '';
  _('connLoginStep2').style.display = 'none';
  _('connLoginModal').style.display = 'flex';
}

function closeDeviceLogin() {
  if (_loginPollTimer) { clearInterval(_loginPollTimer); _loginPollTimer = null; }
  _loginSessionId = '';
  const m = _('connLoginModal');
  if (m) m.style.display = 'none';
}

async function beginDeviceLogin(btn) {
  const provider = (_('connLoginProvider') && _('connLoginProvider').value) || 'kilocode';
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('发起中...'); }
  try {
    const d = await api('POST', '/connections/login/start', { provider: provider });
    const r = d.data || {};
    if (r.error) { toast(t('发起登录失败: ') + r.error, 'error'); return; }
    _loginSessionId = r.sessionId;
    _('connLoginCode').textContent = r.userCode || '';
    const a = _('connLoginUrl');
    a.href = r.authUrl || '#'; a.textContent = r.authUrl || '';
    _('connLoginStatus').innerHTML = '<span class="loading"></span> ' + t('等待授权...');
    _('connLoginStep1').style.display = 'none';
    _('connLoginStep2').style.display = '';
    if (_loginPollTimer) clearInterval(_loginPollTimer);
    _loginPollTimer = setInterval(pollDeviceLogin, 2000);
  } catch (e) {
    toast(t('发起登录失败: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  }
}

async function pollDeviceLogin() {
  if (!_loginSessionId) return;
  try {
    const d = await api('GET', '/connections/login/status?sessionId=' + encodeURIComponent(_loginSessionId));
    const r = d.data || {};
    if (!r.done) return;
    clearInterval(_loginPollTimer); _loginPollTimer = null;
    if (r.success) {
      toast(t('登录成功') + ': ' + (r.userCode || ''), 'success');
      closeDeviceLogin();
      loadConnections();
    } else {
      _('connLoginStatus').innerHTML = renderTestResultHTML(false, r.error || t('登录失败'));
    }
  } catch (e) { /* 网络抖动下轮询继续 */ }
}

function copyDeviceCode() {
  const code = (_('connLoginCode') && _('connLoginCode').textContent) || '';
  if (!code) return;
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) { navigator.clipboard.writeText(code); }
    else {
      const ta = document.createElement('textarea'); ta.value = code; document.body.appendChild(ta);
      ta.select(); document.execCommand('copy'); document.body.removeChild(ta);
    }
    toast(t('代码已复制'), 'info');
  } catch (e) {}
}

function showConnectionImport() {
  _('connImpProvider').value = (_connFlows[0]) || '';
  _('connImpToken').value = ''; _('connImpEmail').value = ''; _('connImpOrg').value = '';
  _('connImportModal').style.display = 'flex';
  try { _('connImpToken').focus(); } catch (e) {}
}

function hideConnectionImport() { _('connImportModal').style.display = 'none'; }

async function submitConnectionImport(btn) {
  const body = {
    provider: _('connImpProvider').value.trim(),
    accessToken: _('connImpToken').value.trim(),
    email: _('connImpEmail').value.trim(),
    orgId: _('connImpOrg').value.trim(),
  };
  if (!body.provider || !body.accessToken) { toast(t('服务与 Token 必填'), 'error'); return; }
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    await api('POST', '/connections/import', body);
    toast(t('连接已保存'), 'success');
    hideConnectionImport();
    loadConnections();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

async function testConnection(id, cell) {
  const out = cell && cell.closest ? cell.closest('td').querySelector('.conn-row-test-out') : null;
  const orig = cell ? cell.innerHTML : '';
  if (cell) { cell.disabled = true; cell.innerHTML = '<span class="loading"></span> ' + t('测试中...'); }
  if (out) out.innerHTML = '';
  try {
    const d = await api('POST', '/connections/test', { id });
    const r = d.data || {};
    if (out) out.innerHTML = renderTestResultHTML(r.ok, r.ok ? (t('可用') + ' · HTTP ' + r.status) : (r.error || t('失败')));
    loadConnections();
  } catch (e) {
    if (out) out.innerHTML = renderTestResultHTML(false, friendlyTestError(e));
  } finally {
    if (cell) { cell.disabled = false; cell.innerHTML = orig; }
  }
}

async function deleteConnection(id) {
  if (!(await showConfirm(t('确定删除该连接？绑定它的 Provider 将不再注入 token。')))) return;
  try {
    await api('POST', '/connections/delete', { id });
    toast(t('已删除'), 'success');
    loadConnections();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

// Wave-4: connection pool picker in the provider drawer (checkbox list grouped
// by provider service). Replaces the old single-select provConnection dropdown.
let _provConnSel = [];

function initConnPick(ids) { _provConnSel = (ids || []).slice(); renderConnPick(); }

function toggleConnPick(id, on) {
  const i = _provConnSel.indexOf(id);
  if (on && i < 0) _provConnSel.push(id);
  if (!on && i >= 0) _provConnSel.splice(i, 1);
  const cnt = _('provConnCount');
  if (cnt) cnt.textContent = ' · ' + (_provConnSel.length ? _provConnSel.length + t('已选') : t('未选择'));
}

function renderConnPick() {
  const box = _('provConnPick');
  if (!box) return;
  const cnt = _('provConnCount');
  if (cnt) cnt.textContent = ' · ' + (_provConnSel.length ? _provConnSel.length + t('已选') : t('未选择'));
  if (!_connList.length) {
    box.innerHTML = '<span class="conn-pick-empty">' + t('暂无登录连接') + ' · <a href="javascript:void(0)" onclick="showConnectionImport()" style="color:var(--accent)">' + t('导入 Token') + '</a></span>';
    return;
  }
  const byProv = {};
  _connList.forEach(c => { (byProv[c.provider] = byProv[c.provider] || []).push(c); });
  box.innerHTML = Object.keys(byProv).sort().map(prov =>
    '<div class="conn-pick-group">' + esc(flowLabel(prov)) + '</div>' +
    byProv[prov].map(c => {
      const on = _provConnSel.includes(c.id);
      const who = c.email || (c.providerSpecificData && c.providerSpecificData.orgId ? 'org ' + c.providerSpecificData.orgId : c.id);
      return '<label class="conn-pick-item' + (on ? ' on' : '') + '">' +
        '<input type="checkbox"' + (on ? ' checked' : '') + ' onchange="toggleConnPick(\'' + escAttr(c.id) + '\', this.checked)"> ' +
        '<span class="mono" style="font-size:12px">' + esc(who) + '</span>' +
        (c.status !== 'active' ? ' <span class="conn-pick-warn">' + t('需重新登录') + '</span>' : '') +
        (c.usageCount ? ' <span class="conn-pick-usage">' + formatNumber(c.usageCount) + ' ' + t('请求') + '</span>' : '') +
        '</label>';
    }).join('')).join('');
}

async function saveHeaders(btn) {
  const tbody = _('headersTableBody');
  const rows = tbody.querySelectorAll('tr');
  const headers = {};
  let hasEmpty = false;
  rows.forEach(tr => {
    const keyInput = tr.querySelector('.header-key');
    const valInput = tr.querySelector('.header-val');
    if (keyInput && valInput) {
      const k = keyInput.value.trim();
      const v = valInput.value.trim();
      if (k) { headers[k] = v; }
      else if (v) { hasEmpty = true; }
    }
  });
  if (hasEmpty) { toast(t('存在有值无键的行，已忽略'), 'info'); }
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    const d = await api('POST', '/config/update', { headers });
    toast(t('请求头已保存'), 'success');
    _('headerSaveResult').innerHTML =
      '<div style="color:var(--green);font-size:13px">✓ ' + t('已保存 ')+ Object.keys(d.data.headers).length + t(' 个请求头') + '</div>';
    setTimeout(() => _('headerSaveResult').innerHTML = '', 5000);
    loadConfig();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

