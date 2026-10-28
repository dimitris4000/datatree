import test from 'node:test';
import assert from 'node:assert/strict';
import { DiffController, mountDiffPanel } from './diff.js';
import { diffBadge } from './tree.js';

const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; };
function result(revision = 1, kind = 'original', target = '2') {
 return { revision, baseline: { kind, filename: kind === 'file' ? 'base.yaml' : 'work.json', format: kind === 'file' ? 'yaml' : 'json', token: 'baseline-token' },
  changes: [{ status: 'removed', before: { path: "$['gone']", location: 'line 1, column 2', kind: 'string', preview: '<img onerror=alert(1)>' }, target },
   { status: 'changed', before: { path: "$['x']", kind: 'number', preview: '1' }, after: { path: "$['x']", kind: 'number', preview: '2' }, target: '3' }],
  counts: { added: 0, removed: 1, changed: 1 }, marks: { [target]: { contains: true }, '3': { changed: true } }, truncated: false };
}
function fixture(options = {}) {
 let revision = 1;
 const c = new DiffController({ revision: () => revision, load: async (kind, rev) => result(rev, kind), upload: async (_, rev) => result(rev, 'file'), navigate: async () => true, render() {}, invalidate() {}, ...options });
 return { c, changeRevision() { revision++; c.invalidateDocument(false); } };
}
test('open compares, removed entries navigate to the provided working parent, and close clears marks', async () => {
 let navigated;
 const { c } = fixture({ navigate: async id => { navigated = id; return true; } });
 await c.setVisible(true); assert.equal(c.result.counts.removed, 1);
 await c.choose(0); assert.equal(navigated, '2'); assert.equal(c.index, 0);
 await c.move(1); assert.equal(c.index, 1); await c.move(1); assert.equal(c.index, 0);
 await c.setVisible(false); assert.equal(c.result, null); assert.equal(c.visible, false);
});
test('failed comparison file replacement retains the baseline and results', async () => {
 const { c } = fixture(); await c.setVisible(true);
 await c.replace(async () => ({ text: '{}', format: 'json', filename: 'base.json' }));
 const prior = c.result; assert.equal(c.baseline, 'file');
 c.upload = async () => { throw new Error('Invalid YAML'); };
 await c.replace(async () => ({ text: 'bad', format: 'yaml', filename: 'bad.yaml' }));
 assert.equal(c.result, prior); assert.equal(c.baseline, 'file'); assert.equal(c.file.filename, 'base.yaml'); assert.match(c.error, /Invalid YAML/);
});
test('uploads are serialized, keep their captured revision, and closing never resurrects highlights', async () => {
 const pending = deferred(); let calls = 0;
 const { c } = fixture({ upload: async (_, rev) => { calls++; assert.equal(rev, 1); return pending.promise; } });
 await c.setVisible(true);
 const upload = c.replace(async () => ({ text: '{}', format: 'json', filename: 'base.json' }));
 await Promise.resolve();
 await c.replace(async () => { throw new Error('second upload should be ignored'); }); assert.equal(calls, 1);
 await c.setVisible(false); pending.resolve(result(1, 'file')); await upload;
 assert.equal(c.result, null); assert.equal(c.visible, false); assert.equal(c.uploading, false);
});
test('superseded refresh results and errors cannot replace newer results', async () => {
 const first = deferred(), second = deferred(); let calls = 0;
 const { c } = fixture({ load: () => (++calls === 1 ? first.promise : second.promise) });
 const opening = c.setVisible(true), next = c.refresh();
 second.resolve(result(1, 'original', 'new')); await next;
 first.resolve(result(1, 'original', 'old')); await opening;
 assert.ok(c.result.marks.new); assert.equal(c.result.marks.old, undefined);
 const pending = deferred(); c.load = () => pending.promise;
 const refresh = c.refresh(); await c.setVisible(false); pending.reject(new Error('old error')); await refresh;
 assert.equal(c.error, ''); assert.equal(c.result, null);
});
test('document revisions invalidate IDs; new imports reset the file baseline', async () => {
 const pending = deferred(); const { c, changeRevision } = fixture({ upload: () => pending.promise });
 await c.setVisible(true); const replacing = c.replace(async () => ({ text: '{}', format: 'json' })); await Promise.resolve();
 changeRevision(); assert.equal(c.result, null);
 pending.resolve(result(1, 'file')); await replacing; assert.equal(c.result, null); assert.equal(c.baseline, 'original');
 c.upload = async (_, rev) => result(rev, 'file'); await c.replace(async () => ({ text: '{}', format: 'json' }));
 assert.equal(c.baseline, 'file'); c.invalidateDocument(true); assert.equal(c.baseline, 'original'); assert.equal(c.file, null);
});
test('stale response revisions and cancelled draft navigation never become active changes', async () => {
 const { c } = fixture({ load: async () => result(9), navigate: async () => false });
 await c.setVisible(true); assert.equal(c.result, null); assert.match(c.error, /changed|stale/i);
 c.load = async () => result(); await c.refresh(); await c.choose(0); assert.equal(c.index, -1);
});
test('tree badges distinguish direct changes from descendant changes', () => {
 assert.match(diffBadge({ added: true }).label, /Added/);
 assert.match(diffBadge({ changed: true }).label, /Changed/);
 assert.match(diffBadge({ contains: true }).label, /Contains changes/);
 assert.equal(diffBadge(undefined), null);
});

function panelDocument() {
 class Element {
  constructor(tag) { this.tagName = tag; this.children = []; this.listeners = new Map(); this.attributes = new Map(); this.value = ''; this.hidden = false; this.disabled = false; this.classList = { toggle() {} }; }
  addEventListener(e, f) { this.listeners.set(e, f); } append(...nodes) { this.children.push(...nodes); } replaceChildren(...nodes) { this.children = nodes; }
  setAttribute(k, v) { this.attributes.set(k, v); } set innerHTML(_) { throw new Error('unsafe preview rendering'); }
 }
 const elements = new Map(['toggle','panel','baseline','file','filename','refresh','status','counts','results','previous','next'].map(id => [`diff-${id}`, new Element()]));
 return { getElementById: id => elements.get(id), createElement: tag => new Element(tag), elements };
}
test('comparison selection uses the shared picker path and cancellation retains its baseline',async()=>{
 const doc=panelDocument(),calls=[];let choice={path:'/data/baseline.yaml'};
 const panel=mountDiffPanel(doc,{revision:()=>1,pickFile:async()=>choice,load:async()=>result(),upload:async(payload,revision)=>{calls.push({payload,revision});return result(revision,'file');},navigate:async()=>true,highlight(){}});
 await panel.controller.setVisible(true);await doc.getElementById('diff-file').listeners.get('click')();
 assert.deepEqual(calls,[{payload:{path:'/data/baseline.yaml'},revision:1}]);assert.equal(panel.controller.baseline,'file');
 const prior=panel.controller.result;choice=null;await doc.getElementById('diff-file').listeners.get('click')();assert.equal(calls.length,1);assert.equal(panel.controller.result,prior);assert.equal(panel.controller.error,'');
});
test('panel renders explicit statuses and safe before/after text and hides marks on close', async () => {
 const doc = panelDocument(), $ = id => doc.getElementById(`diff-${id}`); let marks;
 const panel = mountDiffPanel(doc, { revision: () => 1, load: async () => result(), upload: async () => result(1, 'file'), navigate: async () => true, invalidate() {}, highlight: value => { marks = value; } });
 await $('toggle').listeners.get('click')();
 assert.equal($('panel').hidden, false); assert.match($('counts').textContent, /1 removed/); assert.ok(marks['3'].changed);
 const row = $('results').children[0]; assert.match(row.children[0].children[0].textContent, /Removed/); assert.match(row.children[1].children[0].children[1].textContent, /<img onerror=alert\(1\)>/);
 await $('toggle').listeners.get('click')(); assert.equal($('panel').hidden, true); assert.deepEqual(marks, {}); assert.equal(panel.controller.result, null);
});

test('canvas renders added/changed/contains labels and removing marks clears them', async () => {
 const { TreeCanvas, layout } = await import('./tree.js');
 const texts = [], fills = [];
 const ctx = new Proxy({ fillText: text => texts.push(text), fillRect() { fills.push(this.fillStyle); }, measureText: text => ({ width: String(text).length * 6 }) }, { get(target, key) { return key in target ? target[key] : () => {}; } });
 const root = { id: 'root', kind: 'object', label: '$', total: 2, children: [{ id: 'a', kind: 'string', label: 'a', value: 'new' }, { id: 'b', kind: 'number', label: 'b', value: '2' }] };
 const tree = Object.create(TreeCanvas.prototype);
 Object.assign(tree, { canvas: { width: 600, getContext: () => ctx }, width: 600, height: 400, view: { x: 0, y: 0, scale: 1 }, callbacks: {}, graph: layout(root, new Map(), new Set(['root'])) });
 tree.setDiffMarks({ root: { contains: true }, a: { added: true }, b: { changed: true } });
 assert.ok(texts.some(t => t.includes('Contains changes')));
 assert.ok(texts.some(t => t.includes('Added'))); assert.ok(texts.some(t => t.includes('Changed')));
 assert.ok(fills.includes('#e1f2e4')); assert.ok(fills.includes('#fff0d2'));
 texts.length = 0; tree.setDiffMarks({});
 assert.equal(texts.some(t => /Added|Changed|Contains changes/.test(t)), false);
});


test('before/after values are selectable content outside navigation and revert buttons', async () => {
 const doc = panelDocument(), $ = id => doc.getElementById(`diff-${id}`);
 let navigations = 0, reversions = 0;
 const panel = mountDiffPanel(doc, { revision: () => 1, load: async () => result(), upload: async () => result(), navigate: async () => { navigations++; return true; }, revert: async () => { reversions++; return false; }, highlight() {} });
 await $('toggle').listeners.get('click')();
 const row = $('results').children[1];
 assert.equal(row.tagName, 'li'); assert.equal(row.listeners.has('click'), false);
 const header = row.children[0], values = row.children[1];
 assert.equal(header.children[0].tagName, 'button'); assert.equal(header.children[1].tagName, 'button');
 assert.equal(header.children[1].textContent, 'Revert');
 const before = values.children[0].children[1], after = values.children[1].children[1];
 assert.equal(before.tagName, 'pre'); assert.equal(before.tabIndex, 0); assert.equal(before.textContent, '1');
 assert.equal(after.tagName, 'pre'); assert.equal(after.tabIndex, 0); assert.equal(after.textContent, '2');
 assert.equal(before.listeners.has('click'), false); assert.equal(after.listeners.has('click'), false);
 await header.children[1].listeners.get('click')(); assert.equal(reversions, 1); assert.equal(navigations, 0);
 await header.children[0].listeners.get('click')(); assert.equal(navigations, 1);
 assert.equal(panel.controller.index, 1);
});
test('revert submits the displayed revision/baseline token, serializes and respects rejected draft guards', async () => {
 const pending = deferred(); let calls = 0;
 const { c } = fixture({ revert: async (payload, revision) => {
  calls++; assert.equal(revision, 1); assert.deepEqual(payload, { baseline: 'original', baselineToken: 'baseline-token', changeIndex: 1 });
  return pending.promise;
 } });
 await c.setVisible(true); const prior = c.result;
 const reverting = c.revertChange(1); await c.revertChange(0); assert.equal(calls, 1); assert.equal(c.reverting, true);
 pending.resolve(false); assert.equal(await reverting, false); assert.equal(c.result, prior); assert.equal(c.reverting, false);
 c.revert = async () => { throw new Error('stale comparison baseline'); };
 assert.equal(await c.revertChange(1), false); assert.match(c.error, /stale/); assert.equal(c.result, prior);
});

test('cached comparisons reopen without rereading and explicit refresh bypasses the cache', async () => {
 let calls=0;
 const {c,changeRevision}=fixture({load:async(kind,revision)=>{calls++;return result(revision,kind);}});
 await c.setVisible(true);await c.setVisible(false);await c.setVisible(true);
 assert.equal(calls,1);
 await c.refresh();assert.equal(calls,2,'explicit Refresh asks for new comparison');
 changeRevision();await c.setVisible(true);assert.equal(calls,3);
});
