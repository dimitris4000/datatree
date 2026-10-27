import test from 'node:test';
import assert from 'node:assert/strict';
import { ValidationController, mountValidationPanel, canValidateDocument } from './validation.js';
import { validationBadge, TreeCanvas, layout } from './tree.js';

const metadata = { filename: 'schema.json', token: 's1' };
const deferred = () => { let resolve, reject; const promise = new Promise((a,b) => {resolve=a;reject=b;});return {promise,resolve,reject};};
const result = (revision=1, schema=metadata) => ({revision,schema,valid:false,complete:true,truncated:false,diagnostics:[{target:'2',instancePath:'/mode',documentIndex:0,location:'line 1, column 2',keyword:'enum',schemaPath:'schema#/properties/mode',message:'<img onerror=bad>',suggestions:['"prod"','"dev"']}],marks:{'1':{contains:true},'2':{direct:true}}});
function fixture(options={}) {
 let revision=1,calls=0;
 const c=new ValidationController({revision:()=>revision,attach:async()=>({revision,schema:metadata}),clear:async()=>({revision,schema:null}),validate:async()=>{calls++;return result(revision);},navigate:async()=>true,render(){},...options});
 return {c,get calls(){return calls;},change(){revision++;c.install(metadata);}};
}
test('CSV, YAML and JSONL documents can attach schemas and run validation in the UI', async()=>{
 for (const format of ['csv', 'yaml', 'jsonl']) {
  const f=fixture({available:()=>canValidateDocument({root:'1',format})});
  await f.c.replace(async()=>({text:'true'}));
  assert.equal(f.c.schema?.token,'s1');
  await f.c.run();
  assert.equal(f.c.result?.complete,true);
  assert.equal(await f.c.choose(0),true);
 }
});
test('schema attachment and panel opening never validate; explicit run does',async()=>{
 const f=fixture();await f.c.setVisible(true);await f.c.replace(async()=>({text:'true'}));assert.equal(f.calls,0);assert.equal(f.c.schema.token,'s1');assert.equal(f.c.result,null);
 await f.c.run();assert.equal(f.calls,1);assert.equal(f.c.result.valid,false);await f.c.choose(0);assert.equal(f.c.index,0);
 await f.c.setVisible(false);assert.equal(f.c.visible,false);await f.c.setVisible(true);assert.equal(f.calls,1);
});
test('applied revisions retain attachment but stale results cannot navigate',async()=>{
 let navigation=0;const f=fixture({navigate:async()=>{navigation++;return true;}});f.c.install(metadata);await f.c.run();f.change();assert.equal(f.c.stale,true);assert.equal(f.c.schema.token,'s1');assert.equal(await f.c.choose(0),false);assert.equal(navigation,0);
 await f.c.run();assert.equal(f.c.stale,false);assert.equal(f.c.result.revision,2);f.c.install(null);assert.equal(f.c.result,null);assert.equal(f.c.schema,null);
});
test('replacement failure preserves results and attachment; successful clear removes both',async()=>{
 const f=fixture();f.c.install(metadata);await f.c.run();const prior=f.c.result;f.c.attach=async()=>{throw new Error('bad schema');};await f.c.replace(async()=>({text:'bad'}));assert.equal(f.c.result,prior);assert.equal(f.c.schema,metadata);assert.match(f.c.error,/bad schema/);
 await f.c.clearSchema();assert.equal(f.c.schema,null);assert.equal(f.c.result,null);
});
test('late validation responses cannot survive close, revision changes or schema replacement',async()=>{
 for(const action of ['close','revision','schema']){
  const pending=deferred(),f=fixture({validate:()=>pending.promise});f.c.install(metadata);await f.c.setVisible(true);const running=f.c.run();
  if(action==='close')await f.c.setVisible(false);else if(action==='revision')f.change();else f.c.install({...metadata,token:'s2'});
  pending.resolve(result());await running;assert.equal(f.c.result,null);assert.equal(f.c.loading,false);
 }
 const f=fixture({validate:async()=>result(99)});f.c.install(metadata);await f.c.run();assert.equal(f.c.result,null);assert.match(f.c.error,/changed|stale/i);
});
test('uploads serialize, close retains attached metadata, and cancelled navigation stays unselected',async()=>{
 const pending=deferred();let uploads=0;const f=fixture({attach:async()=>{uploads++;return pending.promise;},navigate:async()=>false});
 await f.c.setVisible(true);const uploading=f.c.replace(async()=>({text:'true'}));await Promise.resolve();await f.c.replace(async()=>({text:'false'}));assert.equal(uploads,1);await f.c.setVisible(false);pending.resolve({revision:1,schema:metadata});await uploading;assert.equal(f.c.schema.token,'s1');assert.equal(f.c.result,null);
 await f.c.setVisible(true);f.c.validate=async()=>result();await f.c.run();assert.equal(await f.c.choose(0),false);assert.equal(f.c.index,-1);
});

function panelDocument(){
 class Element{constructor(tag){this.tagName=tag;this.children=[];this.listeners=new Map();this.attributes=new Map();this.classList={toggle(){}};this.value='';this.hidden=false;this.disabled=false;}addEventListener(e,f){this.listeners.set(e,f);}append(...nodes){this.children.push(...nodes);}replaceChildren(...nodes){this.children=nodes;}setAttribute(k,v){this.attributes.set(k,v);}set innerHTML(_){throw new Error('unsafe HTML');}}
 const elements=new Map(['toggle','panel','file','filename','text','attach','clear','run','status','results','previous','next','inspector'].map(id=>['validation-'+id,new Element()]));return {getElementById:id=>elements.get(id),createElement:tag=>new Element(tag)};
}
test('schema selection uses the shared picker path and cancellation retains validation results',async()=>{
 const doc=panelDocument(),calls=[];let choice={path:'/data/rules.json'};
 const panel=mountValidationPanel(doc,{revision:()=>1,pickFile:async()=>choice,attach:async(payload,revision)=>{calls.push({payload,revision});return {revision,schema:metadata};},clear:async()=>({revision:1,schema:null}),validate:async()=>result(),navigate:async()=>true,highlight(){},selected:()=> '2'});
 await doc.getElementById('validation-file').listeners.get('click')();
 assert.deepEqual(calls,[{payload:{path:'/data/rules.json'},revision:1}]);assert.equal(panel.controller.schema.token,'s1');
 await panel.controller.run();const prior=panel.controller.result;choice=null;
 await doc.getElementById('validation-file').listeners.get('click')();assert.equal(calls.length,1);assert.equal(panel.controller.result,prior);assert.equal(panel.controller.error,'');
});
test('panel safely renders errors, suggestions, Inspector details and stale/hidden marks',async()=>{
 const doc=panelDocument(),$=id=>doc.getElementById('validation-'+id);let marks;
 const panel=mountValidationPanel(doc,{revision:()=>1,attach:async()=>({revision:1,schema:metadata}),clear:async()=>({revision:1,schema:null}),validate:async()=>result(),navigate:async()=>true,highlight:m=>marks=m,selected:()=> '2'});
 panel.controller.install(metadata);await $('toggle').listeners.get('click')();await $('run').listeners.get('click')();assert.equal(marks['2'].direct,true);
 const texts=e=>[e.textContent||'',...e.children.flatMap(texts)].join(' ');
 assert.match(texts($('results')),/<img onerror=bad>/);assert.match(texts($('results')),/prod/);assert.match(texts($('inspector')),/prod/);assert.match(texts($('results')),/schema#\/properties\/mode/);
 await $('toggle').listeners.get('click')();assert.deepEqual(marks,{});assert.equal($('inspector').hidden,true);
});
test('validation and diff canvas labels coexist and can clear independently',()=>{
 assert.match(validationBadge({direct:true}).label,/error/i);assert.match(validationBadge({contains:true}).label,/Contains errors/);assert.equal(validationBadge(),null);
 const texts=[],ctx=new Proxy({fillText:t=>texts.push(t),measureText:t=>({width:String(t).length*6})},{get:(t,k)=>k in t?t[k]:()=>{}});
 const root={id:'1',kind:'object',label:'$',total:1,children:[{id:'2',kind:'string',label:'mode',value:'bad'}]};const tree=Object.create(TreeCanvas.prototype);Object.assign(tree,{canvas:{width:600,getContext:()=>ctx},width:600,height:400,view:{x:0,y:0,scale:1},callbacks:{},graph:layout(root,new Map(),new Set(['1']))});
 tree.setDiffMarks({'1':{contains:true},'2':{changed:true}});tree.setValidationMarks({'1':{contains:true},'2':{direct:true}});assert.ok(texts.some(t=>t.includes('Contains errors')));assert.ok(texts.some(t=>/Error/.test(t)));assert.ok(texts.some(t=>t.includes('Changed')));
 texts.length=0;tree.setValidationMarks({});assert.ok(texts.some(t=>t.includes('Changed')));assert.equal(texts.some(t=>/Error|Contains errors/.test(t)),false);
});
