import test from 'node:test';
import assert from 'node:assert/strict';
import { mountFileBrowser } from './file-browser.js';

function fixture({browse,open,ready=()=>true}={}) {
 class Element {
  constructor(){this.value='';this.textContent='';this.hidden=false;this.disabled=false;this.checked=false;this.children=[];this.listeners=new Map();this.attributes=new Map();}
  addEventListener(name,fn){this.listeners.set(name,fn);} replaceChildren(...nodes){this.children=nodes;} append(...nodes){this.children.push(...nodes);} setAttribute(k,v){this.attributes.set(k,v);} showModal(){this.closed=false;} close(){this.closed=true;this.listeners.get('close')?.();} set innerHTML(_){throw Error('unsafe HTML');}
 }
 const elements=new Map(['file-browser-form','file-browser-path','file-browser-up','file-browser-hidden','file-browser-list','file-browser-status','local-path','local-file-form','file-open-error','open-document','open-document-title','file-browser-help','file-browser-open','local-path-help'].map(id=>[id,new Element()]));
 const $=id=>elements.get(id),calls=[],opened=[];let revision=1;
 const listing={revision:1,path:'/data',parent:'/',truncated:false,entries:[{name:'nested',path:'/data/nested',directory:true},{name:'<sample>.yaml',path:'/data/<sample>.yaml',directory:false}]};
 const picker=mountFileBrowser({getElementById:$,createElement:()=>new Element()},{revision:()=>revision,browse:async payload=>{calls.push(payload);return browse?browse(payload):listing;},open:async payload=>{opened.push(payload);if(open)return open(payload);return true;},ready});
 return {$,picker,calls,opened,listing,change(){revision++;},submit:()=> $('local-file-form').listeners.get('submit')({preventDefault(){}})};
}

test('custom picker browses the server, selects a full path and opens through local-open',async()=>{
 const f=fixture();await f.picker.show();assert.deepEqual(f.calls,[{revision:1,path:'',showHidden:false}]);assert.equal(f.$('file-browser-path').value,'/data');
 const [directory,file]=f.$('file-browser-list').children;
 assert.equal(file.children[0].textContent,'<sample>.yaml');await file.children[0].listeners.get('click')();assert.equal(f.$('local-path').value,'/data/<sample>.yaml');assert.equal(f.opened.length,0);
 await f.submit();assert.deepEqual(f.opened,[{revision:1,path:'/data/<sample>.yaml'}]);assert.equal(f.$('open-document').closed,true);
 await directory.children[0].listeners.get('click')();assert.equal(f.calls.at(-1).path,'/data/nested');
});
test('shared schema picker filters files and returns a path without opening a working document',async()=>{
 const f=fixture({browse:async()=>({...f.listing,entries:[...f.listing.entries,{name:'rules.json',path:'/data/rules.json',directory:false}]})});
 const selection=f.picker.pick('schema');await new Promise(resolve=>setImmediate(resolve));
 assert.equal(f.$('open-document-title').textContent,'Choose validation schema');
 assert.equal(f.$('file-browser-list').children.length,2);
 await f.$('file-browser-list').children[1].children[0].listeners.get('click')();await f.submit();
 assert.deepEqual(await selection,{path:'/data/rules.json'});assert.equal(f.opened.length,0);
});
test('cancelling a shared picker returns no selection and restores normal document opening',async()=>{
 const f=fixture();const selection=f.picker.pick('comparison');f.$('open-document').close();assert.equal(await selection,null);
 await f.picker.show();f.$('local-path').value='/data/working.yaml';await f.submit();assert.equal(f.opened.length,1);
});
test('Up, typed directories and hidden-file toggle request server-side navigation',async()=>{
 const f=fixture();await f.picker.show();await f.$('file-browser-up').listeners.get('click')();assert.equal(f.calls.at(-1).path,'/');
 f.$('file-browser-path').value='/other';await f.$('file-browser-form').listeners.get('submit')({preventDefault(){}});assert.equal(f.calls.at(-1).path,'/other');
 f.$('file-browser-hidden').checked=true;await f.$('file-browser-hidden').listeners.get('change')();assert.equal(f.calls.at(-1).showHidden,true);
});
test('failed browsing and opening show errors and retain an editable path',async()=>{
 const f=fixture({browse:async()=>{throw Error('Permission denied');},open:async()=>{throw Error('Invalid companion schema');}});await f.picker.show();assert.match(f.$('file-browser-status').textContent,/Permission denied/);
 f.$('local-path').value='/data/bad.json';await f.submit();assert.match(f.$('file-open-error').textContent,/Invalid companion schema/);assert.equal(f.$('open-document').closed,undefined);assert.equal(f.$('local-path').value,'/data/bad.json');
});
test('stale directory responses cannot replace current browsing results',async()=>{
 const pending=[];const f=fixture({browse:()=>new Promise(resolve=>pending.push(resolve))});const first=f.picker.show();const second=f.picker.show();pending[1]({...f.listing,path:'/new'});await second;pending[0]({...f.listing,path:'/old'});await first;assert.equal(f.$('file-browser-path').value,'/new');
 const g=fixture({browse:()=>new Promise(resolve=>pending.push(resolve))});const loading=g.picker.show();g.change();pending.at(-1)(g.listing);await loading;assert.equal(g.$('file-browser-list').children.length,0);
});
test('opening is serialized and rejected draft guards never open files',async()=>{
 let finish;const f=fixture({open:()=>new Promise(resolve=>finish=resolve)});await f.picker.show();f.$('local-path').value='/data/test.json';const first=f.submit();await f.submit();assert.equal(f.opened.length,1);finish(true);await first;
 const guarded=fixture({ready:()=>false});await guarded.picker.show();guarded.$('local-path').value='/data/test.json';await guarded.submit();assert.equal(guarded.opened.length,0);
});
