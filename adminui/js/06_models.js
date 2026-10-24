// ========== 模型列表 ==========
let _cachedModels = [];
let _modelSyncSeen = false;
let _modelGroupOpen = {}; // 分组折叠状态（localStorage 持久化）
const _modelFilters = { search: '', provider: '', cost: '', status: '' };
const MODEL_GROUP_OPEN_KEY = 'cline_admin_model_groups_open';
try {
  const _mgo = JSON.parse(localStorage.getItem(MODEL_GROUP_OPEN_KEY) || '{}');
  if (_mgo && typeof _mgo === 'object') _modelGroupOpen = _mgo;
} catch (e) {}

// ===== 两级分组：一级 = 真实来源（Cline / Zen / Provider / 自定义），二级 = 厂商 =====
// mergedSourceKey：合并行的主来源，优先级 自定义 > Provider > zen > Cline
function mergedSourceKey(m) {
  if (m._customAny) return 'custom';
  if (m._sources.indexOf('provider') !== -1) return 'provider';
  if (m._sources.indexOf('zen') !== -1) return 'zen';
  return 'remote';
}

// 一级分组 key：src:cline / src:zen / src:prov:<name> / src:custom / src:other
function sourceGroupKey(m) {
  const src = mergedSourceKey(m);
  if (src === 'provider') return 'src:prov:' + ((m._provNames && m._provNames[0]) || m.provider || 'Provider');
  if (src === 'zen') return 'src:zen';
  if (src === 'custom') return 'src:custom';
  if (src === 'remote') return 'src:cline';
  return 'src:other';
}

function sourceGroupLabel(key) {
  if (key === 'src:cline') return t('Cline');
  if (key === 'src:zen') return t('Zen (opencode)');
  if (key.indexOf('src:prov:') === 0) return key.slice(9);
  if (key === 'src:custom') return t('用户自定义');
  return t('其他');
}

function sourceSortKey(a, b) {
  const ord = k => k === 'src:cline' ? 0 : (k === 'src:zen' ? 1 : (k.indexOf('src:prov:') === 0 ? 2 : (k === 'src:custom' ? 3 : 4)));
  const oa = ord(a), ob = ord(b);
  if (oa !== ob) return oa - ob;
  return a.localeCompare(b);
}

// 二级（厂商）key：模型 ID 第一个 "/" 前的前缀；无前缀回退 provider 字段
function vendorKey(m) {
  const idx = (m.id || '').indexOf('/');
  if (idx > 0) return m.id.slice(0, idx);
  return m.provider || '__other';
}

function vendorLabel(key) { return key === '__other' ? t('其他') : key; }

function vendorSortKey(a, b) {
  if (a === '__other') return 1;
  if (b === '__other') return -1;
  return a.localeCompare(b);
}

function modelSource(m) {
  if (m.custom) return 'custom';
  if (m.source === 'provider') return 'provider';
  if (m.source === 'zen' || m.provider === 'opencode') return 'zen';
  return 'remote';
}
const MODEL_SOURCE_LABEL = { zen: 'opencode', remote: 'Cline', provider: 'Provider' };

// mergeModelsById 同一模型 ID 可能同时被 Cline 与 opencode 同步（池内多条目）：
// 合并为一行展示 —— 来源/计费取并集；delisted 区分「任一下架/全部下架」
// （任一来源仍在官方列表就不算整体下架）；启停开关按 ID 作用到所有来源条目。
function mergeModelsById(models) {
  const map = new Map();
  for (const m of models) {
    const prev = map.get(m.id);
    if (!prev) {
      map.set(m.id, {
        id: m.id, provider: m.provider || '', context: m.context || 0, output: m.output || 0,
        _sources: [modelSource(m)],
        _costs: [m.cost || 'free'],
        _provNames: (modelSource(m) === 'provider' && m.provider) ? [m.provider] : [],
        _delistedAny: !!m.delisted,
        _delistedAll: !!m.delisted,
        _customAny: !!m.custom,
        _disabled: !!m.disabled,
      });
      continue;
    }
    const src = modelSource(m);
    if (prev._sources.indexOf(src) === -1) prev._sources.push(src);
    if (prev._costs.indexOf(m.cost || 'free') === -1) prev._costs.push(m.cost || 'free');
    // 同一模型 ID 可被多个自定义 Provider 同步进来（同名不同源）：
    // 全部来源名字都亮出来，肉眼即可分辨同一模型的两个上游
    if (src === 'provider' && m.provider && prev._provNames.indexOf(m.provider) === -1) prev._provNames.push(m.provider);
    prev._delistedAny = prev._delistedAny || !!m.delisted;
    prev._delistedAll = prev._delistedAll && !!m.delisted;
    prev._customAny = prev._customAny || !!m.custom;
    prev._disabled = prev._disabled || !!m.disabled;
    if (!prev.provider && m.provider) prev.provider = m.provider;
    if ((m.context || 0) > prev.context) prev.context = m.context || 0;
    if ((m.output || 0) > prev.output) prev.output = m.output || 0;
  }
  return Array.from(map.values());
}

function onModelFilterChange(key, value) {
  _modelFilters[key] = value;
  renderModelList();
}

function modelMatchesFilters(m) {
  if (_modelFilters.search && m.id.toLowerCase().indexOf(_modelFilters.search.toLowerCase()) === -1) return false;
  if (_modelFilters.provider && sourceGroupKey(m) !== _modelFilters.provider) return false;
  if (_modelFilters.cost && m._costs.indexOf(_modelFilters.cost) === -1) return false;
  if (_modelFilters.status === 'disabled' && !m._disabled) return false;
  if (_modelFilters.status === 'enabled' && m._disabled) return false;
  return true;
}

// 工具栏「来源」筛选：按一级来源分组 key 生成选项
function buildProviderOptions(merged) {
  const sel = _('modelFilterProvider');
  if (!sel) return;
  const keys = Array.from(new Set(merged.map(sourceGroupKey))).sort(sourceSortKey);
  const cur = _modelFilters.provider || sel.value;
  sel.innerHTML = '<option value="">' + t('全部来源') + '</option>' +
    keys.map(k => '<option value="' + escAttr(k) + '"' + (k === cur ? ' selected' : '') + '>' + esc(sourceGroupLabel(k)) + '</option>').join('');
}

function renderModelStats(merged) {
  const bar = _('modelStatsBar');
  if (!bar) return;
  if (!merged.length) { bar.innerHTML = ''; return; }
  const disabled = merged.filter(m => m._disabled).length;
  const delisted = merged.filter(m => m._delistedAll).length;
  bar.innerHTML =
    '<span>' + t('共') + ' <b>' + merged.length + '</b> ' + t('个模型') + '</span>' +
    '<span><span class="dot g"></span>' + t('已启用') + ' <b>' + (merged.length - disabled) + '</b></span>' +
    '<span><span class="dot y"></span>' + t('已停用') + ' <b>' + disabled + '</b></span>' +
    (delisted ? '<span><span class="dot r"></span>' + t('已下架') + ' <b>' + delisted + '</b></span>' : '');
}

function renderModelRow(m) {
  let tags = '';
  const hasFree = m._costs.indexOf('free') !== -1;
  const hasPass = m._costs.indexOf('pass') !== -1;
  if (hasFree && hasPass) {
    tags += '<span class="model-tag free">' + t('免费') + '</span><span class="model-tag pass">' + t('付费') + '</span>';
  } else if (hasFree) {
    tags += '<span class="model-tag free">' + t('免费') + '</span>';
  } else {
    tags += '<span class="model-tag pass">' + t('付费') + '</span>';
  }
  // Per-row tier provenance: surface the classifier signal that decided the
  // cost, matching the provider import preview (manual tiering shows no badge).
  if (m.tierSignal && m.tierSignal !== 'manual' && _tierSignals[m.tierSignal]) {
    const sigLabel = t(_tierSignals[m.tierSignal]);
    tags += '<span class="model-source-badge" title="' + escAttr(t('自动判定层级') + ': ' + sigLabel) + '">' + esc(sigLabel) + '</span>';
  }
  if (m._delistedAll) {
    tags += '<span class="delisted-badge" title="' + t('已从上游官方列表移除，实测可能仍可用；同步只标记不删除，上游报模型不存在时自动清理') + '">' + t('已下架') + '</span>';
  }
  for (const s of m._sources) {
    if (s === 'custom') tags += '<span class="model-source-badge custom">' + t('自定义') + '</span>';
    else if (s === 'provider') {
      // 别名标识： Provider 来源的 badge 显示真实名称（多源同名模型全部列出）
      const n = (m._provNames && m._provNames.length) ? m._provNames.join(' · ') : (m.provider || MODEL_SOURCE_LABEL[s]);
      tags += '<span class="model-source-badge provider" title="' + esc(m.id) + '">' + esc(n) + '</span>';
    }
    else tags += '<span class="model-source-badge ' + s + '">' + MODEL_SOURCE_LABEL[s] + '</span>';
  }
  let meta = '';
  if (m.context) meta += 'ctx ' + formatTokenCount(m.context);
  if (m.output) meta += (meta ? ' · ' : '') + 'out ' + formatTokenCount(m.output);
  // 所有模型都开放上下文/最大输出编辑：Cline 模型不在已知硬限制表时，
  // 手动设置的 Output 会作为 modelMaxOutputLimit 的封顶值生效
  const prevTest = _modelTestResults[m.id] || null;
  let actions = '<span class="model-test-out" title="' + (prevTest && !prevTest.ok ? escAttr(prevTest.text) : '') + '" ' +
      (prevTest ? 'data-tested="1"' : '') + '>' + (prevTest ? renderTestResultHTML(prevTest.ok, prevTest.text) : '') + '</span>' +
    '<button class="btn btn-sm" style="padding:2px 6px" onclick="testModelRow(\'' + escAttr(m.id) + '\', this)" title="' + t('测试') + '" aria-label="' + t('测试') + '">' + t('测试') + '</button>' +
    '<button class="btn btn-sm" style="padding:2px 6px" onclick="editModelMeta(\'' + esc(m.id) + '\',' + m.context + ',' + m.output + ',\'' + esc(m.cost || '') + '\',\'' + esc(m.tierSignal || '') + '\')" title="' + t('上下文窗口 / 最大输出 (token)，点击⚙修改') + '" aria-label="' + t('上下文窗口 / 最大输出 (token)，点击⚙修改') + '">⚙</button>';
  if (m._customAny || m._delistedAny) {
    actions += '<button class="btn btn-sm btn-danger" style="padding:2px 6px" onclick="deleteModel(\'' + esc(m.id) + '\')" title="' + (m._delistedAny ? t('移除（已下架模型支持手动移除）') : t('删除')) + '" aria-label="' + (m._delistedAny ? t('移除（已下架模型支持手动移除）') : t('删除')) + '">✕</button>';
  }
  return '<div class="model-row' + (m._disabled ? ' disabled-row' : '') + '">' +
    '<label class="switch" title="' + (m._disabled ? t('点击启用该模型') : t('点击停用该模型')) + '"><input type="checkbox"' + (m._disabled ? '' : ' checked') + ' onchange="toggleModel(\'' + esc(m.id) + '\', !this.checked)"><span class="slider"></span></label>' +
    '<div class="model-row-main"><span class="model-row-id mono">' + esc(m.id) + '</span>' + tags + '</div>' +
    '<div class="model-row-meta" title="' + t('上下文窗口 / 最大输出 (token)，点击⚙修改') + '">' + meta + '</div>' +
    '<div class="model-row-actions">' + actions + '</div>' +
  '</div>';
}

// ========== 模型行内测试（/models/test：16-token 小探测，服务端按模型串行化） ==========
// 结果按模型 ID 回记，筛选/折叠重渲染时回显上次测试结论
const _modelTestResults = {}; // id -> { ok, text }

async function testModelRow(id, btn) {
  if (btn && btn.disabled) return; // 行级防重入：等待完成前忽略再次点击
  const row = btn && btn.closest ? btn.closest('.model-row') : null;
  const out = row ? row.querySelector('.model-test-out') : null;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>'; }
  if (out) out.innerHTML = '<span style="color:var(--text3)"><span class="loading"></span></span>';
  try {
    const d = await api('POST', '/models/test', { model: id });
    const r = d.data || {};
    const text = r.ok ? ((r.upstream || '') + ' · ' + (r.durationMs || 0) + 'ms') : (r.error || t('失败'));
    _modelTestResults[id] = { ok: !!r.ok, text: text };
    if (out) {
      out.innerHTML = renderTestResultHTML(r.ok, text);
      out.title = r.ok ? '' : text;
    }
  } catch (e) {
    const text = friendlyTestError(e);
    _modelTestResults[id] = { ok: false, text: text };
    if (out) out.innerHTML = renderTestResultHTML(false, text);
    if (out) out.title = text;
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  }
}

// 模型管理列表渲染：两级分组（来源 → 厂商）+ 工具栏筛选；两级都可折叠、可批量启停
function renderModelList() {
  const listEl = _('modelsList');
  if (!listEl) return;
  const merged = mergeModelsById(_cachedModels);
  renderModelStats(merged);
  buildProviderOptions(merged);
  if (!merged.length) {
    listEl.innerHTML = listEmptyState(ICON_MODELS, t('暂无模型'));
    return;
  }
  const filtered = merged.filter(modelMatchesFilters);
  if (!filtered.length) {
    listEl.innerHTML = listEmptyState(ICON_MODELS, t('无匹配模型'));
    return;
  }
  // 一级：来源；二级：厂商
  const sources = new Map(); // sourceKey -> Map(vendorKey -> rows[])
  for (const m of filtered) {
    const sk = sourceGroupKey(m);
    const vk = vendorKey(m);
    if (!sources.has(sk)) sources.set(sk, new Map());
    const vmap = sources.get(sk);
    if (!vmap.has(vk)) vmap.set(vk, []);
    vmap.get(vk).push(m);
  }
  const groupActions = () =>
    '<span class="model-group-actions" onclick="event.stopPropagation()">' +
      '<button class="btn" onclick="toggleModelsBatchEl(this, false)">' + t('全部启用') + '</button>' +
      '<button class="btn" onclick="toggleModelsBatchEl(this, true)">' + t('全部停用') + '</button>' +
    '</span>';
  const groupHead = (key, label, count, open, src) =>
    '<div class="model-group-head' + (src ? ' model-group-src-head' : '') + (open ? ' expanded' : '') + '" data-key="' + escAttr(key) + '" onclick="toggleModelGroupEl(this)" title="' + t('点击展开/折叠') + '">' +
      '<span class="model-group-caret">▸</span>' +
      '<span class="model-group-label">' + esc(label) + '</span>' +
      '<span class="model-group-count">' + count + '</span>' +
      groupActions() +
    '</div>';
  listEl.innerHTML = Array.from(sources.keys()).sort(sourceSortKey).map(sk => {
    const vmap = sources.get(sk);
    let total = 0;
    vmap.forEach(rows => { total += rows.length; });
    const sOpen = _modelGroupOpen[sk] !== false;
    const vendors = Array.from(vmap.keys()).sort(vendorSortKey);
    const inner = vendors.map(vk => {
      const rows = vmap.get(vk).slice().sort((a, b) => a.id.localeCompare(b.id));
      const vPath = sk + '/' + vk;
      const vOpen = _modelGroupOpen[vPath] !== false;
      return '<div class="model-group model-group-vendor">' +
        groupHead(vPath, vendorLabel(vk), rows.length, vOpen, false) +
        '<div class="model-group-body" style="display:' + (vOpen ? 'block' : 'none') + '">' + rows.map(renderModelRow).join('') + '</div>' +
      '</div>';
    }).join('');
    return '<div class="model-group model-group-src">' +
      groupHead(sk, sourceGroupLabel(sk), total, sOpen, true) +
      '<div class="model-group-body" style="display:' + (sOpen ? 'block' : 'none') + '">' + inner + '</div>' +
    '</div>';
  }).join('');
}

// 折叠/展开分组（两级通用）：直接读自身 data-key，避免把 key 拼进 onclick 字符串
function toggleModelGroupEl(head) {
  if (!head) return;
  const key = head.dataset.key || '';
  const open = !head.classList.contains('expanded');
  head.classList.toggle('expanded', open);
  const body = head.nextElementSibling; // 直接相邻的 .model-group-body
  if (body && body.classList.contains('model-group-body')) body.style.display = open ? 'block' : 'none';
  _modelGroupOpen[key] = open;
  try { localStorage.setItem(MODEL_GROUP_OPEN_KEY, JSON.stringify(_modelGroupOpen)); } catch (e) {}
}

// 启用/停用模型：按 ID 提交（池内同 ID 的多来源条目一并切换），后端在
// 停用当前默认模型时会自动清空默认；完成后刷新列表与默认模型下拉。
async function toggleModel(id, disabled) {
  try {
    await api('POST', '/models/toggle', { ids: [id], disabled: disabled });
    toast((disabled ? t('已停用') : t('已启用')) + ': ' + id, 'success');
  } catch (e) {
    toast(t('保存失败: ') + e.message, 'error');
  }
  await loadModels();
  if (_('settingDefModel')) loadConfig();
}

// 将分组 key 解析为合并行集合：src:xxx 匹配整个来源；src:xxx/vendor 匹配单一厂商
function mergedRowsForGroupKey(key) {
  const merged = mergeModelsById(_cachedModels);
  if (!key || key.indexOf('src:') !== 0) return merged.filter(m => vendorKey(m) === key);
  const slash = key.indexOf('/');
  if (slash === -1) return merged.filter(m => sourceGroupKey(m) === key);
  const srcKey = key.slice(0, slash);
  const vKey = key.slice(slash + 1);
  return merged.filter(m => sourceGroupKey(m) === srcKey && vendorKey(m) === vKey);
}

// 分组按钮入口：从按钮所在分组头读取 key（两级通用）
function toggleModelsBatchEl(btn, disabled) {
  const head = btn && btn.closest ? btn.closest('.model-group-head') : null;
  if (!head) return;
  toggleModelsBatch(head.dataset.key || '', disabled);
}

async function toggleModelsBatch(groupKey, disabled) {
  const ids = mergedRowsForGroupKey(groupKey).map(m => m.id);
  if (!ids.length) return;
  const confirmMsg = disabled ? t('确定停用该分组全部模型？') : t('确定启用该分组全部模型？');
  if (!(await showConfirm(confirmMsg + ' (' + ids.length + ')'))) return;
  try {
    await api('POST', '/models/toggle', { ids: ids, disabled: disabled });
    toast((disabled ? t('已停用') : t('已启用')) + ' ' + ids.length + ' ' + t('个模型'), 'success');
  } catch (e) {
    toast(t('保存失败: ') + e.message, 'error');
  }
  await loadModels();
  if (_('settingDefModel')) loadConfig();
}

// 免费导向的批量开关：一次作用于全部合并行（不受工具栏筛选影响）
async function bulkModelToggle(mode) {
  _setToolbarMoreOpen(false);
  const merged = mergeModelsById(_cachedModels);
  let rows, disabled, btn, confirmMsg;
  if (mode === 'free') {
    rows = merged.filter(m => m._costs.indexOf('free') !== -1);
    disabled = false;
    btn = _('bulkEnableFreeBtn');
    confirmMsg = t('确定启用全部免费模型？');
  } else {
    // 停用付费：仅纯付费模型（free+pass 混合的保持启用，优先保免费）
    rows = merged.filter(m => m._costs.indexOf('pass') !== -1 && m._costs.indexOf('free') === -1);
    disabled = true;
    btn = _('bulkDisablePaidBtn');
    confirmMsg = t('确定停用全部付费模型？');
  }
  const ids = rows.map(m => m.id);
  if (!ids.length) { toast(t('没有符合条件的模型'), 'info'); return; }
  if (!(await showConfirm(confirmMsg + ' (' + ids.length + ')'))) return;
  if (btn) btn.disabled = true;
  try {
    await api('POST', '/models/toggle', { ids: ids, disabled: disabled });
    toast((disabled ? t('已停用') : t('已启用')) + ' ' + ids.length + ' ' + t('个模型'), 'success');
  } catch (e) {
    toast(t('保存失败: ') + e.message, 'error');
  }
  if (btn) btn.disabled = false;
  await loadModels();
  if (_('settingDefModel')) loadConfig();
}

function toggleAddModelRow() {
  const row = _('modelAddRow');
  row.style.display = (row.style.display === 'none') ? 'flex' : 'none';
  if (row.style.display === 'flex') { setModelCost(_('newModelCost').value || 'pass'); _('newModelId').focus(); }
}

// 计费分段按钮：真值写入保留的隐藏 select#newModelCost（addModel 读取路径不变）
function setModelCost(v) {
  const sel = _('newModelCost');
  if (sel) sel.value = v;
  document.querySelectorAll('#modelCostSeg button').forEach(b => b.classList.toggle('active', b.dataset.cost === v));
}

// ===== 模型工具栏「更多」溢出菜单：开关 + 点击外部收起（vanilla，无外部库） =====
function _setToolbarMoreOpen(open) {
  const more = _('modelBulkMore'), btn = _('toolbarMoreBtn');
  if (more) more.classList.toggle('open', open);
  if (btn) btn.setAttribute('aria-expanded', open ? 'true' : 'false');
}
function toggleToolbarMore(ev) {
  ev.stopPropagation();
  const more = _('modelBulkMore');
  if (more) _setToolbarMoreOpen(!more.classList.contains('open'));
}
document.addEventListener('click', e => {
  const more = _('modelBulkMore');
  if (more && more.classList.contains('open') && !more.contains(e.target)) _setToolbarMoreOpen(false);
});

// 修改模型上下文/最大输出：压缩阈值按此计算，设置后 zen 同步保留该值
let _mmTierWasManual = false;
function showModelMetaEditor(id, ctx, out, cost, tierSignal) {
  _('mmId').value = id;
  _('mmCtx').value = ctx || 200000;
  _('mmOut').value = out || 32768;
  const tier = _('mmTier');
  _mmTierWasManual = tierSignal === 'manual';
  tier.value = _mmTierWasManual ? (cost || 'auto') : 'auto';
  let note = '';
  if (tierSignal && tierSignal !== 'manual') note = t('当前判定') + ': ' + esc(t(_tierSignals[tierSignal] || tierSignal));
  else if (tierSignal === 'manual') note = t('当前判定') + ': ' + t('手动设置');
  else if (cost) note = t('当前判定') + ': ' + (cost === 'free' ? t('免费') : t('付费')) + ' · ' + t('自动');
  _('mmTierNote').textContent = note;
  _('modelMetaEditor').style.display = 'flex';
  try { _('modelMetaEditor').scrollIntoView({ behavior: 'smooth', block: 'nearest' }); } catch (e) {}
}

function hideModelMetaEditor() { _('modelMetaEditor').style.display = 'none'; }

async function saveModelMeta(btn) {
  const id = _('mmId').value;
  const c = parseInt(_('mmCtx').value, 10) || 0;
  const o = parseInt(_('mmOut').value, 10) || 0;
  const tier = _('mmTier').value;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    await api('POST', '/models/context', { id: id, context: c, output: o });
    if (tier !== 'auto') { await api('POST', '/models/cost', { id: id, cost: tier }); }
    else if (_mmTierWasManual) { await api('POST', '/models/cost', { id: id, cost: '' }); }
    toast(t('已保存') + ': ' + id + ' ctx=' + formatTokenCount(c), 'success');
    hideModelMetaEditor();
    await loadModels();
  } catch (e) { toast(t('保存失败') + ': ' + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

let _confirmResolve = null;
function showConfirm(msg) {
  return new Promise(resolve => {
    _confirmResolve = resolve;
    _('confirmMsg').textContent = msg;
    _('confirmModal').style.display = 'flex';
  });
}
function resolveConfirm(val) {
  _('confirmModal').style.display = 'none';
  if (_confirmResolve) { _confirmResolve(val); _confirmResolve = null; }
}

// Esc 逐层关闭弹层：确认 → 模型元信息 → 拉取模型选择器 → 组合测试 → 模型同步（组合选择器的搜索框自带 Esc 处理）
document.addEventListener('keydown', e => {
  if (!e || e.key !== 'Escape') return;
  if (_('confirmModal') && _('confirmModal').style.display === 'flex') { resolveConfirm(false); return; }
  if (_('modelMetaEditor') && _('modelMetaEditor').style.display === 'flex') { hideModelMetaEditor(); return; }
  if (_('provPickerModal') && _('provPickerModal').style.display === 'flex') { closeProvModelPicker(); return; }
  if (_('presetPickerModal') && _('presetPickerModal').style.display === 'flex') { closePresetPicker(); return; }
  if (_('importPreviewModal') && _('importPreviewModal').style.display === 'flex') { closeImportPreview(); return; }
  if (_('connLoginModal') && _('connLoginModal').style.display === 'flex') { closeDeviceLogin(); return; }
  if (_('connImportModal') && _('connImportModal').style.display === 'flex') { hideConnectionImport(); return; }
  if (_('comboTestModal') && _('comboTestModal').style.display === 'flex') { closeComboTest(); return; }
  if (_('modelSyncOverlay') && _('modelSyncOverlay').style.display === 'flex') { closeModelSyncModal(); }
});

function editModelMeta(id, ctx, out, cost, tierSignal) {
  showModelMetaEditor(id, ctx, out, cost, tierSignal);
}

// 模型分组渲染已迁移至 renderModelList（按提供商分组 + 筛选 + 批量启停）

async function loadModels() {
  try {
    const d = await api('GET', '/models');
    const models = d.data.models || [];
    _cachedModels = models;
    renderModelList();
    const ls = d.data.lastSync || {};
    if (_('modelSyncTime')) {
      _('modelSyncTime').innerHTML = formatDateTime(ls.syncedAt);
    }
    // 启动自动同步的变更弹窗（仅提示一次）
    if (!_modelSyncSeen && ls.syncedAt && ls.changed) {
      _modelSyncSeen = true;
      showModelSyncModal(ls);
    }
  } catch (e) { _('modelsList').textContent = t('加载失败'); }
}

async function syncModels() {
  const btn = _('syncModelsBtn');
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('同步中...'); }
  try {
    const d = await api('POST', '/models/sync');
    const res = d.data || {};
    await loadModels();
    if (res.changed) {
      showModelSyncModal(res);
      toast(t('模型列表已更新'), 'success');
    } else {
      toast(t('模型无变化'), 'info');
    }
    if (_('settingDefModel')) await loadConfig();
  } catch (e) {
    toast(t('模型同步失败') + ': ' + (e.message || ''), 'error');
  }
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
}

async function syncOcModels() {
  const btn = _('syncOcModelsBtn');
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('同步中...'); }
  try {
    const d = await api('POST', '/opencode/models/sync');
    const res = d.data || {};
    await loadModels();
    if (res.changed) {
      showModelSyncModal(res);
      toast(t('opencode 模型列表已更新'), 'success');
    } else {
      toast(t('模型无变化'), 'info');
    }
  } catch (e) {
    toast(t('模型同步失败') + ': ' + (e.message || ''), 'error');
  }
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
}

// ========== opencode 免费模型配置 ==========
async function loadOcConfig() {
  try {
    const d = await api('GET', '/opencode/config');
    const c = d.data;
    _('ocEnabled').value = String(!!c.enabled);
    if (c.key) _('ocKey').value = c.key;
    if (c.baseURL) _('ocBaseURL').value = c.baseURL;
    _('ocMaxConcurrency').value = c.maxConcurrency;
    _('ocRetries').value = c.retries;
    _('ocFailover').value = String(!!c.failover);
    _('ocFailoverCount').value = c.failoverCount;
    _('ocFailoverMinutes').value = c.failoverMinutes;
    const cp = c.compaction || {};
    _('ocCompactAuto').value = String(!!cp.auto);
    _('ocCompactBuffer').value = cp.buffer != null ? cp.buffer : 20000;
    _('ocCompactKeepTokens').value = cp.keepTokens != null ? cp.keepTokens : 8000;
    _('ocCompactMaxSummary').value = cp.maxSummary != null ? cp.maxSummary : 4096;
    _('ocProxyStrategy').value = c.proxyStrategy || 'round_robin';
    _('ocProxies').value = (c.proxies || []).join('\n');
    // 运行状态与出口冷却
    const rt = c.runtime || {};
    let status;
    if (!c.enabled) status = '<span style="color:var(--text3)">⏸ ' + t('已停用') + '</span>';
    else if (rt.failoverActive) status = '<span style="color:var(--red)">🔴 ' + t('故障转移中（opencode 暂不可用，请求走 Cline 池）') + '</span>';
    else status = '<span style="color:var(--green)">🟢 ' + t('正常') + '</span>' +
      '<span style="margin-left:8px">' + t('已同步模型') + ': ' + (c.syncedModels || 0) + '</span>';
    _('ocRuntimeStatus').innerHTML = status;
    const cds = c.proxyCooldowns || {};
    const entries = Object.entries(cds);
    _('ocProxyCooldowns').textContent = entries.length
      ? entries.map(([p, until]) => maskProxyForDisplay(p) + ' → ' + until).join('; ')
      : t('无冷却');
  } catch (e) { toast(t('加载失败') + ': ' + e.message, 'error'); }
}

function maskProxyForDisplay(p) {
  return p.replace(/\/\/([^@/]+)@/, '//***@');
}

async function saveOcConfig(btn) {
  const numOr = (id, def) => { const v = parseInt(_(id).value, 10); return isNaN(v) ? def : v; };
  const payload = {
    enabled: _('ocEnabled').value === 'true',
    key: _('ocKey').value.trim(),
    baseURL: _('ocBaseURL').value.trim(),
    maxConcurrency: numOr('ocMaxConcurrency', 8),
    retries: numOr('ocRetries', 3),
    failover: _('ocFailover').value === 'true',
    failoverCount: numOr('ocFailoverCount', 3),
    failoverMinutes: numOr('ocFailoverMinutes', 5),
    proxyStrategy: _('ocProxyStrategy').value,
    proxies: _('ocProxies').value.split('\n').map(s => s.trim()).filter(Boolean),
    compaction: {
      auto: _('ocCompactAuto').value === 'true',
      buffer: numOr('ocCompactBuffer', 20000),
      keepTokens: numOr('ocCompactKeepTokens', 8000),
      maxSummary: numOr('ocCompactMaxSummary', 4096),
    },
  };
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    await api('POST', '/opencode/config/update', payload);
    _('ocSaveResult').innerHTML = '<span style="color:var(--green)">✓ ' + t('opencode 配置已保存') + '</span>';
    setTimeout(() => _('ocSaveResult').innerHTML = '', 5000);
    await loadOcConfig();
  } catch (e) {
    toast(t('保存失败: ') + (e.message || ''), 'error');
  }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

async function loadClineProxyConfig() {
  try {
    const d = await api('GET', '/cline-proxy/config');
    const c = d.data;
    _('clineProxyStrategy').value = c.proxyStrategy || 'round_robin';
    _('clineProxies').value = (c.proxies || []).join('\n');
  } catch (e) { toast(t('加载失败') + ': ' + e.message, 'error'); }
}

async function saveClineProxyConfig(btn) {
  const payload = {
    proxyStrategy: _('clineProxyStrategy').value,
    proxies: _('clineProxies').value.split('\n').map(s => s.trim()).filter(Boolean),
  };
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    await api('POST', '/cline-proxy/config/update', payload);
    _('clineProxySaveResult').innerHTML = '<span style="color:var(--green)">✓ ' + t('Cline 代理配置已保存') + '</span>';
    setTimeout(() => _('clineProxySaveResult').innerHTML = '', 5000);
    await loadClineProxyConfig();
  } catch (e) {
    toast(t('保存失败: ') + (e.message || ''), 'error');
  }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

function showModelSyncModal(res) {
  const ov = _('modelSyncOverlay');
  if (!ov) return;
  const box = _('modelSyncModal');
  const add = (res.added || []).map(m => '<div style="padding:3px 0">+ <span class="mono">' + esc(m) + '</span></div>').join('');
  const rem = (res.removed || []).map(m => '<div style="padding:3px 0;color:var(--text3)">- <span class="mono">' + esc(m) + '</span></div>').join('');
  let body = '';
  if (add) body += '<div style="color:var(--green);font-weight:600;margin-bottom:6px">' + t('新增模型') + '</div>' + add;
  if (rem) body += '<div style="color:var(--text2);font-weight:600;margin:10px 0 6px">' + t('已下架（保留在列表，标记显示）') + '</div>' + rem;
  if (!body) body = '<div style="color:var(--text2)">' + t('模型无变化') + '</div>';
  box.innerHTML =
    '<h2 style="margin:0 0 10px;font-size:18px">' + t('模型同步') + '</h2>' +
    '<div style="font-size:13px;max-height:320px;overflow-y:auto;color:var(--text);line-height:1.6">' + body + '</div>' +
    '<div style="text-align:right;margin-top:16px"><button class="btn btn-primary" onclick="closeModelSyncModal()">OK</button></div>';
  ov.style.display = 'flex';
}

function closeModelSyncModal() {
  const ov = _('modelSyncOverlay');
  if (ov) ov.style.display = 'none';
}

async function addModel(btn) {
  const id = _('newModelId').value.trim();
  const cost = _('newModelCost').value;
  if (!id) { toast(t('请输入模型 ID'), 'error'); return; }
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('添加中...'); }
  try {
    await api('POST', '/models/add', { id, cost });
    toast(t('模型已添加: ') + id, 'success');
    _('newModelId').value = '';
    await loadModels();
    await loadConfig();
  } catch (e) { toast(t('添加失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

async function deleteModel(id) {
  if (!(await showConfirm(t('确认删除模型 ') + id + ' ?'))) return;
  try {
    await api('POST', '/models/delete', { id });
    toast(t('模型已删除: ') + id, 'success');
    await loadModels();
    await loadConfig();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

// ========== 配置加载 ==========
async function loadConfig() {
  try {
    const d = await api('GET', '/config');
    const c = d.data;
    if (c.address) _('settingAddr').value = c.address;
    if (c.strategy) _('settingStrategy').value = c.strategy;
    _('settingModelChain').value = (c.modelChain || []).join(', ');
    if (c.onlyFree !== undefined && _('settingOnlyFree')) _('settingOnlyFree').value = String(c.onlyFree);
    if (c.version) {
      if (_('settingVersion')) _('settingVersion').value = c.version;
      if (_('footerVersion')) _('footerVersion').textContent = c.version;
      if (_('aboutVersion')) _('aboutVersion').textContent = t('版本 ') + c.version;
    }
    if (c.poolPath) _('settingPoolPath').value = c.poolPath;
    if (c.defaultModel !== undefined) {
      const sel = _('settingDefModel');
      // 免费一组、付费一组（optgroup 分类 + 选项标注计费）；
      // 当前默认值不在可用列表时补在开头，避免选中值丢失
      const has = id => (_cachedModels || []).some(m => m.id === id);
      let opts = '';
      if (c.defaultModel && !has(c.defaultModel)) {
        opts += '<option value="' + esc(c.defaultModel) + '" selected>' + esc(c.defaultModel) + esc(t('（当前）')) + '</option>';
      }
      [{ key: 'free', label: t('免费模型'), tag: t('（免费）') },
       { key: 'pass', label: t('付费模型'), tag: t('（付费）') }].forEach(g => {
        // 已停用的模型不进默认模型下拉
        const items = (_cachedModels || []).filter(m => !m.disabled && (m.cost === 'free') === (g.key === 'free'));
        if (!items.length) return;
        opts += '<optgroup label="' + esc(g.label) + ' (' + items.length + ')">' +
          items.map(m => '<option value="' + esc(m.id) + '"' + (m.id === c.defaultModel ? ' selected' : '') + '>' + esc(m.id) + esc(g.tag) + '</option>').join('') +
          '</optgroup>';
      });
      sel.innerHTML = opts || '<option value="">' + t('（无可用模型）') + '</option>';
    }
    // 本机 IP 展示（监听 0.0.0.0 时局域网访问地址）
    const ips = c.localIPs || [];
    if (ips.length) {
      _('localIPsRow').style.display = '';
      _('localIPsList').innerHTML = ips.map(ip =>
        '<span class="model-tag free">' + esc(ip) + '</span>'
      ).join('');
    } else {
      _('localIPsRow').style.display = 'none';
    }
    // 监听地址下拉：补入本机 IP 选项并选中当前值
    const listenSel = _('settingListenHost');
    if (listenSel) {
      if (!listenSel.dataset.inited) {
        listenSel.dataset.inited = '1';
        let opts = '<option value="127.0.0.1">' + t('127.0.0.1（仅本机）') + '</option>';
        opts += '<option value="0.0.0.0">' + t('0.0.0.0（所有网卡）') + '</option>';
        opts += ips.map(ip => '<option value="' + esc(ip) + '">' + esc(ip) + t('（本机网卡）') + '</option>').join('');
        listenSel.innerHTML = opts;
      }
      if (c.host) listenSel.value = c.host;
    }
    // 后台密码状态
    if (c.hasPassword !== undefined) _('passwordStatus').textContent = c.hasPassword ? t('已启用') : t('未启用');
    // 非回环监听安全警告（0.0.0.0 / 局域网 IP 都会暴露管理后台）
    const h = c.host || '';
    const safeHosts = ['', '127.0.0.1', 'localhost', '::1'];
    _('listenWarn').style.display = (safeHosts.indexOf(h) === -1) ? '' : 'none';
    if (c.headers) {
      const tbody = _('headersTableBody');
      tbody.innerHTML = Object.entries(c.headers).map(([k, v]) =>
        '<tr>' +
          '<td><input type="text" class="header-key" value="' + esc(k) + '" style="font-size:12px;font-family:ui-monospace,monospace;width:100%"></td>' +
          '<td><input type="text" class="header-val" value="' + esc(v) + '" style="font-size:12px;font-family:ui-monospace,monospace;width:100%"></td>' +
          '<td><button class="btn btn-sm btn-danger" onclick="this.closest(\'tr\').remove()" title="' + t('移除') + '" aria-label="' + t('移除') + '">✕</button></td>' +
        '</tr>'
      ).join('');
    }
    if (typeof fillTuning === 'function') fillTuning(c);
  } catch (e) { toast(t('加载失败') + ': ' + e.message, 'error'); }
}

