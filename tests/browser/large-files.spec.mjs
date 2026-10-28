import {test,expect} from '@playwright/test';
import {readFile,writeFile} from 'node:fs/promises';
import {tmpdir,cpus} from 'node:os';
import {join} from 'node:path';

const percentile=(values,p)=>values.length?values.slice().sort((a,b)=>a-b)[Math.min(values.length-1,Math.ceil(values.length*p)-1)]:0;

test('large document renders a bounded editor, accepts input and preserves undo',async({page,baseURL})=>{
 const port=Number(process.env.DATATREE_BROWSER_PORT||18917);
 const input=JSON.parse(await readFile(join(tmpdir(),`datatree-browser-${port}.json`),'utf8'));
 const errors=[];page.on('pageerror',error=>errors.push(error.name));
 await page.route('**/*',route=>route.request().url().startsWith(baseURL+'/')?route.continue():route.abort());
 await page.addInitScript(()=>{
  window.__dtPerformance={keys:[],paint:[],scroll:[],longTasks:[],eventDurations:[],eventTimingSupported:false};
  document.addEventListener('keydown',event=>{
   if(!event.target.closest?.('#source-pane'))return;
   const start=event.timeStamp;
   requestAnimationFrame(()=>{
    window.__dtPerformance.keys.push(performance.now()-start);
    requestAnimationFrame(()=>window.__dtPerformance.paint.push(performance.now()-start));
   });
  },true);
  document.addEventListener('scroll',event=>{
   if(!event.target.closest?.('#source-pane'))return;
   const start=performance.now();requestAnimationFrame(()=>window.__dtPerformance.scroll.push(performance.now()-start));
  },true);
  try{new PerformanceObserver(list=>{for(const entry of list.getEntries())window.__dtPerformance.longTasks.push(entry.duration);}).observe({type:'longtask',buffered:true});}catch{}
  try{
   new PerformanceObserver(list=>{for(const entry of list.getEntries())if(entry.name==='keydown'&&entry.target?.closest?.('#source-pane'))window.__dtPerformance.eventDurations.push(entry.duration);}).observe({type:'event',durationThreshold:16,buffered:true});
   window.__dtPerformance.eventTimingSupported=true;
  }catch{}
 });
 const profiler=process.env.DATATREE_BROWSER_PROFILE==='1'?await page.context().newCDPSession(page):null;
 if(profiler){await profiler.send('Profiler.enable');await profiler.send('Profiler.start');}
 const navigationStarted=Date.now();
 await page.goto('/');
 await expect(page.locator('#revision')).toContainText('Revision 1');
 const uiReadyMS=Date.now()-navigationStarted;
 const backendInitialMS=(input.readyAt||input.startedAt)-input.startedAt;
 const openMS=backendInitialMS+uiReadyMS;
 const source=page.locator('#source-pane .cm-content');
 await expect(source).toBeVisible();
 const renderedLines=await page.locator('#source-pane .cm-line').count();
 expect(renderedLines).toBeLessThan(500);
 await source.click();await page.keyboard.press('ArrowLeft');
 for(let i=0;i<30;i++){await page.keyboard.type('x');await page.keyboard.press('Backspace');}
 await page.waitForTimeout(100);
 const scroller=page.locator('#source-pane .cm-scroller');
 for(let i=0;i<10;i++){
  await scroller.evaluate((element,step)=>{element.scrollTop=(step+1)*1000;},i);
  await page.waitForTimeout(20);
 }
 await page.locator('#discard').click();
 const typing=await page.evaluate(()=>window.__dtPerformance);
 // Event Timing omits events shorter than its16ms threshold. Treat these as
 //16ms upper bounds rather than selectively reporting only slower entries.
 const keyDurations=typing.eventDurations.concat(Array(Math.max(0,typing.keys.length-typing.eventDurations.length)).fill(16));
 const metrics={format:input.format,provided:input.provided,inputBytes:input.bytes,cpu:cpus()[0].model,openMS,backendInitialMS,uiReadyMS,renderedLines,typingP95:percentile(typing.eventTimingSupported?keyDurations:typing.keys,.95),keyFrameP95:percentile(typing.keys,.95),afterPaintP95:percentile(typing.paint,.95),scrollFrameP95:percentile(typing.scroll,.95),longTaskMax:Math.max(0,...typing.longTasks)};
 if(profiler){const {profile}=await profiler.send('Profiler.stop');await writeFile(join(tmpdir(),'datatree-browser-cpu.json'),JSON.stringify(profile));await profiler.detach();}
 // Private input remains unmodified. The generated format fixtures have known
 // paths so the public search/navigation/edit/undo workflow can be exercised.
 if(!input.provided){
  const path=input.format==='csv'?"$[0].name":input.format==='jsonl'?"$[0].settings.replicas":"$.rows[0].settings.replicas";
  await page.locator('#find-mode').selectOption('query');await page.locator('#find-text').fill(path);
  const begin=Date.now();await page.locator('#find-submit').click();
  await expect(page.locator('#inspector .selection-title')).toBeVisible();
  await expect(page.locator('#find-status')).toContainText('1 of 1');
  metrics.revealMS=Date.now()-begin;
  await source.focus();await page.keyboard.press('ArrowRight');await page.keyboard.press('Backspace');await page.keyboard.type(input.format==='csv'?'x':'4');
  const apply=Date.now();await page.locator('#apply').click();
  await expect(page.locator('#revision')).toContainText('Revision 2');
  metrics.editMS=Date.now()-apply;
  await page.locator('#undo').click();await expect(page.locator('#revision')).toContainText('Revision 3');
  const state=await(await page.request.get('/api/state')).json();expect(state.originalMatches).toBe(true);
 }
 const session=await page.context().newCDPSession(page);
 const heap=await session.send('Runtime.getHeapUsage');metrics.jsHeapUsedBytes=heap.usedSize;
 expect(errors).toEqual([]);
 const out=process.env.DATATREE_BROWSER_RESULTS||join(tmpdir(),`datatree-browser-results-${input.format}-${Math.round(input.bytes/1048576)}.json`);
 await writeFile(out,JSON.stringify(metrics,null,2)+'\n');
 console.log(JSON.stringify(metrics));
 if(process.env.DATATREE_PERF_STRICT==='1'){
  expect(metrics.typingP95).toBeLessThan(50);
  expect(metrics.openMS).toBeLessThan(input.bytes>20*1048576?10000:2000);
  if(metrics.editMS!==undefined)expect(metrics.editMS).toBeLessThan(500);
 }
});
