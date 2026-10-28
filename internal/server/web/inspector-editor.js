import { SourceEditor } from './source-editor.js';

// Only open details request a bounded preview. Editing is a separate full fetch.
export function mountInspectorEditors(inspector, {revision,format,load,changed=()=>{},status=()=>{}}) {
 const controller=new AbortController(),entries=[];
 let destroyed=false;
 function mount(entry,text,full) {
  if(destroyed) return;
  if(!entry.editor) {
   const host=inspector.ownerDocument.createElement('div');
   host.className='representation-editor';
   host.setAttribute('aria-label',entry.source.tagName==='TEXTAREA'?'Subtree source':`${entry.format} representation`);
   entry.source.after(host);
   entry.editor=new SourceEditor(host,null,null,()=>{},()=>{if(entry.full)changed();});
   const tools=inspector.ownerDocument.createElement('div');tools.className='button-row fold-tools';
   for(const [label,action] of [['Collapse all',()=>entry.editor.collapse()],['Expand all',()=>entry.editor.expand()]]){
    const button=inspector.ownerDocument.createElement('button');button.type='button';button.textContent=label;button.onclick=action;tools.append(button);
   }
   host.before(tools);entry.tools=tools;
  }
  entry.editor.setText(text,entry.source.dataset.foldFormat||(entry.format==='native'?(format==='yaml'?'yaml':'json'):entry.format));
  entry.full=full;
  entry.editor.readOnly=!full||entry.source.tagName!=='TEXTAREA';
  entry.source.hidden=true;
  entry.source.dataset.representationFull=String(full);
  if(entry.submit)entry.submit.disabled=!full;
 }
 async function fetchEntry(entry,full=false) {
  if(destroyed||entry.loading||(!full&&entry.editor))return;
  entry.loading=true;entry.note.textContent=full?'Loading complete subtree…':'Loading preview…';
  if(entry.edit)entry.edit.disabled=true;
  try {
   const result=await load(entry.node,entry.format,full,controller.signal);
   if(destroyed)return;
   if(result.revision!==revision)throw new Error('Document changed; select this node again.');
   if(full&&result.truncated)throw new Error('Complete subtree was not returned. Editing remains disabled.');
   mount(entry,result.text,full);
   entry.truncated=result.truncated;
   const editable=entry.source.tagName==='TEXTAREA';
   entry.note.textContent=result.truncated?`Preview only · ${result.totalBytes.toLocaleString()} bytes. Load complete text to ${editable?'edit':'copy'}.`:full?(editable?'Complete subtree loaded for editing.':'Complete representation loaded for copying.'):(editable?'Preview · load complete text to edit.':'Complete representation preview.');
   if(entry.edit)entry.edit.hidden=full;
  }catch(error){if(!destroyed&&error.name!=='AbortError'){entry.note.textContent=error.message;status(error.message,true);}}
  finally{entry.loading=false;if(entry.edit)entry.edit.disabled=false;}
 }
 for(const source of inspector.querySelectorAll('[data-representation-node]')){
  const entry={source,node:source.dataset.representationNode,format:source.dataset.format,full:false};entries.push(entry);
  entry.note=inspector.ownerDocument.createElement('p');entry.note.className='muted representation-status';entry.note.setAttribute('role','status');source.after(entry.note);
  entry.edit=inspector.ownerDocument.createElement('button');entry.edit.type='button';entry.edit.dataset.loadFull='true';entry.edit.textContent=source.tagName==='TEXTAREA'?'Load full text to edit':'Load full representation to copy';entry.edit.onclick=()=>fetchEntry(entry,true);entry.note.after(entry.edit);
  if(source.tagName==='TEXTAREA'){
   entry.submit=source.form?.querySelector('button[type="submit"]');if(entry.submit)entry.submit.disabled=true;
   entry.submitGuard=event=>{
    if(!entry.full||!entry.editor){event.preventDefault();event.stopImmediatePropagation();entry.note.textContent='Load complete text before applying this subtree.';return;}
    source.value=entry.editor.getText();
   };
   source.form?.addEventListener('submit',entry.submitGuard,true);
   if(source.dataset.representationFull==='true'){mount(entry,source.value,true);entry.edit.hidden=true;}
  }
  const details=source.closest('details');
  entry.details=details;entry.toggle=()=>{if(details.open)void fetchEntry(entry);};details?.addEventListener('toggle',entry.toggle);
 }
 const ready=Promise.all(entries.filter(entry=>!entry.details||entry.details.open).map(entry=>fetchEntry(entry)));
 return {ready,
  copyText(source){const entry=entries.find(entry=>entry.source===source);if(!entry?.editor)throw new Error('Open this representation before copying.');if(entry.truncated)throw new Error('This preview is truncated. Load full text to copy the complete subtree.');return entry.editor.getText();},
  destroy(){destroyed=true;controller.abort();for(const entry of entries){entry.editor?.destroy();entry.details?.removeEventListener('toggle',entry.toggle);entry.source.form?.removeEventListener('submit',entry.submitGuard,true);}}
 };
}
