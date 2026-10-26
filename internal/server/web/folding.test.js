import test from 'node:test';
import assert from 'node:assert/strict';
import { foldRanges, visibleLines } from './folding.js';

test('JSON folds nested collections without interpreting brackets inside strings', () => {
  const text = '{\n  "items": [\n    {\n      "value": "} [ \\\""\n    }\n  ],\n  "empty": {}\n}';
  assert.deepEqual(foldRanges(text, 'json'), [
    { start: 0, end: 6 }, { start: 1, end: 4 }, { start: 2, end: 3 },
  ]);
});

test('YAML folds nested mappings, sequences and scalar blocks independently', () => {
  const text = 'items:\n  - name: first\n    nested:\n      value: 1\n  - name: second\nnotes: |\n  literal:\n    text\nother: true\n';
  assert.deepEqual(foldRanges(text, 'yaml'), [
    { start: 0, end: 4 }, { start: 1, end: 3 },
    { start: 2, end: 3 }, { start: 5, end: 7 },
  ]);
});

test('collapsing a parent retains nested fold state and exact source lines', () => {
  const text = '{\n  "a": {\n    "x": 1\n  },\n  "b": 2\n}';
  const ranges = foldRanges(text, 'json');
  const closed = new Set([0, 1]);
  assert.deepEqual(visibleLines(text, ranges, closed), [0, 5]);
  closed.delete(0);
  assert.deepEqual(visibleLines(text, ranges, closed), [0, 1, 3, 4, 5]);
  closed.clear();
  assert.deepEqual(visibleLines(text, ranges, closed), [0, 1, 2, 3, 4, 5]);
});

test('scalars and inline collections do not offer empty folds', () => {
  for (const format of ['json', 'yaml'])
    for (const text of ['42', '{}', '[]', '"<script>"'])
      assert.deepEqual(foldRanges(text, format), []);
});


test('YAML indentless sequences fold under their mapping key', () => {
  assert.deepEqual(foldRanges('items:\n- name: first\n  value: 1\n- name: second\nother: true', 'yaml'), [
    { start: 0, end: 3 }, { start: 1, end: 2 },
  ]);
});

// Minimal DOM surface for exercising rendering and button events without a browser.
class Element {
  constructor(tag, doc) {
    this.tagName = tag.toUpperCase(); this.ownerDocument = doc;
    this.children = []; this.dataset = {}; this.hidden = false;
    this.attributes = {}; this.listeners = {}; this._text = '';
  }
  set textContent(value) { this._text = value; this.children = []; }
  get textContent() { return this._text + this.children.map(n => n.textContent).join(''); }
  append(...nodes) {
    for (const n of nodes) {
      if (n.tagName === 'FRAGMENT') this.append(...n.children);
      else { n.parent = this; this.children.push(n); }
    }
  }
  replaceChildren(...nodes) { this.children = []; this._text = ''; this.append(...nodes); }
  before(n) { this.parent.children.splice(this.parent.children.indexOf(this), 0, n); n.parent = this.parent; }
  after(n) { this.parent.children.splice(this.parent.children.indexOf(this) + 1, 0, n); n.parent = this.parent; }
  setAttribute(key, value) { this.attributes[key] = value; }
  addEventListener(type, handler) { this.listeners[type] = handler; }
  click() { this.listeners.click?.(); }
  focus() { this.ownerDocument.activeElement = this; }
}
const doc = () => {
  const d = { createElement: tag => new Element(tag, d), createDocumentFragment: () => new Element('fragment', d) };
  return d;
};

test('fold controls hide nested lines, expose state, and retain complete copy text', async () => {
  const { createFoldView } = await import('./folding.js');
  const d = doc(), source = d.createElement('pre');
  source.textContent = '{\n  "nested": {\n    "value": "<script>"\n  }\n}';
  const original = source.textContent;
  const { host } = createFoldView(source, 'json');
  const [tools, code] = host.children;
  code.children[1].children[0].click();
  assert.equal(code.children.length, 4);
  assert.equal(code.children[1].children[0].attributes['aria-expanded'], 'false');
  assert.equal(d.activeElement, code.children[1].children[0]);
  tools.children[0].click();
  assert.equal(code.children.length, 2);
  assert.equal(source.textContent, original);
  tools.children[1].click();
  assert.equal(code.children.length, 5);
  assert.equal(code.children[2].children[1].textContent, '    "value": "<script>"');
});

test('subtree foldable mode preserves edits and refreshes without changing form values', async () => {
  const { setupInspectorFolding } = await import('./folding.js');
  const d = doc(), inspector = d.createElement('div'), source = d.createElement('textarea');
  source.dataset.foldFormat = 'yaml'; source.value = 'items:\n  value: 1';
  inspector.append(source);
  inspector.querySelectorAll = () => [source];
  setupInspectorFolding(inspector);
  const [toggle, , host] = inspector.children;
  assert.equal(host.hidden, true);
  source.value = 'items:\n  value: 2';
  toggle.click();
  assert.equal(source.hidden, true);
  assert.equal(host.hidden, false);
  assert.ok(host.textContent.includes('value: 2'));
  host.children[0].children[0].click();
  assert.equal(source.value, 'items:\n  value: 2');
  toggle.click();
  assert.equal(source.hidden, false);
  assert.equal(host.hidden, true);
  assert.equal(d.activeElement, source);
  setupInspectorFolding(inspector);
  assert.equal(inspector.children.length, 3, 'swapping must not duplicate controls');
});
