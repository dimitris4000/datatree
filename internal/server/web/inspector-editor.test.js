import test from 'node:test';
import assert from 'node:assert/strict';
import {editorDOM} from './editor-dom-test.js';

test('representations load only when opened and truncated preview cannot be submitted as subtree', async () => {
 const {mountInspectorEditors}=await import('./inspector-editor.js');
 const {dom,host}=editorDOM();
 host.innerHTML='<details open><summary>Subtree</summary><form><textarea id="subtree" name="text" data-representation-node="n" data-format="native" readonly></textarea><button type="submit">Apply</button></form></details><details><summary>JSON</summary><pre data-representation-node="n" data-format="json"></pre></details>';
 const calls=[];
 const panel=mountInspectorEditors(host,{revision:1,format:'json',load:async (node,format,full)=>{calls.push({node,format,full});return {revision:1,text:full?'complete':'preview',truncated:!full,totalBytes:90000};},changed:()=>{}});
 await panel.ready;
 assert.deepEqual(calls,[{node:'n',format:'native',full:false}]);
 const form=host.querySelector('form');
 assert.equal(form.dispatchEvent(new dom.window.Event('submit',{cancelable:true,bubbles:true})),false);
 assert.ok(form.querySelector('button[type="submit"]').disabled);
 assert.equal(host.querySelectorAll('.cm-editor').length,1);
 const full=host.querySelector('[data-load-full]');
 await full.onclick();
 assert.equal(form.querySelector('button[type="submit"]').disabled,false);
 assert.equal(form.dispatchEvent(new dom.window.Event('submit',{cancelable:true,bubbles:true})),true);
 assert.equal(host.querySelector('textarea').value,'complete');
 assert.equal(calls.length,2);
 panel.destroy();dom.window.close();
});

test('complete preview still requires explicit edit and full response; stale and aborted loads cannot mount', async () => {
 const {mountInspectorEditors}=await import('./inspector-editor.js');
 const {dom,host}=editorDOM();
 host.innerHTML='<details open><form><textarea data-representation-node="n" data-format="native" readonly></textarea><button type="submit">Apply</button></form></details>';
 let signal,resolve;
 const panel=mountInspectorEditors(host,{revision:2,format:'yaml',load:(_n,_f,_full,s)=>{signal=s;return new Promise(r=>resolve=r);}});
 panel.destroy();assert.equal(signal.aborted,true);
 resolve({revision:2,text:'late',truncated:false,totalBytes:4});
 await panel.ready;
 assert.equal(host.querySelector('.cm-editor'),null);
 dom.window.close();
});

test('truncated JSON and YAML representations expose complete read-only text for copying', async () => {
 const {mountInspectorEditors}=await import('./inspector-editor.js');
 const {EditorView}=await import('@codemirror/view');
 for(const format of ['json','yaml']){
  const {dom,host}=editorDOM();
  host.innerHTML=`<details open><summary>${format}</summary><pre data-representation-node="n" data-format="${format}"></pre></details>`;
  const complete='λ 😀 exact source\n'.repeat(10000),calls=[];
  const panel=mountInspectorEditors(host,{revision:1,format:'json',load:async(node,requested,full)=>{
   calls.push({node,format:requested,full});return {revision:1,text:full?complete:'preview',truncated:!full,totalBytes:Buffer.byteLength(complete)};
  }});
  await panel.ready;
  const source=host.querySelector('pre');
  assert.throws(()=>panel.copyText(source),/truncated/i);
  const full=host.querySelector('[data-load-full]');
  assert.ok(full,'read-only representations need an explicit complete-text action');
  await full.onclick();
  assert.equal(panel.copyText(source),complete);
  assert.equal(EditorView.findFromDOM(host.querySelector('.cm-editor')).state.readOnly,true);
  assert.deepEqual(calls,[{node:'n',format,full:false},{node:'n',format,full:true}]);
  panel.destroy();dom.window.close();
 }
});

test('failed full-text fetch cannot make an old truncated preview copyable', async () => {
 const {mountInspectorEditors}=await import('./inspector-editor.js');
 const {dom,host}=editorDOM();
 host.innerHTML='<details open><textarea data-representation-node="n" data-format="native" readonly></textarea></details>';
 const panel=mountInspectorEditors(host,{revision:1,format:'json',load:async(_n,_f,full)=>{
  if(full)throw new Error('Full text request failed');
  return {revision:1,text:'partial',truncated:true,totalBytes:90000};
 }});
 await panel.ready;await host.querySelector('[data-load-full]').onclick();
 assert.match(host.querySelector('.representation-status').textContent,/failed/);
 assert.throws(()=>panel.copyText(host.querySelector('textarea')),/truncated/i);
 panel.destroy();dom.window.close();
});
