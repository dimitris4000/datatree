import {spawn} from 'node:child_process';
import {mkdtemp,writeFile,stat,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join,extname} from 'node:path';
const dir=await mkdtemp(join(tmpdir(),'datatree-browser-'));
const port=Number(process.env.DATATREE_BROWSER_PORT||18917);
const env={...process.env,GOCACHE:process.env.GOCACHE||'/tmp/datatree-go-cache'};
const run=(cmd,args)=>new Promise((resolve,reject)=>{
 const child=spawn(cmd,args,{env,stdio:['ignore','inherit','inherit']});
 child.once('error',reject);child.once('exit',code=>code===0?resolve():reject(new Error(`${cmd} exited ${code}`)));
});
let format=process.env.DATATREE_BROWSER_FORMAT||'json';
let file=process.env.DATATREE_BROWSER_FILE;
const provided=!!file;
if(provided){format=extname(file).slice(1).toLowerCase();if(format==='yml')format='yaml';if(format==='ndjson')format='jsonl';}
if(!['json','jsonl','yaml','csv'].includes(format))throw new Error('unsupported browser fixture format');
if(!file){
 file=join(dir,`fixture.${format}`);
 const mib=Number(process.env.DATATREE_BROWSER_MIB||10);
 if(!Number.isInteger(mib)||mib<1||mib>100)throw new Error('DATATREE_BROWSER_MIB must be 1..100');
 await run('go',['run','./tests/browser/generate.go','-format',format,'-mib',String(mib),'-path',file]);
}
const binary=join(dir,'datatree');
await run('go',['build','-o',binary,'./cmd/datatree']);
const info={format,provided,bytes:(await stat(file)).size,startedAt:Date.now()};
await writeFile(join(tmpdir(),`datatree-browser-${port}.json`),JSON.stringify(info));
const child=spawn(binary,['-listen',`127.0.0.1:${port}`,'-max-file-size-mb','100','-max-nodes','4000000',file],{env,stdio:['ignore','pipe','inherit']});
child.stdout.on('data',chunk=>{
 process.stdout.write(chunk);
 if(!info.readyAt&&chunk.toString().includes('Data Tree:')){
  info.readyAt=Date.now();void writeFile(join(tmpdir(),`datatree-browser-${port}.json`),JSON.stringify(info));
 }
});
let stopped=false;
async function stop(){if(stopped)return;stopped=true;child.kill('SIGTERM');}
process.on('SIGTERM',stop);process.on('SIGINT',stop);
child.once('exit',async code=>{await rm(dir,{recursive:true,force:true});process.exit(code||0);});
child.once('error',error=>{console.error(error.message);process.exit(1);});
