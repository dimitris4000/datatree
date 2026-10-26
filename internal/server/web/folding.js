// Line ranges hide content only: source text remains the copy/edit authority.
export function foldRanges(text, format) {
  const lines = text.split('\n'), ranges = [];
  if (format === 'json') {
    const stack = [];
    let quoted = false, escaped = false;
    lines.forEach((line, index) => {
      for (const char of line) {
        if (quoted) {
          if (escaped) escaped = false;
          else if (char === '\\') escaped = true;
          else if (char === '"') quoted = false;
        } else if (char === '"') quoted = true;
        else if (char === '{' || char === '[') stack.push(index);
        else if (char === '}' || char === ']') {
          const start = stack.pop();
          if (start !== undefined && index > start + 1)
            ranges.push({ start, end: index - 1 });
        }
      }
    });
  } else {
    // YAML block collections use indentation. Block scalar contents are opaque.
    const stack = [];
    let scalarIndent = null, previous = null;
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      if (!line.trim()) continue;
      const rawIndent = line.match(/^ */)[0].length;
      const indent = rawIndent + (/^\s*-(?:\s|$)/.test(line) ? 1 : 0);
      if (scalarIndent !== null && rawIndent > scalarIndent) continue;
      scalarIndent = null;
      while (stack.length && indent <= stack.at(-1).indent) {
        const item = stack.pop();
        ranges.push({ start: item.index, end: i - 1 });
      }
      if (previous && indent > previous.indent) stack.push(previous);
      previous = { index: i, indent };
      if (/(?:^|:\s+|-\s+)[|>][1-9+-]*\s*(?:#.*)?$/.test(line)) {
        scalarIndent = rawIndent;
        stack.push(previous);
        previous = null;
      }
    }
    const end = lines.length - (lines.at(-1) === '' ? 2 : 1);
    for (const item of stack) if (end > item.index)
      ranges.push({ start: item.index, end });
  }
  // One fold per line, outermost first if several collections share a line.
  return ranges.sort((a, b) => a.start - b.start || b.end - a.end)
    .filter((range, index, all) => index === 0 || range.start !== all[index - 1].start);
}

export function visibleLines(text, ranges, collapsed) {
  const ends = new Map(ranges.map(r => [r.start, r.end])), visible = [];
  for (let i = 0, count = text.split('\n').length; i < count; i++) {
    visible.push(i);
    if (collapsed.has(i) && ends.has(i)) i = ends.get(i);
  }
  return visible;
}

export function createFoldView(source, format) {
  const doc = source.ownerDocument;
  const host = doc.createElement('div');
  host.className = 'fold-view';
  const tools = doc.createElement('div');
  tools.className = 'button-row fold-tools';
  const code = doc.createElement('div');
  code.className = 'fold-code';
  let text, lines, ranges, collapsed = new Set();
  function render(focusLine) {
    const byStart = new Map(ranges.map(r => [r.start, r]));
    const fragment = doc.createDocumentFragment();
    let focus;
    for (const index of visibleLines(text, ranges, collapsed)) {
      const row = doc.createElement('div');
      row.className = 'fold-line';
      const range = byStart.get(index);
      const gutter = doc.createElement(range ? 'button' : 'span');
      gutter.className = 'fold-toggle';
      if (range) {
        gutter.type = 'button';
        gutter.textContent = collapsed.has(index) ? '▸' : '▾';
        gutter.setAttribute('aria-expanded', String(!collapsed.has(index)));
        gutter.setAttribute('aria-label', `${collapsed.has(index) ? 'Expand' : 'Collapse'} subtree at line ${index + 1}`);
        gutter.addEventListener('click', () => {
          if (collapsed.has(index)) collapsed.delete(index);
          else collapsed.add(index);
          render(index);
        });
        if (index === focusLine) focus = gutter;
      }
      const content = doc.createElement('span');
      content.textContent = lines[index];
      row.append(gutter, content);
      if (range && collapsed.has(index)) {
        const hint = doc.createElement('span');
        hint.className = 'fold-hint';
        hint.textContent = ` … ${range.end - index} lines`;
        row.append(hint);
      }
      fragment.append(row);
    }
    code.replaceChildren(fragment);
    focus?.focus();
  }
  for (const [label, close] of [['Collapse all', true], ['Expand all', false]]) {
    const button = doc.createElement('button');
    button.type = 'button';
    button.textContent = label;
    button.addEventListener('click', () => {
      collapsed = new Set(close ? ranges.map(r => r.start) : []);
      render();
    });
    tools.append(button);
  }
  function refresh() {
    text = source.value ?? source.textContent;
    lines = text.split('\n');
    ranges = foldRanges(text, format);
    collapsed.clear();
    tools.hidden = ranges.length === 0;
    render();
  }
  host.append(tools, code);
  refresh();
  return { host, refresh };
}

export function setupInspectorFolding(inspector) {
  for (const source of inspector.querySelectorAll('[data-fold-format]')) {
    if (source.dataset.foldReady) continue;
    source.dataset.foldReady = 'true';
    const view = createFoldView(source, source.dataset.foldFormat);
    source.after(view.host);
    if (source.tagName === 'TEXTAREA') {
      const toggle = source.ownerDocument.createElement('button');
      toggle.type = 'button';
      toggle.textContent = 'Foldable view';
      toggle.setAttribute('aria-pressed', 'false');
      view.host.hidden = true;
      source.before(toggle);
      toggle.addEventListener('click', () => {
        const folding = view.host.hidden;
        if (folding) view.refresh();
        source.hidden = folding;
        view.host.hidden = !folding;
        toggle.textContent = folding ? 'Edit text' : 'Foldable view';
        toggle.setAttribute('aria-pressed', String(folding));
        if (!folding) source.focus();
      });
    } else source.hidden = true;
  }
}
