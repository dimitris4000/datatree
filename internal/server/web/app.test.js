import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {EditorView} from '@codemirror/view';
import {SourceEditor} from './source-editor.js';
import {editorDOM} from './editor-dom-test.js';

test('metadata startup loads source once and only the root tree page, without eager inspector or source map', async () => {
 const {dom}=editorDOM();
 dom.window.document.documentElement.innerHTML=await readFile(new URL('./index.html',import.meta.url),'utf8');
 globalThis.devicePixelRatio=1;
 dom.window.HTMLCanvasElement.prototype.getContext=()=>new Proxy({measureText:()=>({width:50})},{get:(target,name)=>target[name]||(()=>{}),set:(t,n,v)=>(t[n]=v,true)});
 const calls=[];let revision=1,patchPayload;
 const json=body=>new Response(JSON.stringify(body),{headers:{'content-type':'application/json'}});
 const settle=()=>new Promise(resolve=>setTimeout(resolve,160));
 globalThis.fetch=async (path,options={})=>{
  calls.push(String(path));
  let body;
  if(path==='/api/state')body={revision,root:`r${revision}`,nodes:1000,format:'json'};
  else if(String(path).startsWith('/api/patch')){patchPayload=JSON.parse(options.body);revision++;return json({revision,root:`r${revision}`,nodes:1000,format:'json'});}
  else if(String(path).startsWith('/api/locate?'))return json({revision,range:{id:`r${revision}`,start:0,end:13},ancestors:[]});
  else if(String(path).startsWith('/api/node-location?'))return json({revision,range:{id:`c${revision}`,start:9,end:11,parent:`r${revision}`,index:0},ancestors:[{id:`r${revision}`,offset:0}]});
  else if(String(path).startsWith('/inspector?'))return new Response('<p>Selected child</p>');
  else if(String(path).startsWith('/api/source?'))return new Response('{"child":{}}',{headers:{'X-Document-Revision':'1'}});
  else if(String(path).startsWith('/api/tree?'))body={id:`r${revision}`,kind:'object',label:'$',total:1,children:[{id:`c${revision}`,label:'child',kind:'object',total:1}]};
  else throw new Error(`Unexpected startup request ${path}`);
  return new Response(JSON.stringify(body),{headers:{'content-type':'application/json'}});
 };
 await import(`./app.js?startup=${Date.now()}`);
 assert.equal(calls.filter(p=>p.startsWith('/api/source?')).length,1);
 assert.equal(calls.filter(p=>p.startsWith('/api/tree?')).length,1);
 assert.equal(calls.some(p=>/source-map|inspector|original|representation/.test(p)),false);
 assert.equal(dom.window.document.querySelectorAll('.source-selection').length,0);
 assert.ok(dom.window.document.querySelector('.cm-content'));
 const editor=EditorView.findFromDOM(dom.window.document.querySelector('.cm-editor'));
 let serializations=0;
 const getText=SourceEditor.prototype.getText;
 SourceEditor.prototype.getText=function(){serializations++;return getText.call(this);};
 editor.dispatch({changes:{from:2,to:7,insert:'renamed'}});
 assert.equal(serializations,0,'typing must not serialize complete source');
 dom.window.document.getElementById('apply').click();await settle();
 assert.deepEqual(patchPayload,{revision:1,patches:[{start:2,end:7,text:'renamed'}]});
 assert.equal(serializations,0,'patch apply must serialize only changed regions');
 assert.equal(calls.filter(p=>p.startsWith('/api/source?')).length,1,'accepted draft is already known');
 assert.equal(dom.window.document.getElementById('draft-state').textContent,'No changes');
 SourceEditor.prototype.getText=getText;
 dom.window.document.querySelector('[data-node-id="c2"]').click();await settle();
 assert.ok(calls.some(p=>p.startsWith('/api/node-location?')));
 assert.ok(calls.some(p=>p.startsWith('/inspector?')));
 assert.equal(dom.window.document.getElementById('inspector').textContent,'Selected child');
 editor.dispatch({selection:{anchor:0,head:1}});await settle();await settle();
 assert.ok(calls.some(p=>p.startsWith('/api/locate?')),'user selections use indexed point lookup');
 assert.equal(calls.some(p=>p.includes('/api/source-map')),false);
 editor.destroy();dom.window.close();
});

async function applicationFixture(t,name,respond){
 const {dom}=editorDOM();
 dom.window.document.documentElement.innerHTML=await readFile(new URL('./index.html',import.meta.url),'utf8');
 globalThis.devicePixelRatio=1;
 dom.window.HTMLCanvasElement.prototype.getContext=()=>new Proxy({measureText:()=>({width:50})},{get:(target,key)=>target[key]||(()=>{}),set:(target,key,value)=>(target[key]=value,true)});
 globalThis.fetch=respond;
 globalThis.htmx={process(){}};
 await import(`./app.js?case=${name}-${Date.now()}`);
 const editor=EditorView.findFromDOM(dom.window.document.querySelector('.cm-editor'));
 t.after(()=>{for(const node of dom.window.document.querySelectorAll('.cm-editor'))EditorView.findFromDOM(node)?.destroy();dom.window.close();delete globalThis.htmx;});
 return {dom,editor,$:id=>dom.window.document.getElementById(id)};
}
const responseJSON=(body,status=200)=>new Response(JSON.stringify(body),{status,headers:{'content-type':'application/json'}});
const waitFor=async(predicate)=>{for(let n=0;n<150;n++){if(predicate())return;await new Promise(resolve=>setTimeout(resolve,10));}assert.fail('Timed out waiting for application state');};
const metadata=revision=>({revision,root:`r${revision}`,nodes:1,format:'json'});
const rootPage=revision=>({id:`r${revision}`,kind:'object',label:'$',total:0,children:[]});

test('Undo waits until the selected inspector finishes refreshing after Apply',async t=>{
 let revision=1,releaseInspector;
 const f=await applicationFixture(t,'history-refresh',async path=>{
  if(path==='/api/state')return responseJSON({...metadata(revision),canUndo:revision===2});
  if(path.startsWith('/api/source?'))return new Response('{"child":{}}');
  if(path.startsWith('/api/tree?'))return responseJSON({...rootPage(revision),total:1,children:[{id:`c${revision}`,label:'child',kind:'object',total:0}]});
  if(path.startsWith('/api/node-location?'))return responseJSON({revision,range:{id:`c${revision}`,start:9,end:11,parent:`r${revision}`,index:0},ancestors:[{id:`r${revision}`,offset:0}]});
  if(path.startsWith('/inspector?')){
   if(revision!==2)return new Response('<p>Selected child</p>');
   return new Promise(resolve=>{releaseInspector=()=>resolve(new Response('<p>Refreshed child</p>'));});
  }
  if(path.startsWith('/api/patch')){revision=2;return responseJSON({...metadata(2),canUndo:true});}
  if(path.startsWith('/api/history')){revision=3;return responseJSON({...metadata(3),canRedo:true});}
  throw new Error(`Unexpected request ${path}`);
 });
 f.dom.window.document.querySelector('[data-node-id="c1"]').click();
 await waitFor(()=>f.$('inspector').textContent.includes('Selected child'));
 f.editor.dispatch({changes:{from:0,insert:' '}});f.$('apply').click();
 await waitFor(()=>!!releaseInspector);
 assert.match(f.$('revision').textContent,/Revision 2/);
 assert.equal(f.$('undo').disabled,true,'Undo must not accept a click that ready() would discard');
 assert.equal(f.$('apply').disabled,true);
 releaseInspector();await waitFor(()=>!f.$('apply').disabled);
 assert.equal(f.$('undo').disabled,false);
 f.$('undo').click();await waitFor(()=>f.$('revision').textContent.includes('Revision 3'));
});

test('Cancel during follow-up tree loading cannot undo a committed source Apply in the UI',async t=>{
 let revision=1,releaseTree;
 const f=await applicationFixture(t,'committed-apply',async(path,options={})=>{
  if(path==='/api/state')return responseJSON(metadata(revision));
  if(path.startsWith('/api/source?'))return new Response('{}');
  if(path.startsWith('/api/patch')){revision=2;return responseJSON({job:'applied',status:'queued'},202);}
  if(path.startsWith('/api/jobs?'))return responseJSON({status:'complete',result:metadata(2)});
  if(path.startsWith('/api/tree?')){
   if(revision===1)return responseJSON(rootPage(1));
   return new Promise((resolve,reject)=>{
    releaseTree=()=>resolve(responseJSON(rootPage(2)));
    options.signal?.addEventListener('abort',()=>reject(new DOMException('Cancelled','AbortError')),{once:true});
   });
  }
  throw new Error(`Unexpected request ${path}`);
 });
 f.editor.dispatch({changes:{from:1,insert:'"x":1'}});f.$('apply').click();
 await waitFor(()=>!!releaseTree);
 f.$('cancel-operation').click();releaseTree();
 await waitFor(()=>!f.$('apply').disabled);
 assert.match(f.$('revision').textContent,/Revision 2/);
 assert.equal(f.$('draft-state').textContent,'No changes');
 assert.match(f.$('status').textContent,/Source applied/);
});

test('Inspector writes use tracked jobs, retain invalid drafts, and reconcile a committed cancellation race',async t=>{
 let revision=1,attempt=0,committed=false,f;const payloads=[],requests=[];
 f=await applicationFixture(t,'inspector-jobs',async(path,options={})=>{
  requests.push(path);
  if(path==='/api/state')return responseJSON(metadata(revision));
  if(path.startsWith('/api/source?'))return new Response(revision===1?'{}':'{"server":true}');
  if(path.startsWith('/api/tree?'))return responseJSON(rootPage(revision));
  if(path.startsWith('/api/edit?')){attempt++;payloads.push(JSON.parse(options.body));return responseJSON({job:`edit${attempt}`},202);}
  if(path==='/api/jobs/cancel')return responseJSON({error:'operation already committed'},400);
  if(path.startsWith('/api/jobs?')){
   if(attempt===1)return responseJSON({status:'failed',error:'Invalid JSON subtree'});
   if(!committed){committed=true;revision=2;f.$('cancel-operation').click();throw new DOMException('Aborted after commit','AbortError');}
   return responseJSON({status:'complete',result:metadata(2)});
  }
  throw new Error(`Unexpected request ${path}`);
 });
 f.$('inspector').innerHTML='<form hx-post="/actions/edit"><input name="op" value="replace"><input name="node" value="r1"><input name="revision" value="1"><input name="format" value="json"><textarea name="text" data-representation-node="r1" data-format="native" data-representation-full="true">{"broken":</textarea><button type="submit">Apply</button></form>';
 f.$('inspector').dispatchEvent(new f.dom.window.CustomEvent('htmx:afterSwap',{bubbles:true,detail:{target:f.$('inspector')}}));
 const form=f.$('inspector').querySelector('form');
 const begin=()=>{
  form.dispatchEvent(new f.dom.window.CustomEvent('htmx:confirm',{bubbles:true,cancelable:true,detail:{elt:form}}));
  const event=new f.dom.window.CustomEvent('htmx:beforeRequest',{bubbles:true,cancelable:true,detail:{elt:form,requestConfig:{parameters:{op:'replace',node:'r1',revision:'1',format:'json',text:'stale form field'}}}});
  form.dispatchEvent(event);assert.equal(event.defaultPrevented,true,'untracked htmx XHR must be stopped');
 };
 begin();await waitFor(()=>!f.$('apply').disabled);
 const draft=EditorView.findFromDOM(f.$('inspector').querySelector('.cm-editor'));
 assert.equal(draft.state.doc.toString(),'{"broken":');
 assert.equal(payloads[0].text,'{"broken":','tracked submission uses the full mounted editor');
 assert.match(form.querySelector('[data-inspector-error]').textContent,/Invalid JSON/);
 draft.dispatch({changes:{from:0,to:draft.state.doc.length,insert:'{"server":true}'}});
 begin();await waitFor(()=>!f.$('apply').disabled);
 assert.match(f.$('revision').textContent,/Revision 2/);
 assert.equal(f.editor.state.doc.toString(),'{"server":true}');
 assert.match(f.$('status').textContent,/applied|committed/i);
 assert.doesNotMatch(f.$('status').textContent,/cancelled|unchanged/i);
 assert.equal(requests.some(path=>path.startsWith('/actions/edit')),false);
 assert.equal(requests.filter(path=>path==='/api/state').length,1,'terminal job result replaces speculative state reads');
});

test('Cancel during committed import source reload still installs the canonical document',async t=>{
 let revision=1,releaseSource;
 const f=await applicationFixture(t,'committed-import',async(path,options={})=>{
  if(path==='/api/state')return responseJSON(metadata(revision));
  if(path.startsWith('/api/import')){revision=2;return responseJSON({job:'imported'},202);}
  if(path.startsWith('/api/jobs?'))return responseJSON({status:'complete',result:metadata(2)});
  if(path.startsWith('/api/source?')){
   if(revision===1)return new Response('{}');
   return new Promise((resolve,reject)=>{
    releaseSource=()=>resolve(new Response('{"imported":true}'));
    options.signal?.addEventListener('abort',()=>reject(new DOMException('Cancelled','AbortError')),{once:true});
   });
  }
  if(path.startsWith('/api/tree?'))return responseJSON(rootPage(revision));
  throw new Error(`Unexpected request ${path}`);
 });
 f.$('example').click();await waitFor(()=>!!releaseSource);
 f.$('cancel-operation').click();releaseSource();
 await waitFor(()=>!f.$('apply').disabled);
 assert.match(f.$('revision').textContent,/Revision 2/);
 assert.equal(f.editor.state.doc.toString(),'{"imported":true}');
 assert.match(f.$('status').textContent,/File opened/);
});

test('a failed canonical read after commit reports committed state rather than cancellation',async t=>{
 let revision=1;
 const f=await applicationFixture(t,'refresh-failure',async path=>{
  if(path==='/api/state')return responseJSON(metadata(revision));
  if(path.startsWith('/api/source?'))return new Response('{}');
  if(path.startsWith('/api/patch')){revision=2;return responseJSON(metadata(2));}
  if(path.startsWith('/api/tree?'))return revision===1?responseJSON(rootPage(1)):responseJSON({error:'Tree temporarily unavailable'},503);
  throw new Error(`Unexpected request ${path}`);
 });
 f.editor.dispatch({changes:{from:1,insert:'"x":1'}});f.$('apply').click();
 await waitFor(()=>!f.$('apply').disabled);
 assert.match(f.$('status').textContent,/committed at revision 2.*refreshing.*failed/i);
 assert.doesNotMatch(f.$('status').textContent,/cancelled|unchanged/i);
});

test('Original source selection remains synchronized when its normalized text matches the applied document',async t=>{
 const calls=[];
 const f=await applicationFixture(t,'original-sync',async path=>{
  calls.push(path);
  if(path==='/api/state')return responseJSON({...metadata(1),originalMatches:true});
  if(path.startsWith('/api/source?'))return new Response('{}');
  if(path.startsWith('/api/tree?'))return responseJSON(rootPage(1));
  if(path.startsWith('/api/locate?'))return responseJSON({revision:1,range:null,ancestors:[]});
  throw new Error(`Unexpected request ${path}`);
 });
 f.$('original-tab').click();await waitFor(()=>!f.$('apply').disabled || f.$('original-tab').getAttribute('aria-pressed')==='true');
 await new Promise(resolve=>setTimeout(resolve,50));
 f.editor.dispatch({selection:{anchor:0,head:1}});
 await waitFor(()=>calls.some(path=>path.startsWith('/api/locate?')));
});

test('stable scalar Apply refreshes loaded changed ancestors without rereading unrelated pages or moving the view',async t=>{
 const {TreeCanvas}=await import('./tree.js');
 let tree,revision=1;const requests=[];
 const originalInstall=TreeCanvas.prototype.install;
 t.mock.method(TreeCanvas.prototype,'install',function(...args){tree=this;return originalInstall.apply(this,args);});
 const source=JSON.stringify({a:Array(40).fill(100),b:Array(40).fill(200)});
 const scalarOffset=(value,index)=>{let offset=-1;for(let n=0;n<=index;n++)offset=source.indexOf(String(value),offset+1);return offset;};
 const childPage=(id,offset=0)=>({id,label:id,kind:'array',total:40,offset,children:Array.from({length:20},(_,n)=>{
  const index=offset+n;return {id:`${id}${index}`,label:`[${index}]`,kind:'number',value:String(id==='a'?(revision===2&&index===20?101:100):200),total:0};
 })});
 const root=()=>({id:'root',label:'$',kind:'object',total:2,offset:0,children:[childPage('a'),childPage('b')]});
 const state=()=>({revision,root:'root',nodes:83,format:'json',originalMatches:revision===1});
 const f=await applicationFixture(t,'stable-scalar',async path=>{
  requests.push(path);
  if(path==='/api/state')return responseJSON(state());
  if(path.startsWith('/api/source?'))return new Response(source);
  if(path.startsWith('/api/patch')){revision=2;return responseJSON({...state(),stableIDs:true,changedNodes:['a20','a','root']});}
  if(path.startsWith('/api/tree?')){const query=new URL(path,'http://localhost').searchParams;return responseJSON(query.get('node')==='root'?root():childPage(query.get('node'),Number(query.get('offset'))));}
  if(path.startsWith('/api/node-location?'))return responseJSON({revision,range:{id:'b20',parent:'b',index:20,start:scalarOffset(200,20),end:scalarOffset(200,20)+3},ancestors:[{id:'root',offset:0},{id:'b',offset:20}]});
  if(path.startsWith('/inspector?'))return new Response('<p>Selected b20</p>');
  throw new Error(`Unexpected request ${path}`);
 });
 await tree.callbacks.action({type:'toggle',id:'a'});await tree.callbacks.action({type:'toggle',id:'b'});
 await tree.callbacks.action({type:'page',id:'a',offset:20});await tree.callbacks.action({type:'page',id:'b',offset:20});
 await tree.callbacks.action({type:'select',id:'b20'});
 tree.view={x:333,y:-77,scale:0.72};
 const view=tree.view,records=tree.records,expanded=tree.expanded,unrelatedPage=tree.records.get('b');
 f.editor.dispatch({changes:{from:scalarOffset(100,20),to:scalarOffset(100,20)+3,insert:'101'}});
 const caret=f.editor.state.selection.main;
 requests.length=0;f.$('apply').click();await waitFor(()=>!f.$('apply').disabled);
 const pages=requests.filter(path=>path.startsWith('/api/tree?')).map(path=>{const query=new URL(path,'http://localhost').searchParams;return {id:query.get('node'),offset:Number(query.get('offset'))};});
 assert.deepEqual(pages,[{id:'root',offset:0},{id:'a',offset:20}]);
 assert.strictEqual(tree.records,records);assert.strictEqual(tree.expanded,expanded);
 assert.strictEqual(tree.records.get('b'),unrelatedPage,'root depth2 must not replace an unrelated cached child page');
 assert.equal(tree.records.get('a').offset,20);assert.equal(tree.records.get('b').offset,20);
 assert.equal(tree.records.get('a').children[0].value,'101');
 assert.equal(tree.selected,'b20');assert.strictEqual(tree.view,view);assert.deepEqual(tree.view,{x:333,y:-77,scale:0.72});
 assert.equal(tree.expanded.has('a'),true);assert.equal(tree.expanded.has('b'),true);
 assert.equal(f.editor.state.selection.main.from,caret.from);assert.equal(f.editor.state.selection.main.to,caret.to);
 assert.equal(f.$('draft-state').textContent,'No changes');
 assert.equal(requests.some(path=>path.startsWith('/api/source?')),false);
 assert.equal(requests.some(path=>path.startsWith('/api/node-location?')),false,'geometry-preserving edits retain source lookup cache');
});


test('Inspector structured values preserve exact text and selected types; delete confirms the submitted action',async t=>{
 const payloads=[];
 const f=await applicationFixture(t,'inspector-structured',async(path,options={})=>{
  if(path==='/api/state')return responseJSON(metadata(1));
  if(path.startsWith('/api/source?'))return new Response('{}');
  if(path.startsWith('/api/tree?'))return responseJSON(rootPage(1));
  if(path.startsWith('/api/edit?')){payloads.push(JSON.parse(options.body));return responseJSON({error:'Edit rejected for test'},400);}
  throw new Error(`Unexpected request ${path}`);
 });
 const submit=async(parameters,submitter)=>{
  const form=f.$('inspector').querySelector('form');
  form.dispatchEvent(new f.dom.window.CustomEvent('htmx:confirm',{bubbles:true,cancelable:true,detail:{elt:form,triggeringEvent:{submitter}}}));
  const event=new f.dom.window.CustomEvent('htmx:beforeRequest',{bubbles:true,cancelable:true,detail:{elt:form,requestConfig:{parameters,triggeringEvent:{submitter}}}});
  form.dispatchEvent(event);assert.equal(event.defaultPrevented,true);await waitFor(()=>!f.$('apply').disabled);
 };
 for(const [kind,value,text] of [['string','line\n"quoted"',JSON.stringify('line\n"quoted"')],['number','9007199254740993','9007199254740993'],['boolean','true','true'],['null','ignored','null']]){
  f.$('inspector').innerHTML='<form hx-post="/actions/edit"><input name="value"><button type="submit">Apply</button></form>';
  await submit({op:'replace',node:'r1',revision:'1',structured:'true',kind,value,confirm:'true'});
  const action=payloads.at(-1);
  assert.equal(action.text,text);assert.equal(action.format,'json');assert.equal(action.structured,true);assert.equal(action.confirm,true);
  assert.equal('kind' in action,false);assert.equal('value' in action,false);
 }
 const before=payloads.length;
 await submit({op:'replace',node:'r1',revision:'1',structured:'true',kind:'boolean',value:'123'});
 assert.equal(payloads.length,before);assert.match(f.$('inspector').textContent,/selected type/i);
 f.$('inspector').innerHTML='<form hx-post="/actions/edit"><input name="node" value="r1"><input name="revision" value="1"><button name="op" value="clone">Clone</button><button name="op" value="delete" data-destructive="true">Delete</button></form>';
 const remove=f.$('inspector').querySelector('[value="delete"]');let confirms=0,allow=false;
 globalThis.confirm=()=>{confirms++;return allow;};t.after(()=>delete globalThis.confirm);
 await submit(undefined,remove);assert.equal(payloads.length,before);assert.equal(confirms,1);
 allow=true;await submit(undefined,remove);
 assert.equal(confirms,2);assert.equal(payloads.at(-1).op,'delete');assert.equal(payloads.at(-1).node,'r1');
 assert.equal(payloads.at(-1).structured,false);assert.equal(payloads.at(-1).confirm,false);
});
