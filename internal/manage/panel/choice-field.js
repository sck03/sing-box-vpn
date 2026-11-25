import { el } from './dom.js';
import { choiceOptions, selectionModel } from './selection-model.js';

let sequence = 0;

// Native checkboxes supply keyboard and screen-reader semantics. No requests
// or global listeners: the enclosing form owns this component's lifetime.
export function choiceField(field, initial, changed) {
  const model = selectionModel(initial, field.maxSelected ?? Infinity);
  const id = `choices-${++sequence}`;
  const trigger = el('button', { type: 'button', class: 'choice-trigger', 'aria-expanded': 'false', 'aria-controls': id });
  const search = el('input', { type: 'search', class: 'choice-search', placeholder: '搜索名称或 ID', 'aria-label': `搜索${field.label}` });
  const status = el('small', { class: 'choice-status', role: 'status', 'aria-live': 'polite' });
  const feedback = el('small', { class: 'choice-feedback', role: 'status' });
  const list = el('div', { class: `choice-list${field.presentation === 'cards' ? ' choice-cards' : ''}` });
  const chosen = el('div', { class: 'choice-selected' });
  const pager = el('div', { class: 'choice-pager' });
  const inline = field.presentation === 'inline';
  const panel = el('div', { id, class: 'choice-panel', hidden: !inline }, search);
  trigger.hidden = inline;
  search.hidden = inline;
  const root = el('div', { class: 'choice-control', role: 'group', 'aria-label': field.label }, trigger, panel);
  const pageSize = field.pageSize || 40;
  let options = [], visibleIDs = null, page = 0, disabled = false, signature = '';
  let onlySelected = false;
  const control = (text, fn, attrs = {}) => el('button', { type: 'button', ...attrs, onclick: fn }, text);
  const selectAll = control('', () => {
    const missing = filtered().filter((option) => !option.disabled && !model.has(option.value));
    const before = model.values().length;
    model.select(missing.map((option) => option.value));
    feedback.textContent = model.values().length - before < missing.length
      ? `已达到 ${field.maxSelected} 项上限，请先取消部分选择。` : '';
    commit();
  });
  const clear = control('清空选择', () => { model.set([]); feedback.textContent = ''; commit(); });
  const selectedFilter = control('仅看已选', () => {
    onlySelected = !onlySelected;
    selectedFilter.setAttribute('aria-pressed', String(onlySelected));
    page = 0; renderList();
  }, { 'aria-pressed': 'false' });
  panel.append(el('div', { class: 'choice-tools' }, selectAll, clear, selectedFilter), status, list, pager, chosen);
  root.append(feedback);

  function filtered() {
    const query = search.value.trim().toLocaleLowerCase();
    return options.filter((option) => (!visibleIDs || visibleIDs.has(option.value)) &&
      (!onlySelected || model.has(option.value)) &&
      (!query || `${option.label} ${option.value} ${option.description || ''}`.toLocaleLowerCase().includes(query)));
  }
  function summary() {
    const selected = model.values();
    const names = new Map(options.map((option) => [option.value, option.label]));
    const labels = selected.slice(0, 2).map((value) => names.get(value) || value);
    trigger.textContent = selected.length ? `${labels.join('、')}${selected.length > 2 ? ` · 另有 ${selected.length - 2} 项` : ''}` : '请选择';
    trigger.title = selected.map((value) => names.get(value) || value).join('、');
    trigger.setAttribute('aria-label', field.label);
  }
  function renderChosen() {
    chosen.replaceChildren();
    if (!field.ordered) return;
    chosen.append(el('strong', {}, '成员顺序（新增成员追加到末尾）'));
    const names = new Map(options.map((option) => [option.value, option.label]));
    model.values().forEach((value, index, values) => {
      const label = names.get(value) || value;
      const move = (step) => {
        model.move(value, step); commit();
        const row = chosen.querySelectorAll('.choice-order-row')[index + step];
        const direction = row?.querySelectorAll('button')[step < 0 ? 0 : 1];
        (direction?.disabled ? row.querySelector('button:not(:disabled)') : direction)?.focus();
      };
      chosen.append(el('div', { class: 'choice-order-row' },
        el('span', {}, `${index + 1}. ${label}`),
        control('↑', () => move(-1), { disabled: disabled || index === 0, 'aria-label': `上移 ${label}` }),
        control('↓', () => move(1), { disabled: disabled || index === values.length - 1, 'aria-label': `下移 ${label}` }),
        control('移除', () => { model.toggle(value, false); commit(); trigger.focus(); }, { disabled, 'aria-label': `移除 ${label}` })));
    });
  }
  function refreshFlags() {
    const selected = model.values(), matches = filtered();
    for (const input of list.querySelectorAll('input')) {
      const option = options.find((option) => option.value === input.value);
      input.checked = model.has(input.value);
      input.disabled = disabled || option.disabled || (!input.checked && selected.length >= (field.maxSelected ?? Infinity));
    }
    const visible = new Set(matches.map((option) => option.value));
    const hidden = selected.filter((id) => !visible.has(id)).length;
    status.textContent = `已选 ${selected.length}${field.maxSelected ? ` / ${field.maxSelected}` : ''} 项 · 当前结果 ${matches.length} 项${hidden ? ` · ${hidden} 项未在当前结果中显示` : ''}`;
    clear.disabled = disabled || !selected.length;
    selectAll.disabled = disabled || selected.length >= (field.maxSelected ?? Infinity) || !matches.some((option) => !option.disabled && !model.has(option.value));
  }
  function renderList() {
    const matches = filtered();
    const pages = Math.max(1, Math.ceil(matches.length / pageSize));
    page = Math.min(page, pages - 1);
    selectAll.textContent = `选择当前筛选结果（${matches.filter((option) => !option.disabled).length}）`;
    list.replaceChildren();
    let group = '';
    for (const option of matches.slice(page * pageSize, (page + 1) * pageSize)) {
      if (option.group && option.group !== group) {
        group = option.group; list.append(el('strong', { class: 'choice-group' }, group));
      }
      const checkbox = el('input', { type: 'checkbox', value: option.value, 'aria-label': option.label });
      checkbox.addEventListener('change', (event) => {
        event.stopPropagation();
        model.toggle(option.value, checkbox.checked);
        feedback.textContent = '';
        // Refresh flags in place so checkbox activation retains keyboard focus.
        summary();
        if (onlySelected && !checkbox.checked) {
          renderList();
          (list.querySelector('input:not(:disabled)') || search).focus();
        } else refreshFlags();
        renderChosen(); changed();
      });
      const text = el('span', { class: 'choice-option-text' }, el('strong', {}, option.label));
      if (option.description || option.value !== option.label) text.append(el('small', {}, option.description || option.value));
      if (option.disabled) text.append(el('small', { class: 'choice-reason' }, option.reason || '当前不可选择'));
      list.append(el('label', { class: 'choice-option', 'data-value': option.value }, checkbox, text));
    }
    if (!matches.length) list.append(el('p', { class: 'help' }, '没有匹配选项，已选内容仍保留。'));
    pager.replaceChildren();
    const turnPage = (next) => {
      page = next; renderList();
      (list.querySelector('input:not(:disabled)') || search).focus();
    };
    if (pages > 1) pager.append(
      control('上一页', () => turnPage(page - 1), { disabled: page === 0 }),
      el('span', {}, `${page + 1} / ${pages}`),
      control('下一页', () => turnPage(page + 1), { disabled: page + 1 === pages }));
    refreshFlags();
  }
  function commit() { summary(); renderList(); renderChosen(); changed(); }
  trigger.addEventListener('click', () => {
    panel.hidden = !panel.hidden;
    trigger.setAttribute('aria-expanded', String(!panel.hidden));
    if (!panel.hidden) { renderList(); renderChosen(); search.focus(); }
  });
  root.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && !inline && !panel.hidden) {
      event.preventDefault(); event.stopPropagation();
      panel.hidden = true; trigger.setAttribute('aria-expanded', 'false'); trigger.focus();
    }
    if (event.target === search && event.key === 'Enter') event.preventDefault();
    if (event.target === search && event.key === 'ArrowDown') {
      event.preventDefault(); list.querySelector('input:not(:disabled)')?.focus();
    }
  });
  search.addEventListener('input', () => { page = 0; renderList(); });
  search.addEventListener('change', (event) => event.stopPropagation());
  summary();
  return {
    root, input: trigger, value: model.values,
    focus() {
      if (inline) list.querySelector('input:not(:disabled)')?.focus();
      else {
        panel.hidden = false; trigger.setAttribute('aria-expanded', 'true'); search.focus();
      }
    },
    setValue(values) { model.set(values); summary(); renderList(); renderChosen(); },
    update(values, visible, loading = false) {
      const next = choiceOptions(values);
      const nextSignature = JSON.stringify([next, visible, loading]);
      if (signature === nextSignature) return;
      signature = nextSignature; options = next;
      visibleIDs = visible == null ? null : new Set(visible.map(String));
      if (!loading) {
        const removed = model.retain(options.filter((option) => !option.disabled).map((option) => option.value));
        feedback.textContent = removed.length ? `已移除 ${removed.length} 个不再可用的选项：${removed.join('、')}` : '';
      }
      page = 0; summary(); renderList(); renderChosen();
    },
    disable(value) {
      if (disabled === value) return;
      disabled = value; trigger.disabled = search.disabled = disabled;
      if (disabled && !inline) { panel.hidden = true; trigger.setAttribute('aria-expanded', 'false'); }
      refreshFlags(); renderChosen();
    },
    validate(required) {
      if (required && !model.values().length) throw new Error(`请选择${field.label}`);
      if (model.values().length > (field.maxSelected ?? Infinity)) throw new Error(`${field.label}最多选择 ${field.maxSelected} 项`);
    },
  };
}
