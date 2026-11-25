// Filtering and paging never change selection. Existing order is preserved;
// new members append, so editing a group cannot silently change its default.
export function selectionModel(initial = [], limit = Infinity) {
  let selected = [...new Set(initial.map(String))];
  return {
    values: () => [...selected],
    has: (id) => selected.includes(String(id)),
    set(values) { selected = [...new Set(values.map(String))]; },
    toggle(id, checked) {
      id = String(id);
      if (!checked) selected = selected.filter((value) => value !== id);
      else if (!selected.includes(id) && selected.length < limit) selected.push(id);
    },
    select(ids) { for (const id of ids) this.toggle(id, true); },
    retain(ids) {
      const available = new Set(ids.map(String));
      const removed = selected.filter((id) => !available.has(id));
      selected = selected.filter((id) => available.has(id));
      return removed;
    },
    move(id, step) {
      const index = selected.indexOf(id), next = index + step;
      if (index < 0 || next < 0 || next >= selected.length) return;
      [selected[index], selected[next]] = [selected[next], selected[index]];
    },
  };
}

export function choiceOptions(options = []) {
  return options.map((option) => {
    if (Array.isArray(option)) return { value: String(option[0]), label: String(option[1]) };
    if (typeof option === 'object') return { ...option, value: String(option.value), label: String(option.label) };
    return { value: String(option), label: String(option) };
  });
}
