import test from 'node:test';
import assert from 'node:assert/strict';
import { mountConversionDialog } from './conversion.js';

function fixture({local=true, convert, getSource}={}) {
 const elements = new Map();
 for (const id of ['conversion','conversion-form','conversion-preview','conversion-save','conversion-filename','conversion-save-fields','conversion-save-label','conversion-save-help','conversion-error','confirm-convert','cancel-convert','close-convert']) {
  elements.set(id, {value:'',checked:false,disabled:false,hidden:false,open:false,listeners:new Map(),addEventListener(event,fn){this.listeners.set(event,fn);},showModal(){this.open=true;},close(){this.open=false;}});
 }
 const $=id=>elements.get(id), calls=[], downloads=[], installed=[];
 let revision=1;
 const panel=mountConversionDialog({getElementById:$},{revision:()=>revision,preview:async format=>({revision,format,text:'name: Alice\n',suggestedFilename:'my data.yaml',canSaveLocal:local,saveDirectory:local?'/data':''}),convert:async payload=>{calls.push(payload);if(convert)return convert(payload);return {revision:2,source:'name: Alice\n',savedPath:payload.saveFilename?'/data/'+payload.saveFilename:undefined};},install:async state=>installed.push(state),getSource,download:(text,name)=>downloads.push({text,name}),busy(){},status(){}});
 return {$,panel,calls,downloads,installed,change(){revision++;},submit:()=> $('conversion-form').listeners.get('submit')({preventDefault(){}})};
}

test('local conversion suggests a matching filename and saves the user-chosen name',async()=>{
 const f=fixture();await f.panel.open('yaml');
 assert.equal(f.$('conversion-filename').value,'my data.yaml');assert.match(f.$('conversion-save-help').textContent,/\/data/);
 f.$('conversion-save').checked=true;f.$('conversion-save').listeners.get('change')();assert.equal(f.$('conversion-save-fields').hidden,false);
 f.$('conversion-filename').value='another name.yaml';await f.submit();
 assert.deepEqual(f.calls,[{revision:1,format:'yaml',confirm:true,saveFilename:'another name.yaml'}]);assert.equal(f.installed.length,1);assert.equal(f.$('conversion').open,false);assert.equal(f.downloads.length,0);
});
test('conversion without saving makes no disk-save request or download',async()=>{
 const f=fixture();await f.panel.open('yaml');await f.submit();
 assert.deepEqual(f.calls,[{revision:1,format:'yaml',confirm:true}]);assert.equal(f.downloads.length,0);
});
test('uploads offer a named download without claiming access to the original directory',async()=>{
 const f=fixture({local:false});await f.panel.open('yaml');assert.match(f.$('conversion-save-label').textContent,/Download/);assert.match(f.$('conversion-save-help').textContent,/hide/i);
 f.$('conversion-save').checked=true;await f.submit();assert.equal(f.calls[0].saveFilename,undefined);assert.deepEqual(f.downloads,[{text:'name: Alice\n',name:'my data.yaml'}]);
});
test('save failures retain preview and editable filename for retry',async()=>{
 const f=fixture({convert:async()=>{throw new Error('File already exists; choose a different filename');}});await f.panel.open('yaml');f.$('conversion-save').checked=true;await f.submit();
 assert.equal(f.$('conversion').open,true);assert.match(f.$('conversion-error').textContent,/already exists/);assert.equal(f.$('conversion-filename').value,'my data.yaml');assert.equal(f.installed.length,0);assert.equal(f.$('confirm-convert').disabled,false);
});
test('stale previews, invalid names and cancelled dialogs never convert',async()=>{
 for(const name of ['', '../escape.yaml', 'wrong.json']) {const f=fixture();await f.panel.open('yaml');f.$('conversion-save').checked=true;f.$('conversion-filename').value=name;await f.submit();assert.equal(f.calls.length,0);assert.ok(f.$('conversion-error').textContent);}
 const f=fixture();await f.panel.open('yaml');f.change();await f.submit();assert.equal(f.calls.length,0);assert.match(f.$('conversion-error').textContent,/stale/i);
 const cancelled=fixture();await cancelled.panel.open('yaml');cancelled.$('cancel-convert').listeners.get('click')();assert.equal(cancelled.$('conversion').open,false);assert.equal(cancelled.calls.length,0);
});
test('conversion submissions are serialized while saving',async()=>{
 let finish;const pending=new Promise(resolve=>finish=resolve);const f=fixture({convert:()=>pending});await f.panel.open('yaml');const first=f.submit();await f.submit();assert.equal(f.calls.length,1);assert.equal(f.$('confirm-convert').disabled,true);finish({revision:2,source:'name: Alice\n'});await first;assert.equal(f.$('confirm-convert').disabled,false);
});

test('metadata-only conversion downloads explicit revision source rather than its bounded preview',async()=>{
 const reads=[];
 const f=fixture({local:false,convert:async()=>({revision:2}),getSource:async revision=>{reads.push(revision);return 'complete document';}});
 await f.panel.open('yaml');f.$('conversion-save').checked=true;await f.submit();
 assert.deepEqual(reads,[2]);assert.deepEqual(f.downloads,[{text:'complete document',name:'my data.yaml'}]);
});
