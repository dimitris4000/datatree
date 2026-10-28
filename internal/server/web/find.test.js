import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { JSDOM } from 'jsdom';

test('Search starts hidden and its heading toggle preserves searches across closing and reopening', async () => {
  const { mountFindPanel } = await import('./find.js');
  const dom = new JSDOM(await readFile(new URL('./index.html', import.meta.url), 'utf8'));
  try {
    const doc = dom.window.document;
    const section = doc.querySelector('.find-panel');
    const toggle = doc.querySelector('#tree-pane .pane-heading #find-toggle');
    assert.equal(section.hidden, true);
    assert.ok(toggle, 'Search toggle belongs beside Changes and Validation');
    assert.equal(toggle.textContent, 'Search');
    assert.equal(toggle.getAttribute('aria-controls'), section.id);
    assert.equal(toggle.getAttribute('aria-expanded'), 'false');
    let loads = 0;
    const { controller } = mountFindPanel(doc, {
      revision: () => 1, navigate: async () => true,
      load: async () => { loads++; return { revision: 1, matches: [match('found')], truncated: false }; },
    });
    toggle.click();
    assert.equal(section.hidden, false);
    assert.equal(toggle.getAttribute('aria-expanded'), 'true');
    doc.getElementById('find-text').value = 'found';
    await controller.submit({ mode: 'search', text: 'found', values: true });
    toggle.click();
    assert.equal(section.hidden, true);
    assert.equal(toggle.getAttribute('aria-expanded'), 'false');
    toggle.click();
    assert.equal(section.hidden, false);
    assert.equal(toggle.getAttribute('aria-expanded'), 'true');
    assert.equal(doc.getElementById('find-text').value, 'found');
    assert.equal(doc.querySelectorAll('#find-results li').length, 1);
    assert.equal(loads, 1, 'toggling must not execute a search');
    assert.equal(doc.getElementById('diff-panel').hidden, true);
    assert.equal(doc.getElementById('validation-panel').hidden, true);
  } finally {
    dom.window.close();
  }
});

test('result pages stay bounded and can return to an earlier page', async () => {
 const {FindController}=await import('./find.js');
 const calls=[];
 const controller=new FindController({revision:()=>1,navigate:async()=>true,render:()=>{},load:async options=>{
  calls.push(options.cursor||'');
  return {revision:1,matches:options.cursor?[{id:'3'},{id:'4'}]:[{id:'1'},{id:'2'}],truncated:!options.cursor,nextCursor:options.cursor?'':'2'};
 }});
 await controller.submit({mode:'search',text:'x',values:true});
 assert.equal(await controller.page(1),true);
 assert.deepEqual(controller.matches.map(m=>m.id),['3','4']);
 assert.equal(controller.pageIndex,1);
 assert.equal(await controller.page(1),false);
 assert.equal(await controller.page(-1),true);
 assert.deepEqual(controller.matches.map(m=>m.id),['1','2']);
 assert.deepEqual(calls,['','2','']);
 controller.clear();assert.equal(controller.nextCursor,'');assert.equal(controller.pageIndex,0);
});

test('failed result page keeps the current matches and page position', async () => {
 const {FindController}=await import('./find.js');
 const controller=new FindController({revision:()=>1,navigate:async()=>true,render:()=>{},load:async options=>{
  if(options.cursor)throw new Error('page unavailable');
  return {revision:1,matches:[{id:'1'}],truncated:true,nextCursor:'1'};
 }});
 await controller.submit({mode:'search',text:'x',values:true});
 assert.equal(await controller.page(1),false);
 assert.equal(controller.pageIndex,0);assert.equal(controller.matches[0].id,'1');
 assert.equal(controller.error,'page unavailable');
});
import { FindController, revealResult } from './find.js';
import { layout } from './tree.js';

const match = id => ({ id, path: `$['${id}']`, location: 'line 1, column 1', kind: 'string', preview: id });
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
function fixture(load, navigate = async () => true) {
  let revision = 1;
  const controller = new FindController({ load, navigate, revision: () => revision, render() {} });
  return { controller, changeRevision() { revision++; } };
}
test('search results navigate, wrap, and respect cancelled navigation', async () => {
  let allow = true;
  const { controller: c } = fixture(async () => ({ revision: 1, matches: [match('a'), match('b')], truncated: true }), async () => allow);
  await c.submit({ mode: 'search', text: 'a', keys: true, values: true });
  assert.equal(c.matches.length, 2); assert.equal(c.truncated, true); assert.equal(c.index, -1);
  await c.move(1); assert.equal(c.index, 0);
  await c.move(-1); assert.equal(c.index, 1);
  allow = false; await c.choose(0); assert.equal(c.index, 1);
  c.clear(); assert.deepEqual(c.matches, []); assert.equal(c.index, -1);
});
test('single exact query result navigates immediately', async () => {
  const { controller: c } = fixture(async () => ({ revision: 1, matches: [match('a')], truncated: false }));
  await c.submit({ mode: 'query', text: '$.a' });
  assert.equal(c.index, 0);
});
test('empty searches clear without loading and no selected search scope explains the error', async () => {
  const { controller: c } = fixture(async () => { throw new Error('must not load'); });
  await c.submit({ mode: 'search', text: '', keys: true });
  assert.equal(c.error, ''); assert.deepEqual(c.matches, []);
  await c.submit({ mode: 'search', text: 'x', keys: false, values: false });
  assert.match(c.error, /keys or values/i);
});
test('late responses cannot replace newer results or survive clear/revision changes', async () => {
  const first = deferred(), second = deferred(); let calls = 0;
  const { controller: c, changeRevision } = fixture(() => (++calls === 1 ? first.promise : second.promise));
  const old = c.submit({ mode: 'query', text: '$.old' });
  const fresh = c.submit({ mode: 'query', text: '$.*' });
  second.resolve({ revision: 1, matches: [match('new'), match('next')], truncated: false }); await fresh;
  first.resolve({ revision: 1, matches: [match('old')], truncated: false }); await old;
  assert.equal(c.matches[0].id, 'new');
  c.clear(); assert.deepEqual(c.matches, []);
  const pending = deferred(); c.load = () => pending.promise;
  const stale = c.submit({ mode: 'query', text: '$' }); changeRevision();
  pending.resolve({ revision: 1, matches: [match('stale')], truncated: false }); await stale;
  assert.deepEqual(c.matches, []); assert.equal(c.loading, false);
});
test('errors are visible and stale errors are ignored', async () => {
  const delayed = deferred();
  const { controller: c } = fixture(() => delayed.promise);
  const old = c.submit({ mode: 'query', text: '$' }); c.clear();
  delayed.reject(new Error('old error')); await old; assert.equal(c.error, '');
  c.load = async () => { throw new Error('Invalid query'); };
  await c.submit({ mode: 'query', text: '$[bad]' }); assert.equal(c.error, 'Invalid query');
});
test('result revision mismatch cannot expose or navigate another document', async () => {
  const { controller: c } = fixture(async () => ({ revision: 99, matches: [match('wrong')], truncated: false }));
  await c.submit({ mode: 'query', text: '$' }); assert.deepEqual(c.matches, []);
  assert.match(c.error, /changed|stale/i);
});
test('clearing during navigation prevents active result state from returning', async () => {
  const pending = deferred();
  const { controller: c } = fixture(async () => ({ revision: 1, matches: [match('a'), match('b')], truncated: false }), () => pending.promise);
  await c.submit({ mode: 'query', text: '$.*' }); const navigation = c.choose(0);
  c.clear(); pending.resolve(true); await navigation;
  assert.equal(c.index, -1); assert.deepEqual(c.matches, []); assert.equal(c.navigating, false);
});

test('reveal loads ancestors and off-page results and makes the target visible', async () => {
  const children = Array.from({ length: 46 }, (_, i) => ({ id: `n${i}`, label: `[${i}]`, kind: 'object', total: 1 }));
  const root = { id: 'root', label: '$', kind: 'array', total: 46, children: children.slice(0, 20), offset: 0 };
  const records = new Map([['root', root]]), expanded = new Set();
  const ranges = new Map([
    ['root', { id: 'root' }], ['n45', { id: 'n45', parent: 'root', index: 45 }],
    ['value', { id: 'value', parent: 'n45', index: 0 }],
  ]);
  const load = async (id, offset) => {
    const n = id === 'root' ? { ...root, offset, children: children.slice(offset, offset + 20) } :
      { ...children[45], offset, children: [{ id: 'value', kind: 'string', label: 'name', value: 'needle' }] };
    records.set(id, n); return n;
  };
  const graph = () => layout(root, records, expanded);
  assert.equal(await revealResult('value', { ranges, records, expanded, load, current: () => true, relayout: graph }), true);
  assert.equal(records.get('root').offset, 40);
  assert.ok(expanded.has('root')); assert.ok(expanded.has('n45'));
  assert.ok(graph().cards.some(c => c.rows.some(r => r.id === 'value')));
});
test('reveal cancels when an awaited page is stale', async () => {
  let current = true; const expanded = new Set();
  const ok = await revealResult('child', { ranges: new Map([['child', { id: 'child', parent: 'root', index: 30 }], ['root', { id: 'root' }]]), records: new Map(), expanded,
    load: async () => { current = false; }, current: () => current, relayout: () => { throw new Error('stale layout'); } });
  assert.equal(ok, false); assert.equal(expanded.size, 0);
});

test('deep results remain visible when sibling cards exceed the rendering budget', async () => {
  const records = new Map(), ranges = new Map(), expanded = new Set();
  let child = { id: 'target', label: 'needle', kind: 'string', value: 'needle' };
  for (let depth = 79; depth >= 0; depth--) {
    const id = `level${depth}`;
    const siblings = Array.from({ length: 19 }, (_, i) => ({ id: `${id}-s${i}`, label: `[${i}]`, kind: 'string', value: 'x' }));
    const parent = { id, kind: 'array', label: id, total: 20, offset: 0, children: [...siblings, child] };
    ranges.set(child.id, { id: child.id, parent: id, index: 19 });
    records.set(id, parent); child = parent;
  }
  ranges.set(child.id, { id: child.id });
  const root = child; let graph;
  const ok = await revealResult('target', { ranges, records, expanded, current: () => true,
    load: async () => { throw new Error('already loaded'); },
    relayout: priority => { graph = layout(root, records, expanded, priority); return graph; } });
  assert.equal(ok, true); assert.ok(graph.cards.some(c => c.id === 'target'));
  assert.ok(graph.cards.length <= 1000);
});

// A small DOM boundary fixture, matching the native methods this panel uses.
function panelDocument() {
  class Element {
    constructor(tag = 'div') { this.tagName = tag; this.children = []; this.listeners = new Map(); this.attributes = new Map(); this.value = ''; this.checked = false; this.hidden = false; this.disabled = false; this.classList = { toggle() {} }; }
    addEventListener(event, handler) { this.listeners.set(event, handler); }
    append(...nodes) { this.children.push(...nodes); }
    replaceChildren(...nodes) { this.children = nodes; }
    setAttribute(k, v) { this.attributes.set(k, v); }
    set innerHTML(_) { throw new Error('result content must be text'); }
  }
  const elements = new Map(['toggle', 'panel', 'form', 'mode', 'text', 'keys', 'values', 'case', 'options', 'help', 'status', 'results', 'submit', 'clear', 'previous', 'next','previous-page','next-page'].map(id => [`find-${id}`, new Element()]));
  elements.get('find-mode').value = 'search'; elements.get('find-keys').checked = elements.get('find-values').checked = true;
  return { getElementById: id => elements.get(id), createElement: tag => new Element(tag), elements };
}
test('mounted panel submits scope, displays safe results and updates query help', async () => {
  const { mountFindPanel } = await import('./find.js');
  const doc = panelDocument(); const $ = id => doc.getElementById(`find-${id}`);
  const panel = mountFindPanel(doc, {
    revision: () => 1, navigate: async () => true,
    load: async options => {
      assert.equal(options.mode, 'search'); assert.equal(options.text, '<script>');
      assert.equal(options.keys, false); assert.equal(options.values, true); assert.equal(options.caseSensitive, true);
      return { revision: 1, matches: [{ ...match('safe'), preview: '<img onerror=alert(1)>' }], truncated: false };
    },
  });
  $('text').value = '<script>'; $('keys').checked = false; $('case').checked = true;
  await $('form').listeners.get('submit')({ preventDefault() {} });
  assert.equal($('results').hidden, false);
  assert.equal($('results').children[0].children[0].children[1].textContent, 'string · <img onerror=alert(1)>');
  assert.match($('status').textContent, /1 match/);
  await $('next').listeners.get('click')(); assert.equal(panel.controller.index, 0);
  $('mode').value = 'query'; $('mode').listeners.get('change')();
  assert.equal($('options').hidden, true); assert.match($('help').textContent, /\$\.services/);
  assert.equal($('results').hidden, true);
});

 test('clearing Find aborts the actual request signal', async () => {
 let signal;
 const pending=deferred();
 const {controller}=fixture((options,revision,requestSignal)=>{signal=requestSignal;return pending.promise;});
 const run=controller.submit({mode:'search',text:'x',keys:true});
 assert.ok(signal instanceof AbortSignal);
 controller.clear();
 assert.equal(signal.aborted,true);
 pending.resolve({revision:1,matches:[],truncated:false});await run;
 });
