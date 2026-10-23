// ========== 模型组合（Combo） ==========
var _comboList = [];
var _comboCatalog = [];
var _comboReserved = [];
var _comboEntries = [];
var _editingComboId = '';

// 组合模型来源显示名：provider → 真实 Provider 名；remote → Cline；zen/seed → opencode；其余 → 自定义
function comboSourceName(m) {
  if (m.source === 'provider') return m.provider || t('自定义');
  if (m.source === 'remote') return 'Cline';
  if (m.source === 'zen' || m.source === 'seed') return 'opencode';
  return t('自定义');
}

// loadCombos 拉取组合列表 + 模型目录 + 保留别名；可安全地重复调用
async function loadCombos() {
  try {
    const d = await api('GET', '/combos');
    _comboList = d.data.combos || [];
    _comboCatalog = d.data.modelCatalog || [];
    _comboReserved = d.data.reservedAliases || [];
    renderCombosList();
    // 编辑抽屉打开时目录可能已更新，重绘条目下拉（不丢失当前编辑内容）
    const ed = _('comboEditor');
    if (ed && ed.classList.contains('open')) renderComboEntries();
  } catch (e) {
    const box = _('combosList');
    if (box) box.innerHTML = '<div class="empty">' + t('加载失败') + ': ' + esc(e.message) + '</div>';
  }
}

function comboCatalogEntry(id) {
  return _comboCatalog.find(m => m.id === id) || null;
}

// 组合列表渲染：id / 名称 / 分类 / 条目数 / 状态 / 操作
function renderCombosList() {
  const box = _('combosList');
  if (!box) return;
  const stats = _('combosStats');
  if (stats) {
    if (!_comboList.length) {
      stats.innerHTML = '';
    } else {
      const on = _comboList.filter(c => c.enabled !== false).length;
      const entryCount = _comboList.reduce((n, c) => n + ((c.entries || []).length), 0);
      stats.innerHTML = '<span><b>' + _comboList.length + '</b> ' + t('个组合') + '</span>' +
        '<span><span class="dot g"></span><b>' + on + '</b> ' + t('启用中') + '</span>' +
        '<span><b>' + entryCount + '</b> ' + t('条目总数') + '</span>';
    }
  }
  if (!_comboList.length) {
    box.innerHTML = listEmptyState(ICON_COMBOS, t('暂无模型组合'));
    return;
  }
  box.innerHTML = '<div class="combos-list"><table class="action-table"><thead><tr>' +
    '<th>' + t('组合 ID') + '</th><th>' + t('名称') + '</th><th>' + t('分类') + '</th>' +
    '<th class="num-col">' + t('条目') + '</th><th>' + t('状态') + '</th><th>' + t('操作') + '</th>' +
    '</tr></thead><tbody>' +
    _comboList.map(c => {
      const entries = c.entries || [];
      const enabledEntries = entries.filter(e => !e.disabled).length;
      const badge = c.enabled
        ? '<span class="status active"><span class="status-dot active"></span>' + t('已启用') + '</span>'
        : '<span class="status disabled"><span class="status-dot disabled"></span>' + t('已停用') + '</span>';
      const fb = c.emergencyFallback
        ? ' <span class="model-source-badge provider" title="' + t('允许跳出组合回退') + '">' + t('紧急回退') + '</span>'
        : '';
      return '<tr>' +
        '<td class="mono" style="font-size:12px">' + esc(c.id) + '</td>' +
        '<td>' + esc(c.name || '') + fb + '</td>' +
        '<td>' + (c.category ? esc(c.category) : '<span style="color:var(--text3)">-</span>') + '</td>' +
        '<td class="row-count num-col">' + enabledEntries + ' / ' + entries.length + '</td>' +
        '<td>' + badge + '</td>' +
        '<td><button class="btn btn-sm" onclick="testCombo(\'' + escAttr(c.id) + '\', this)">' + t('测试') + '</button> ' +
            '<button class="btn btn-sm" onclick="editCombo(\'' + escAttr(c.id) + '\')">' + t('编辑') + '</button> ' +
            '<button class="btn btn-sm" onclick="cloneCombo(\'' + escAttr(c.id) + '\', this)">' + t('克隆') + '</button> ' +
            '<button class="btn btn-sm" onclick="toggleCombo(\'' + escAttr(c.id) + '\', ' + !c.enabled + ', this)">' + t(c.enabled ? '停用' : '启用') + '</button> ' +
            '<button class="btn btn-sm btn-danger" onclick="deleteCombo(\'' + escAttr(c.id) + '\', this)" title="' + t('删除') + '" aria-label="' + t('删除') + '">✕</button></td>' +
      '</tr>';
    }).join('') +
    '</tbody></table></div>';
}

function showComboEditor(c) {
  _editingComboId = c ? c.id : '';
  const title = _('comboEditorTitle');
  if (title) title.textContent = c ? t('编辑组合') : t('新建组合');
  _('comboId').value = c ? c.id : '';
  _('comboId').disabled = !!c; // 编辑既有组合时 id 不可改（主键）
  _('comboName').value = c ? (c.name || '') : '';
  _('comboDesc').value = c ? (c.description || '') : '';
  _('comboCategory').value = c ? (c.category || '') : '';
  _('comboEnabled').value = c ? String(c.enabled !== false) : 'true';
  _('comboEmergency').checked = c ? !!c.emergencyFallback : false;
  _comboEntries = c ? (c.entries || []).map(e => ({ model: e.model || '', disabled: !!e.disabled, source: e.source || '', provider: e.provider || '' })) : [];
  _('comboSaveResult').textContent = '';
  renderComboEntries();
  openDrawer('comboEditor');
}

function hideComboEditor() {
  closeDrawer('comboEditor');
  _editingComboId = '';
  _comboEntries = [];
}

// 组合下拉/条目标签：id · <来源名> · 免费/付费（不重复显示 Provider 名）
function comboModelLabel(m) {
  return m.id + '  ·  ' + comboSourceName(m) + '  ·  ' + (m.cost === 'free' ? t('免费') : t('付费'));
}

// 组合下拉分组：按来源归组（Provider 用真实名）
function comboOptgroupKey(m) {
  if (m.source === 'provider') return 'prov:' + (m.provider || 'Provider');
  if (m.source === 'remote') return 'remote';
  if (m.source === 'zen' || m.source === 'seed') return 'zen';
  return 'custom';
}
function comboGroupSortKey(a, b) {
  const ord = k => k === 'remote' ? 0 : (k === 'zen' ? 1 : (k.indexOf('prov:') === 0 ? 2 : 3));
  const oa = ord(a), ob = ord(b);
  if (oa !== ob) return oa - ob;
  return a.localeCompare(b);
}

// 构建模型下拉：按来源分 <optgroup>；排除管理页已停用的模型；当前条目即使停用/缺失也始终保留为选中项
function buildComboModelOptions(current, chosenMap) {
  let html = '<option value=""' + (!current ? ' selected' : '') + '>' + t('— 选择模型 —') + '</option>';
  const cur = current ? comboCatalogEntry(current) : null;
  // 保留当前引用：停用标记为「模型已停用」，不在目录中标记为「目录中未找到」，绝不静默丢弃
  if (current && (!cur || cur.disabled)) {
    const label = cur
      ? esc(comboModelLabel(cur)) + '  ·  ' + t('模型已停用')
      : esc(current) + '  ·  ' + t('目录中未找到');
    html += '<option value="' + escAttr(current) + '" selected>' + label + '</option>';
  }
  const groups = new Map(); // groupKey -> { label, items[] }
  const seen = {};
  for (const m of _comboCatalog) {
    if (m.disabled) continue;         // 已停用模型不可选
    if (seen[m.id]) continue;
    seen[m.id] = true;
    const gk = comboOptgroupKey(m);
    if (!groups.has(gk)) groups.set(gk, { label: comboSourceName(m), items: [] });
    groups.get(gk).items.push(m);
  }
  const gkeys = Array.from(groups.keys()).sort(comboGroupSortKey);
  for (const gk of gkeys) {
    const g = groups.get(gk);
    html += '<optgroup label="' + escAttr(g.label) + '">';
    g.items.sort((a, b) => a.id.localeCompare(b.id));
    for (const m of g.items) {
      const used = chosenMap[m.id] && m.id !== current;
      html += '<option value="' + escAttr(m.id) + '"' + (m.id === current ? ' selected' : '') + (used ? ' disabled' : '') + '>' +
        esc(comboModelLabel(m)) + (used ? '  ·  ' + t('已使用') : '') + '</option>';
    }
    html += '</optgroup>';
  }
  return html;
}

// 选中模型的小徽章：来源 / 提供商 / 计费 / 停用 / 下架
function comboEntryBadges(id) {
  if (!id) return '<span style="font-size:11px;color:var(--text3)">' + t('未选择模型') + '</span>';
  const m = comboCatalogEntry(id);
  if (!m) return '<span class="delisted-badge" style="border-color:var(--yellow);color:var(--yellow)">' + t('目录中未找到') + '</span>';
  let b = '';
  let srcKey = 'custom';
  if (m.source === 'provider') srcKey = 'provider';
  else if (m.source === 'remote') srcKey = 'remote';
  else if (m.source === 'zen' || m.source === 'seed') srcKey = 'zen';
  b += '<span class="model-source-badge ' + srcKey + '">' + esc(comboSourceName(m)) + '</span>';
  if (m.provider && m.source !== 'provider') b += '<span class="model-source-badge">' + esc(m.provider) + '</span>';
  b += m.cost === 'free'
    ? '<span class="model-tag free" style="padding:1px 7px;font-size:10px;margin:0">' + t('免费') + '</span>'
    : '<span class="model-tag pass" style="padding:1px 7px;font-size:10px;margin:0">' + t('付费') + '</span>';
  if (m.disabled) b += '<span class="delisted-badge" style="border-color:var(--yellow);color:var(--yellow)">' + t('模型已停用') + '</span>';
  if (m.delisted) b += '<span class="delisted-badge">' + t('已下架') + '</span>';
  return b;
}

// 目录行 → 上游 pin 元组（与后端 source 取值一致：cline / zen / provider）
function comboCatalogPin(m) {
  if (m.source === 'provider') return { source: 'provider', provider: m.provider || '' };
  if (m.source === 'zen' || m.source === 'seed') return { source: 'zen', provider: '' };
  return { source: 'cline', provider: '' }; // remote 或 ""
}

function comboPinKey(pin) {
  return (pin && pin.source ? pin.source : '') + '|' + ((pin && pin.provider) ? pin.provider : '');
}

function comboPinLabel(pin) {
  if (!pin || !pin.source) return '';
  if (pin.source === 'zen') return 'opencode';
  if (pin.source === 'cline') return 'Cline';
  return pin.provider || t('自定义');
}

// 同一模型 ID 在目录中可用的去重上游列表（按 pin 元组去重；<2 个时不需选择器）
function comboEntryUpstreams(modelId) {
  if (!modelId) return [];
  const map = new Map();
  for (const m of _comboCatalog) {
    if (m.id !== modelId) continue;
    const pin = comboCatalogPin(m);
    const k = comboPinKey(pin);
    if (!map.has(k)) map.set(k, pin);
  }
  const pins = Array.from(map.values());
  const ord = p => p.source === 'cline' ? 0 : (p.source === 'zen' ? 1 : 2);
  pins.sort((a, b) => {
    const oa = ord(a), ob = ord(b);
    return oa !== ob ? oa - ob : comboPinLabel(a).localeCompare(comboPinLabel(b));
  });
  return pins;
}

// 仅当该模型有多个上游时渲染「上游」选择器；value: "" 自动 | cline | zen | provider|<Name>
function comboSourceSelectHtml(i, e) {
  const ups = comboEntryUpstreams(e.model);
  if (ups.length < 2) return '';
  const curKey = comboPinKey({ source: e.source, provider: e.provider });
  let opts = '<option value=""' + (!e.source ? ' selected' : '') + '>' + t('自动') + '</option>';
  for (const p of ups) {
    const val = p.source === 'provider' ? ('provider|' + p.provider) : p.source;
    opts += '<option value="' + escAttr(val) + '"' + (comboPinKey(p) === curKey ? ' selected' : '') + '>' + esc(comboPinLabel(p)) + '</option>';
  }
  return '<span class="combo-entry-source"><select title="' + t('上游') + '" onchange="onComboEntrySourceChange(' + i + ', this.value)">' + opts + '</select></span>';
}

// 条目编辑器渲染 + 优先级预览
// 行结构：拖拽手柄 / 优先级序号 / 模型下拉 / 上游钉选 / 徽章 / 启停与排序操作
// 拖拽：以 ⠿ 手柄作为 HTML5 drag 源，行本身作为落点（dragover/drop），落点用
//      inset 指示线 + 序号 + 优先级预览同步"模拟落位"后的新序号（live preview）。
function renderComboEntries() {
  const box = _('comboEntries');
  if (!box) return;
  if (!_comboEntries.length) {
    box.innerHTML = '<div class="combo-empty-entries">' + t('尚未添加条目，点击下方「添加条目」开始。') + '</div>';
    renderComboPreview();
    return;
  }
  const chosen = {};
  _comboEntries.forEach(e => { if (e.model) chosen[e.model] = true; });
  box.innerHTML = _comboEntries.map((e, i) => {
    const opts = buildComboModelOptions(e.model, chosen);
    const dis = e.disabled ? ' disabled-entry' : '';
    return '<div class="combo-entry' + dis + '" data-entry-idx="' + i + '" ondragover="comboDragOver(event, ' + i + ')" ondrop="comboDrop(event, ' + i + ')">' +
      '<span class="combo-entry-grip" draggable="true" ondragstart="comboDragStart(event, ' + i + ')" ondragend="comboDragEnd(event)" title="' + t('拖动调整顺序') + '" role="button" tabindex="0" aria-label="' + t('拖动调整顺序') + '">⠿</span>' +
      '<span class="combo-entry-idx">' + (i + 1) + '</span>' +
      '<span class="combo-entry-select"><select onchange="onComboEntryModelChange(' + i + ', this.value)">' + opts + '</select></span>' +
      comboSourceSelectHtml(i, e) +
      '<span class="combo-entry-badges">' + comboEntryBadges(e.model) + '</span>' +
      '<span class="combo-entry-actions">' +
        '<label class="switch" title="' + t('启用/停用该条目') + '"><input type="checkbox"' + (e.disabled ? '' : ' checked') + ' onchange="onComboEntryDisabled(' + i + ', !this.checked)"><span class="slider"></span></label>' +
        '<button class="btn btn-sm" onclick="moveComboEntry(' + i + ', -1)"' + (i === 0 ? ' disabled' : '') + ' title="' + t('上移') + '" aria-label="' + t('上移') + '">↑</button>' +
        '<button class="btn btn-sm" onclick="moveComboEntry(' + i + ', 1)"' + (i === _comboEntries.length - 1 ? ' disabled' : '') + ' title="' + t('下移') + '" aria-label="' + t('下移') + '">↓</button>' +
        '<button class="btn btn-sm btn-danger" onclick="removeComboEntry(' + i + ')" title="' + t('移除') + '" aria-label="' + t('移除') + '">✕</button>' +
      '</span>' +
    '</div>';
  }).join('');
  renderComboPreview();
}

// 优先级预览：order 传入原下标数组时显示"模拟落位"后的顺序（拖拽 live preview 用）
function renderComboPreview(order) {
  const box = _('comboPreview');
  if (!box) return;
  const idxs = order || _comboEntries.map((_, k) => k);
  const enabled = idxs.filter(k => { const e = _comboEntries[k]; return e && !e.disabled && e.model; });
  if (!enabled.length) {
    box.innerHTML = '<span style="color:var(--text3)">' + t('（暂无启用的条目）') + '</span>';
    return;
  }
  box.innerHTML = enabled.map((k, i) =>
    '<span class="step">' + (i + 1) + '. ' + esc(_comboEntries[k].model) + '</span>'
  ).join('<span class="arrow">→</span>');
}

function addComboEntry() {
  _comboEntries.push({ model: '', disabled: false, source: '', provider: '' });
  renderComboEntries();
}

function removeComboEntry(i) {
  if (i < 0 || i >= _comboEntries.length) return;
  _comboEntries.splice(i, 1);
  renderComboEntries();
}

function onComboEntryModelChange(i, val) {
  const e = _comboEntries[i];
  if (!e) return;
  e.model = val;
  // 新模型若不再提供当前 pin 的上游，则清空为「自动」，避免残留无效钉选
  if (e.source) {
    const key = comboPinKey({ source: e.source, provider: e.provider });
    const stillValid = comboEntryUpstreams(val).some(p => comboPinKey(p) === key);
    if (!stillValid) { e.source = ''; e.provider = ''; }
  }
  renderComboEntries();
}

// 上游选择变化："" 清空；cline/zen 设定来源；provider|<Name> 设定 Provider 来源
function onComboEntrySourceChange(i, value) {
  const e = _comboEntries[i];
  if (!e) return;
  if (value === 'cline' || value === 'zen') {
    e.source = value; e.provider = '';
  } else if (value && value.indexOf('provider|') === 0) {
    e.source = 'provider'; e.provider = value.slice(9);
  } else {
    e.source = ''; e.provider = '';
  }
  renderComboPreview();
}

function onComboEntryDisabled(i, disabled) {
  if (_comboEntries[i]) _comboEntries[i].disabled = !!disabled;
  renderComboEntries();
}

function moveComboEntry(i, dir) {
  const j = i + dir;
  if (j < 0 || j >= _comboEntries.length) return;
  const tmp = _comboEntries[i];
  _comboEntries[i] = _comboEntries[j];
  _comboEntries[j] = tmp;
  renderComboEntries();
}

// ========== 组合条目拖拽排序（HTML5 drag & drop；↑↓ 按钮为无障碍兜底） ==========
let _comboDragFrom = -1; // 拖起条目的原下标
let _comboDragTo = -1;   // 最近 preview 的插入位置（移除原位后的坐标系，0..len）

function comboDragStart(ev, i) {
  _comboDragFrom = i;
  _comboDragTo = -1;
  if (ev.dataTransfer) {
    ev.dataTransfer.effectAllowed = 'move';
    try { ev.dataTransfer.setData('text/plain', String(i)); } catch (e) {}
  }
  const row = ev.target && ev.target.closest ? ev.target.closest('.combo-entry') : null;
  if (row) row.classList.add('combo-dragging');
}

// 计算插入位置：光标在落点行上半 → 插到该行前，否则插到该行后；
// 若拖起项在插入点之前，需先扣除自身占位。
function comboDragInsert(ev, overIdx) {
  if (_comboDragFrom < 0 || overIdx < 0 || overIdx >= _comboEntries.length) return -1;
  const rect = ev.currentTarget.getBoundingClientRect();
  const before = ev.clientY < rect.top + rect.height / 2;
  let ins = before ? overIdx : overIdx + 1;
  if (_comboDragFrom < ins) ins--;
  return ins;
}

// 模拟"移除 from，插入 ins"后原下标的新顺序（0..len-1）
function comboSimulatedOrder(ins) {
  const arr = _comboEntries.map((_, k) => k);
  arr.splice(_comboDragFrom, 1);
  arr.splice(Math.max(0, Math.min(arr.length, ins)), 0, _comboDragFrom);
  return arr;
}

// live preview：inset 指示线 + 各行序号 + 优先级预览同步为模拟落位后的顺序
function comboDragPreview(hoverRow, before, ins) {
  const box = _('comboEntries');
  if (!box) return;
  const rows = box.querySelectorAll('.combo-entry');
  rows.forEach(r => r.classList.remove('drop-before', 'drop-after'));
  if (hoverRow) hoverRow.classList.add(before ? 'drop-before' : 'drop-after');
  const order = comboSimulatedOrder(ins);
  const pos = new Array(_comboEntries.length);
  order.forEach((origIdx, p) => { pos[origIdx] = p; });
  rows.forEach(r => {
    const badge = r.querySelector('.combo-entry-idx');
    const oi = Number(r.getAttribute('data-entry-idx'));
    if (badge && oi >= 0 && Number.isFinite(pos[oi])) badge.textContent = pos[oi] + 1;
  });
  renderComboPreview(order);
}

function comboDragOver(ev, i) {
  ev.preventDefault();
  if (ev.dataTransfer) ev.dataTransfer.dropEffect = 'move';
  if (_comboDragFrom < 0) return;
  const rect = ev.currentTarget.getBoundingClientRect();
  const before = ev.clientY < rect.top + rect.height / 2;
  const ins = comboDragInsert(ev, i);
  if (ins < 0) return;
  _comboDragTo = ins;
  comboDragPreview(ev.currentTarget, before, ins);
}

function comboDrop(ev, i) {
  ev.preventDefault();
  if (ev.dataTransfer) ev.dataTransfer.dropEffect = 'move';
  let ins = _comboDragTo;
  if (_comboDragFrom >= 0 && (ins < 0 || ins === null || ins === undefined)) ins = comboDragInsert(ev, i);
  if (_comboDragFrom >= 0 && ins >= 0) {
    const item = _comboEntries.splice(_comboDragFrom, 1)[0];
    _comboEntries.splice(Math.max(0, Math.min(_comboEntries.length, ins)), 0, item);
    toast(t('已调整顺序'), 'success');
    renderComboEntries(); // 重建行：序号 / 上移下移按钮禁用态 / 预览全部复位
  }
  _comboDragFrom = -1;
  _comboDragTo = -1;
}

// dragend 兜底（取消拖拽 / 松手感复位）；成功 drop 后行已重建，此处为幂等清理
function comboDragEnd() {
  const box = _('comboEntries');
  if (box) box.querySelectorAll('.combo-entry').forEach(r => r.classList.remove('combo-dragging', 'drop-before', 'drop-after'));
  if (_comboDragFrom >= 0) renderComboEntries();
  _comboDragFrom = -1;
  _comboDragTo = -1;
}

// ========== 组合：多选模型选择器（从 /admin/api/combos 的 modelCatalog 取数） ==========
var _pickerOrder = []; // 勾选顺序（model id，先勾先排；确认后即 entries 追加顺序）

function openComboModelPicker() {
  if (!_comboCatalog || !_comboCatalog.length) {
    toast(t('模型目录为空，请先同步模型'), 'error');
    return;
  }
  _pickerOrder = [];
  _('comboPickerSearch').value = '';
  const editing = _comboEntries.some(e => e.model);
  _('comboPickerReplaceBtn').style.display = editing ? '' : 'none';
  renderComboPickerList();
  _('comboModelPicker').style.display = 'flex';
  try { _('comboPickerSearch').focus(); } catch (e) {}
}

function closeComboModelPicker() {
  _('comboModelPicker').style.display = 'none';
  _pickerOrder = [];
}

function comboPickerToggle(model, checked) {
  const idx = _pickerOrder.indexOf(model);
  if (checked && idx === -1) _pickerOrder.push(model);
  if (!checked && idx !== -1) _pickerOrder.splice(idx, 1);
  renderComboPickerList();
}

// 工具条「全选」：把当前筛选结果里所有可选模型按显示顺序补进勾选序列
function comboPickerSelectAllShown() {
  comboPickerVisibleModels().forEach(m => {
    if (_pickerOrder.indexOf(m.id) === -1) _pickerOrder.push(m.id);
  });
  renderComboPickerList();
}

function comboPickerClear() {
  _pickerOrder = [];
  renderComboPickerList();
}

// 当前筛选条件下可展示的模型（跳过管理页已停用；与条目下拉同一条规则）
function comboPickerVisibleModels() {
  const q = (_('comboPickerSearch').value || '').trim().toLowerCase();
  const rx = q.replace(/[.*+\-?^${}()|[\]\\]/g, '\\$&');
  return _comboCatalog.filter(m => !m.disabled && (!rx || (m.id + ' ' + (m.provider || '')).toLowerCase().search(rx) !== -1));
}

// 已在组合中的模型不允许重复勾选（标记「已添加」且复选框禁用）
function renderComboPickerList() {
  const box = _('comboPickerList');
  if (!box) return;
  const scrollTop = box.scrollTop;
  const inCombo = {};
  _comboEntries.forEach(e => { if (e.model) inCombo[e.model] = true; });
  const visible = comboPickerVisibleModels();
  if (!visible.length) {
    box.innerHTML = '<div class="empty">' + t('没有符合条件的模型') + '</div>';
    renderComboPickerCount();
    return;
  }
  // 按来源归组（与条目下拉一致：Provider 用真实名）
  const groups = new Map();
  const seenSrc = {}; // 同一模型多上游时只展示一次
  for (const m of visible) {
    const gk = comboOptgroupKey(m);
    if (!groups.has(gk)) groups.set(gk, { label: comboSourceName(m), pin: comboPinKey(comboCatalogPin(m)), items: [] });
    const g = groups.get(gk);
    if (!g.pin || !seenSrc[g.pin + '|' + m.id]) {
      seenSrc[g.pin + '|' + m.id] = true;
      g.items.push(m);
    }
  }
  const gkeys = Array.from(groups.keys()).sort(comboGroupSortKey);
  let html = '';
  for (const gk of gkeys) {
    const g = groups.get(gk);
    html += '<div class="combo-pick-group">' + esc(g.label) + '</div>';
    g.items.sort((a, b) => a.id.localeCompare(b.id));
    for (const m of g.items) {
      const rank = _pickerOrder.indexOf(m.id);
      const used = inCombo[m.id];
      html += '<label class="combo-pick-row' + (used ? ' combo-pick-used' : '') + '">' +
        '<input type="checkbox"' + (rank !== -1 ? ' checked' : '') + (used ? ' disabled' : '') + ' onchange="comboPickerToggle(\'' + escAttr(m.id) + '\', this.checked)">' +
        '<span class="combo-pick-id">' + esc(m.id) + '</span>' +
        '<span class="model-source-badge ' + (m.source === 'provider' ? 'provider' : (m.source === 'zen' || m.source === 'seed' ? 'zen' : (m.source === 'remote' ? 'remote' : 'custom'))) + '">' + esc(comboSourceName(m)) + '</span>' +
        (m.cost === 'free'
          ? '<span class="model-tag free" style="padding:1px 7px;font-size:10px;margin:0">' + t('免费') + '</span>'
          : '<span class="model-tag pass" style="padding:1px 7px;font-size:10px;margin:0">' + t('付费') + '</span>') +
        (used ? '<span class="model-source-badge" title="' + t('已在组合条目中，无需重复添加') + '">' + t('已添加') + '</span>' : '') +
        (rank !== -1 ? '<span class="combo-pick-rank" title="' + t('勾选顺序即尝试优先级') + '">#' + (rank + 1) + '</span>' : '') +
      '</label>';
    }
  }
  box.innerHTML = html;
  box.scrollTop = scrollTop;
  renderComboPickerCount();
}

function renderComboPickerCount() {
  const el = _('comboPickerCount');
  if (!el) return;
  const n = _pickerOrder.length;
  el.textContent = t('已选 ') + n + ' ' + t('个模型');
}

// 确认：append=false 追加到现有条目之后；append=true（替换整表）用勾选顺序重构全部条目
function comboPickerConfirm(replace) {
  if (!_pickerOrder.length) { toast(t('请先勾选至少一个模型'), 'error'); return; }
  if (replace) {
    _comboEntries = _pickerOrder.map(id => ({ model: id, disabled: false, source: '', provider: '' }));
  } else {
    for (const id of _pickerOrder) {
      if (_comboEntries.some(e => e.model === id)) continue; // 防重复：已在组合中的跳过
      _comboEntries.push({ model: id, disabled: false, source: '', provider: '' });
    }
  }
  closeComboModelPicker();
  renderComboEntries();
  toast(replace ? t('已按勾选顺序替换全部条目') : t('已按勾选顺序追加 ') + _pickerOrder.length + t(' 个条目'), 'success');
}

async function saveCombo() {
  const id = _('comboId').value.trim();
  const name = _('comboName').value.trim();
  const res = _('comboSaveResult');
  res.textContent = '';
  if (!id) { toast(t('请填写组合 ID'), 'error'); return; }
  if (!name) { toast(t('请填写名称'), 'error'); return; }
  if (!_editingComboId && _comboReserved.indexOf(id) !== -1) { toast(t('该 ID 为保留别名，请更换'), 'error'); return; }
  const entries = _comboEntries.filter(e => e.model).map(e => {
    const out = { model: e.model, disabled: !!e.disabled };
    if (e.source) {
      out.source = e.source;
      if (e.source === 'provider' && e.provider) out.provider = e.provider;
    }
    return out;
  });
  const seen = {};
  for (const e of entries) {
    if (seen[e.model]) { toast(t('条目中存在重复模型: ') + e.model, 'error'); return; }
    seen[e.model] = true;
  }
  const payload = {
    id: id,
    name: name,
    description: _('comboDesc').value.trim(),
    category: _('comboCategory').value.trim(),
    enabled: _('comboEnabled').value === 'true',
    emergencyFallback: _('comboEmergency').checked,
    entries: entries,
  };
  try {
    await api('POST', '/combos/save', payload);
    toast(t('组合已保存'), 'success');
    hideComboEditor();
    await loadCombos();
  } catch (e) {
    res.innerHTML = '<span style="color:var(--red)">✗ ' + esc(e.message) + '</span>';
    toast(t('保存失败: ') + e.message, 'error');
  }
}

function editCombo(id) {
  const c = _comboList.find(x => x.id === id);
  if (c) showComboEditor(c);
}

async function toggleCombo(id, enabled, btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    await api('POST', '/combos/toggle', { id: id, enabled: !!enabled });
    toast(t(enabled ? '已启用' : '已停用') + ': ' + id, 'success');
    await loadCombos();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

async function deleteCombo(id, btn) {
  if (!(await showConfirm(t('确定删除该组合？')))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('删除中...'); }
  try {
    await api('POST', '/combos/delete', { id: id });
    toast(t('已删除'), 'success');
    if (_editingComboId === id) hideComboEditor();
    await loadCombos();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

async function cloneCombo(id, btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('克隆中...'); }
  try {
    await api('POST', '/combos/clone', { id: id });
    toast(t('已克隆'), 'success');
    await loadCombos();
  } catch (e) { toast(t('克隆失败: ') + e.message, 'error'); }
  finally { if (btn) { btn.disabled = false; btn.innerHTML = orig; } }
}

// ========== 组合连通性测试（/combos/test 纯只读探测 + 结果内启停/移除工作副本） ==========
let _comboTestCopy = null;    // 正在测试的组合的工作副本（结构同 _comboList 条目，保存前不落盘）
let _comboTestChanged = false;
let _comboTestResults = [];   // 服务端返回的逐条目结果（仅含启用条目，顺序与条目一致）

async function testCombo(id, btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('测试中...'); }
  try {
    const c = _comboList.find(x => x.id === id);
    if (!c) { toast(t('未找到该组合'), 'error'); return; }
    const d = await api('POST', '/combos/test', { id: id });
    const r = d.data || {};
    // 工作副本：所有行内启停/移除只改这里，保存更改后才经既存 /combos/save 流落盘
    _comboTestCopy = {
      id: c.id, name: c.name || '', description: c.description || '', category: c.category || '',
      enabled: c.enabled !== false, emergencyFallback: !!c.emergencyFallback,
      entries: (c.entries || []).map(e => ({ model: e.model || '', disabled: !!e.disabled, source: e.source || '', provider: e.provider || '' })),
    };
    _comboTestChanged = false;
    _comboTestResults = r.results || [];
    renderComboTestModal();
    _('comboTestModal').style.display = 'flex';
  } catch (e) {
    // 400（组合无启用条目）等业务错误原样透出；404/接口缺失 → 重启提示
    const box = _('comboTestList');
    if (box) box.innerHTML = '<div class="empty" style="color:var(--red)">✗ ' + esc(friendlyTestError(e)) + '</div>';
    const summary = _('comboTestSummary');
    if (summary) summary.style.display = 'none';
    const saveBtn = _('comboTestSaveBtn');
    if (saveBtn) saveBtn.style.display = 'none';
    const resLbl = _('comboTestResult');
    if (resLbl) resLbl.textContent = '';
    const idLbl = _('comboTestId');
    if (idLbl) idLbl.textContent = '';
    _comboTestCopy = null;
    _comboTestChanged = false;
    _comboTestResults = [];
    _('comboTestModal').style.display = 'flex';
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  }
}

// 结果与条目关联：结果仅含启用条目且与条目同序 → 依次配对；配对不上（停用/为空）显示为跳过
function comboTestResultDetails() {
  const details = [];
  let ri = 0;
  for (const e of (_comboTestCopy ? _comboTestCopy.entries : [])) {
    if (e.model && !e.disabled && ri < _comboTestResults.length && _comboTestResults[ri].model === e.model) {
      details.push({ entry: e, res: _comboTestResults[ri] });
      ri++;
    } else {
      details.push({ entry: e, res: null });
    }
  }
  return details;
}

// 复用 comboPinLabel 的上游-pin 文案：结果自带 upstream（"provider"|"opencode"|"cline"）
function comboTestUpstreamBadge(upstream) {
  if (!upstream) return '';
  const map = { opencode: 'zen', cline: 'remote', provider: 'provider' };
  const label = upstream === 'opencode' ? 'opencode' : (upstream === 'cline' ? 'Cline' : 'Provider');
  return '<span class="model-source-badge ' + (map[upstream] || '') + '" title="' + t('上游') + '">' + esc(label) + '</span>';
}

function comboTestSourcePin(e, res) {
  const src = res && res.source ? res.source : (e.source || '');
  if (src === 'provider') return '<span class="model-source-badge provider">' + esc((res && res.provider) || e.provider || 'Provider') + '</span>';
  if (src === 'zen') return '<span class="model-source-badge zen">opencode</span>';
  if (src === 'cline') return '<span class="model-source-badge remote">Cline</span>';
  return '<span class="model-source-badge" title="' + t('自动') + '">' + t('自动') + '</span>';
}

function renderComboTestModal() {
  const box = _('comboTestList');
  if (!box) return;
  const idLbl = _('comboTestId');
  if (idLbl) idLbl.textContent = _comboTestCopy ? _comboTestCopy.id : '';
  // 摘要头：全绿 → 紧凑 ✓ 徽章；有失败 → 失败计数徽章（明细在行内）
  const allOk = _comboTestResults.length > 0 && _comboTestResults.every(r => !!r.ok);
  const summary = _('comboTestSummary');
  if (summary) {
    const fails = _comboTestResults.filter(r => !r.ok).length;
    if (!_comboTestResults.length) {
      summary.style.display = 'none';
    } else if (allOk) {
      summary.textContent = t('✓ 全部通过');
      summary.className = 'model-source-badge zen';
      summary.style.display = '';
    } else {
      summary.textContent = fails + ' ✗ ' + t('失败');
      summary.className = 'model-source-badge remote';
      summary.style.display = '';
    }
  }
  const resLbl = _('comboTestResult');
  if (resLbl) resLbl.textContent = '';
  const saveBtn = _('comboTestSaveBtn');
  if (saveBtn) saveBtn.style.display = _comboTestChanged ? '' : 'none';
  const details = comboTestResultDetails();
  if (!details.length) {
    box.innerHTML = '<div class="empty">' + t('（暂无启用的条目）') + '</div>';
    return;
  }
  box.innerHTML = details.map((d, i) => {
    const e = d.entry, res = d.res;
    const verdict = !res
      ? '<span class="combo-test-verdict" style="color:var(--text3)">—</span>'
      : (res.ok
        ? '<span class="combo-test-verdict" style="color:var(--green)">✓ <span>' + esc((res.durationMs || 0) + 'ms') + '</span></span>'
        : '<span class="combo-test-verdict" style="color:var(--red)">✗</span>');
    const errHtml = res && !res.ok
      ? '<span class="combo-test-error" title="' + escAttr(res.error || '') + '">' + esc(res.error || '') + '</span>'
      : '';
    // 移除只对已测失败的条目开放（保存确认前仅改工作副本，取消即放弃）
    const removeBtn = res && !res.ok
      ? '<button class="btn btn-sm btn-danger" onclick="comboTestRemoveEntry(' + i + ')" title="' + t('移除') + '" aria-label="' + t('移除') + '">✕</button>'
      : '';
    return '<div class="combo-test-row' + (e.disabled ? ' combo-test-disabled' : '') + '">' +
      '<span class="combo-entry-idx">' + (i + 1) + '</span>' +
      '<span class="combo-pick-id" style="flex:1 1 160px">' + esc(e.model) + '</span>' +
      comboTestSourcePin(e, res) +
      (res ? comboTestUpstreamBadge(res.upstream) : '') +
      verdict + errHtml +
      '<span class="combo-entry-actions">' +
        '<label class="switch" title="' + t('启用/停用该条目') + '"><input type="checkbox"' + (e.disabled ? '' : ' checked') + ' onchange="comboTestToggleEntry(' + i + ', !this.checked)"><span class="slider"></span></label>' +
        removeBtn +
      '</span>' +
    '</div>';
  }).join('');
}

function comboTestToggleEntry(i, disabled) {
  if (!_comboTestCopy || !_comboTestCopy.entries[i]) return;
  _comboTestCopy.entries[i].disabled = !!disabled;
  _comboTestChanged = true;
  renderComboTestModal();
}

function comboTestRemoveEntry(i) {
  if (!_comboTestCopy || !_comboTestCopy.entries[i]) return;
  _comboTestCopy.entries.splice(i, 1);
  _comboTestChanged = true;
  renderComboTestModal();
}

function closeComboTest() {
  const m = _('comboTestModal');
  if (m) m.style.display = 'none';
  _comboTestCopy = null;
  _comboTestChanged = false;
  _comboTestResults = [];
}

// 保存更改：仅在有修改时可用；走既存保存接口（/combos/save，与 saveCombo 同一载荷结构）
async function comboTestSave(btn) {
  if (!_comboTestCopy || !_comboTestChanged) { closeComboTest(); return; }
  const entries = _comboTestCopy.entries.filter(e => e.model).map(e => {
    const out = { model: e.model, disabled: !!e.disabled };
    if (e.source) {
      out.source = e.source;
      if (e.source === 'provider' && e.provider) out.provider = e.provider;
    }
    return out;
  });
  const payload = {
    id: _comboTestCopy.id,
    name: _comboTestCopy.name,
    description: _comboTestCopy.description,
    category: _comboTestCopy.category,
    enabled: _comboTestCopy.enabled,
    emergencyFallback: _comboTestCopy.emergencyFallback,
    entries: entries,
  };
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('保存中...'); }
  try {
    await api('POST', '/combos/save', payload);
    toast(t('组合已保存'), 'success');
    closeComboTest();
    await loadCombos();
  } catch (e) {
    const resLbl = _('comboTestResult');
    if (resLbl) resLbl.innerHTML = '<span style="color:var(--red)">✗ ' + esc(e.message) + '</span>';
    toast(t('保存失败: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  }
}

