import { build } from 'esbuild';
import { readFile, writeFile } from 'node:fs/promises';
// npm ci + npm run build:web recreates this checked-in, runtime-offline asset.
await build({entryPoints:['internal/server/web/app.js'],outfile:'internal/server/web/app.bundle.js',bundle:true,format:'esm',platform:'browser',target:['es2022'],minify:true,legalComments:'linked',charset:'utf8'});

// Carry every bundled production dependency's license alongside the local asset.
const lock=JSON.parse(await readFile('package-lock.json','utf8'));
const notices=[];
for(const [directory,entry] of Object.entries(lock.packages)){
 if(!directory||entry.dev)continue;
 const info=JSON.parse(await readFile(`${directory}/package.json`,'utf8'));
 let license;
 for(const file of ['LICENSE','LICENSE.md','LICENSE.txt']){
  try{license=await readFile(`${directory}/${file}`,'utf8');break;}catch(error){if(error.code!=='ENOENT')throw error;}
 }
 if(!license)throw new Error(`Missing bundled dependency license: ${info.name}`);
 notices.push(`${info.name} ${entry.version}\n${license.trim()}`);
}
await writeFile('internal/server/web/vendor/editor.LICENSE',notices.join('\n\n--------------------\n\n')+'\n');
