import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileImport, selectUploadDocument, isDocumentFile } from './file-import.js';
const file=(name,text,path='')=>({name,size:new TextEncoder().encode(text).length,webkitRelativePath:path,arrayBuffer:async()=>new TextEncoder().encode(text).buffer});
test('selecting a document and matching sidecar attaches the schema in one import',async()=>{
 const data=file('my.data.yaml','name: Alice'),schema=file('my.data.schema.json','false');
 const files=[schema,data];assert.equal(selectUploadDocument(files),data);
 const payload=await readFileImport(data,files);
 assert.deepEqual(payload,{text:'name: Alice',format:'yaml',filename:'my.data.yaml',schema:{filename:'my.data.schema.json',text:'false'}});
});
test('folder imports match siblings in the selected document directory',async()=>{
 const data=file('data.csv','name\nAlice\n','folder/a/data.csv');
 const right=file('data.schema.json','false','folder/a/data.schema.json'),wrong=file('data.schema.json','true','folder/b/data.schema.json');
 assert.equal((await readFileImport(data,[data,wrong,right])).schema.text,'false');
 assert.equal((await readFileImport(data,[data,wrong])).schema,undefined);
});
test('missing companions and extension aliases load normally without a schema',async()=>{
 for(const [name,format] of [['data.yml','yaml'],['data.ndjson','jsonl'],['DATA.JSON','json']]) {const data=file(name,'{}');const payload=await readFileImport(data,[data]);assert.equal(payload.format,format);assert.equal(payload.schema,undefined);}
 assert.equal(isDocumentFile(file('data.schema.json','{}')),false);
 assert.throws(()=>selectUploadDocument([file('a.json','{}'),file('b.json','{}')]),/one document/i);
});
test('unreadable or oversized sidecars reject import before sending any content',async()=>{
 const data=file('data.json','{}'),schema=file('data.schema.json','true');schema.arrayBuffer=async()=>{throw new Error('Read failed');};
 await assert.rejects(readFileImport(data,[data,schema]),/data.schema.json.*Read failed/);
 schema.size=1024*1024+1;await assert.rejects(readFileImport(data,[data,schema]),/1 MiB/);
});
