import test from 'node:test';
import assert from 'node:assert/strict';

test('whole-document operations poll queued work and expose stages before metadata result', async () => {
 const {requestJSON}=await import('./request.js');
 const calls=[],stages=[];
 const states=[{status:'running',stage:'indexing'},{status:'complete',result:{revision:2,root:'r2'}}];
 const fetcher=async(path,options)=>{calls.push({path,options});return new Response(JSON.stringify(path.startsWith('/api/open')?{job:'job1',status:'queued'}:states.shift()),{status:path.startsWith('/api/open')?202:200});};
 const result=await requestJSON('/api/open',{method:'POST',body:'{}'},{fetcher,progress:stage=>stages.push(stage),pollMs:0});
 assert.match(calls[0].path,/async=true/);
 assert.equal(calls[1].path,'/api/jobs?id=job1');
 assert.deepEqual(result,{revision:2,root:'r2'});
 assert.ok(stages.includes('indexing'));
});

test('cancelling polling cancels the server job and rejects without returning a late result', async () => {
 const {requestJSON}=await import('./request.js');
 const abort=new AbortController(),calls=[];let cancelled=false;
 const fetcher=async(path,options)=>{calls.push({path,options});
  if(path.startsWith('/api/open'))return new Response(JSON.stringify({job:'job2'}),{status:202});
  if(path==='/api/jobs/cancel'){cancelled=true;return new Response('{\"status\":\"running\",\"stage\":\"cancelling\"}');}
  if(cancelled)return new Response('{\"status\":\"cancelled\"}');
  abort.abort();throw new DOMException('cancelled','AbortError');
 };
 await assert.rejects(requestJSON('/api/open',{method:'POST',body:'{}',signal:abort.signal},{fetcher,pollMs:0}),{name:'AbortError'});
 const cancel=calls.find(call=>call.path==='/api/jobs/cancel');
 assert.ok(cancel);assert.deepEqual(JSON.parse(cancel.options.body),{id:'job2'});
 assert.equal(cancel.options.signal,undefined,'cancel request must survive the aborted operation signal');
});

test('cancellation while enqueue response arrives still obtains the job ID and cancels it', async () => {
 const {requestJSON}=await import('./request.js');
 const abort=new AbortController();let cancelled=false;
 const fetcher=async(path,options)=>{
  if(path.startsWith('/api/open')){assert.equal(options.signal,undefined);abort.abort();return new Response('{"job":"late"}',{status:202});}
  if(path==='/api/jobs/cancel'){cancelled=true;return new Response('{"status":"cancelled"}');}
  return new Response('{"status":"cancelled"}');
 };
 await assert.rejects(requestJSON('/api/open',{method:'POST',body:'{}',signal:abort.signal},{fetcher,pollMs:0}),{name:'AbortError'});
 assert.equal(cancelled,true);
});

test('a commit that wins the cancellation race returns its metadata for UI reconciliation', async () => {
 const {requestJSON}=await import('./request.js');
 const abort=new AbortController();let reads=0;
 const fetcher=async(path)=>{
  if(path.startsWith('/api/open'))return new Response('{"job":"committed"}',{status:202});
  if(path==='/api/jobs/cancel')return new Response('{"error":"operation already committed"}',{status:400});
  if(reads++===0){abort.abort();throw new DOMException('cancelled','AbortError');}
  return new Response('{"status":"complete","result":{"revision":3,"root":"r3"}}');
 };
 const result=await requestJSON('/api/open',{method:'POST',body:'{}',signal:abort.signal},{fetcher,pollMs:0});
 assert.deepEqual(result,{revision:3,root:'r3'});
});


test('accepted job cancellation waits for terminal acknowledgement before rejecting',async()=>{
 const {requestJSON}=await import('./request.js');
 const abort=new AbortController();let cancelled=false,terminalReads=0;
 const fetcher=async(path)=>{
  if(path.startsWith('/api/edit'))return new Response('{"job":"pending"}',{status:202});
  if(path==='/api/jobs/cancel'){cancelled=true;return new Response('{"status":"running","stage":"cancelling"}');}
  if(!cancelled){abort.abort();throw new DOMException('Cancelled','AbortError');}
  terminalReads++;return new Response(JSON.stringify({status:terminalReads===1?'running':'cancelled',stage:'cancelling'}));
 };
 await assert.rejects(requestJSON('/api/edit',{method:'POST',body:'{}',signal:abort.signal},{fetcher,pollMs:0}),{name:'AbortError'});
 assert.equal(terminalReads,2,'cancelling acknowledgement is not a terminal result');
});
