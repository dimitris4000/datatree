const asyncMutations=new Set(['/api/import','/api/open','/api/apply','/api/patch','/api/edit','/api/history','/api/diff-revert','/api/convert']);
function pause(ms,signal){
 return new Promise((resolve,reject)=>{
  if(signal?.aborted){reject(new DOMException('Operation cancelled.','AbortError'));return;}
  const abort=()=>{clearTimeout(timer);reject(new DOMException('Operation cancelled.','AbortError'));};
  const timer=setTimeout(()=>{signal?.removeEventListener('abort',abort);resolve();},ms);
  signal?.addEventListener('abort',abort,{once:true});
 });
}
export async function requestJSON(path,options={}, {fetcher=globalThis.fetch,progress=()=>{},pollMs=100}={}){
 const asynchronous=options.method==='POST'&&asyncMutations.has(path)&&!(path==='/api/convert'&&JSON.parse(options.body||'{}').saveFilename!==undefined);
 if(asynchronous)path+='?async=true';
 // Obtain the ID even if cancellation arrives while the short enqueue request
 // finishes, so detached server work can always receive an explicit cancel.
 const response=await fetcher(path,asynchronous?{...options,signal:undefined}:options),result=await response.json();
 if(!response.ok)throw new Error(result.error||`Request failed (${response.status})`);
 if(response.status!==202||!result.job)return result;
 const signal=options.signal,id=result.job;
 let cancelling=null;
 const cancel=()=>{cancelling ||= fetcher('/api/jobs/cancel',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id})}).then(response=>response.json()).catch(()=>null);};
 signal?.addEventListener('abort',cancel,{once:true});
 if(signal?.aborted)cancel();
 try{
  progress('queued');
  for(;;){
   await pause(pollMs,signal);
   const read=await fetcher(`/api/jobs?id=${encodeURIComponent(id)}`,{signal});
   const job=await read.json();
   if(!read.ok)throw new Error(job.error||'Progress request failed.');
   progress(job.stage||job.status);
   if(job.status==='complete')return job.result;
   if(job.status==='failed')throw new Error(job.error||'Operation failed.');
   if(job.status==='cancelled')throw new DOMException('Operation cancelled.','AbortError');
  }
 }catch(error){
  if(signal?.aborted&&cancelling){
   const cancellation=await cancelling;
   // Cancellation acknowledgement can mean only "cancelling". Wait for the
   // tracked job's terminal result, independently of the aborted UI signal.
   if(cancellation?.status==='cancelled')throw new DOMException('Operation cancelled.','AbortError');
   for(;;){
    const response=await fetcher(`/api/jobs?id=${encodeURIComponent(id)}`);
    const job=await response.json();
    if(!response.ok)throw new Error(job.error||'Unable to confirm operation outcome.');
    if(job.status==='complete')return job.result;
    if(job.status==='failed')throw new Error(job.error||'Operation failed.');
    if(job.status==='cancelled')throw new DOMException('Operation cancelled.','AbortError');
    progress(job.stage||job.status);await pause(pollMs);
   }
  }
  throw error;
 }finally{signal?.removeEventListener('abort',cancel);if(cancelling)await cancelling;}
}
