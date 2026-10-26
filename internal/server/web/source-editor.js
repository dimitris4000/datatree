export const normalizedSource = text => text.replace(/\r\n?/g, '\n');
export function lineStarts(text) {
  const starts = [0];
  for (let i = 0; i < text.length; i++) if (text[i] === '\n') starts.push(i + 1);
  return starts;
}
function lineIndex(starts, offset) {
  let low = 0, high = starts.length;
  while (low + 1 < high) {
    const mid = (low + high) >>> 1;
    if (starts[mid] <= offset) low = mid;
    else high = mid;
  }
  return low;
}
export function cursorPosition(text, offset) {
  const starts = lineStarts(text), line = lineIndex(starts, offset);
  return { line: line + 1, column: offset - starts[line] + 1 };
}
export function nodeAt(ranges, start, end) {
  let best = null;
  for (const range of ranges) {
    const contains = range.start <= start && end <= range.end &&
      (start < range.end || range.start === range.end || !range.parent);
    if (contains && (!best || range.end - range.start <= best.end - best.start)) best = range;
  }
  return best;
}
export function revealPath(ranges, id) {
  const byID = ranges instanceof Map ? ranges : new Map(ranges.map(r => [r.id, r]));
  const path = [];
  for (let node = byID.get(id); node?.parent; node = byID.get(node.parent))
    path.push({ id: node.parent, offset: Math.floor(node.index / 20) * 20 });
  return path.reverse();
}

// Keep the native textarea's editing, selection, IME and accessibility behavior.
// Only visible gutter lines are rendered, even for very large documents.
export class SourceEditor {
  constructor(source, gutter, position, onSelection) {
    Object.assign(this, { source, gutter, position, onSelection });
    this.mirror = source.ownerDocument.createElement('div');
    this.mirror.className = 'source-selection';
    this.mirror.setAttribute('aria-hidden', 'true');
    this.mirrorText = source.ownerDocument.createElement('pre');
    this.mirrorText.className = 'source-selection-text';
    this.mirror.append(this.mirrorText);
    source.before(this.mirror);
    this.text = null;
    this.signature = '';
    this.timer = null;
    source.addEventListener('input', () => this.refresh());
    source.addEventListener('scroll', () => this.renderGutter());
    for (const type of ['select', 'keyup', 'pointerup'])
      source.addEventListener(type, () => this.queueSelection());
    source.ownerDocument.addEventListener('selectionchange', () => {
      if (source.ownerDocument.activeElement === source) this.queueSelection();
    });
    this.observer = new ResizeObserver(() => this.renderGutter());
    this.observer.observe(source);
    this.refresh();
  }
  refresh() {
    if (this.text !== this.source.value) {
      this.text = this.source.value;
      this.mirrorSignature = null;
      this.starts = lineStarts(this.text);
    }
    this.renderGutter();
  }
  renderGutter() {
    const source = this.source;
    const lineHeight = parseFloat(getComputedStyle(source).lineHeight) || 18.7;
    const padding = parseFloat(getComputedStyle(source).paddingTop) || 18;
    const first = Math.max(0, Math.floor((source.scrollTop - padding) / lineHeight));
    const last = Math.min(this.starts.length, first + Math.ceil(source.clientHeight / lineHeight) + 2);
    const content = source.ownerDocument.createDocumentFragment();
    const signature = `${source.selectionStart}:${source.selectionEnd}`;
    if (signature !== this.mirrorSignature) {
      this.mirrorSignature = signature;
      const before = source.ownerDocument.createTextNode(this.text.slice(0, source.selectionStart));
      const mark = source.ownerDocument.createElement('mark');
      mark.textContent = this.text.slice(source.selectionStart, source.selectionEnd);
      const after = source.ownerDocument.createTextNode(this.text.slice(source.selectionEnd) + '\n');
      this.mirrorText.replaceChildren(before, mark, after);
    }
    this.mirror.style.width = `${source.clientWidth}px`;
    this.mirrorText.style.transform = `translate(${-source.scrollLeft}px, ${-source.scrollTop}px)`;
    const start = lineIndex(this.starts, source.selectionStart);
    const end = lineIndex(this.starts, Math.max(source.selectionStart, source.selectionEnd - 1));
    for (let i = first; i < last; i++) {
      const number = source.ownerDocument.createElement('div');
      number.textContent = String(i + 1);
      number.className = i >= start && i <= end ? 'source-line active' : 'source-line';
      number.style.height = `${lineHeight}px`;
      content.append(number);
    }
    this.gutter.replaceChildren(content);
    this.gutter.style.paddingTop = `${Math.max(0, padding + first * lineHeight - source.scrollTop)}px`;
    // A negative offset is needed when the top line is partly scrolled away.
    this.gutter.style.transform = `translateY(${Math.min(0, padding + first * lineHeight - source.scrollTop)}px)`;
    this.gutter.parentElement.style.setProperty('--gutter-width', `${(String(this.starts.length).length + 2) * 8}px`);
    const caret = source.selectionDirection === 'backward' ? source.selectionStart : source.selectionEnd;
    const line = lineIndex(this.starts, caret);
    this.position.textContent = `Ln ${line + 1}, Col ${caret - this.starts[line] + 1}`;
  }
  queueSelection() {
    this.renderGutter();
    const signature = `${this.source.selectionStart}:${this.source.selectionEnd}`;
    if (signature === this.signature) return;
    this.signature = signature;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.onSelection(this.source.selectionStart, this.source.selectionEnd), 100);
  }
  highlight(range) {
    if (!range) return;
    clearTimeout(this.timer);
    this.signature = `${range.start}:${range.end}`;
    this.source.setSelectionRange(range.start, range.end);
    const lineHeight = parseFloat(getComputedStyle(this.source).lineHeight) || 18.7;
    const line = lineIndex(this.starts, range.start);
    this.source.scrollTop = Math.max(0, line * lineHeight - this.source.clientHeight / 3);
    const column = range.start - this.starts[line];
    const charWidth = (parseFloat(getComputedStyle(this.source).fontSize) || 11) * 0.6;
    this.source.scrollLeft = Math.max(0, column * charWidth - this.source.clientWidth / 3);
    this.renderGutter();
  }
}
