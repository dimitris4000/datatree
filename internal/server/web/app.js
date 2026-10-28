import { unifiedDiff } from "./source-diff.js";
import { requestJSON } from "./request.js";
import { mountDiffPanel } from "./diff.js";
import { mountFileBrowser } from "./file-browser.js";
import { mountConversionDialog, downloadConvertedCopy } from "./conversion.js";
import { mountValidationPanel, canValidateDocument } from "./validation.js";
import { mountFindPanel } from "./find.js";
import { SourceEditor } from "./source-editor.js";
import { mountInspectorEditors } from "./inspector-editor.js";
import { TreeCanvas, expandLevels, collapseAll, diffBadge, validationBadge } from "./tree.js";
import { NodeContextMenu, menuTarget, runNodeAction } from "./context-menu.js";
const $ = (id) => document.getElementById(id);
const source = $("source"),
  inspector = $("inspector");
let state = { revision: 0 },
  original = null,
  workingDraft = null,
  cleanDraft = null,
  sourceDirty = false,
  inspectorDirty = false,
  showOriginal = false,
  showDiff = false,
  busy = false,
  selection = null;
let nodeIndices = new Map(),
  records = new Map(),
  expanded = new Set();
let refreshingCanonical = false;
let sourceByID = new Map(), sourceSync = 0, inspectorEditors = null, operationAbort = null, inspectorAbort = null;
function status(message, error = false) {
  $("status").textContent = message;
  $("status").classList.toggle("error", error);
}
function canLeaveInspector() {
  if (!inspectorDirty) return true;
  if (!confirm("Discard the unapplied subtree edit?")) return false;
  inspectorDirty = false;
  return true;
}
function ready() {
  if (busy) {
    status("Wait for the current operation.");
    return false;
  }
  if (sourceDirty) {
    status("Apply or discard the source draft before changing the tree.", true);
    return false;
  }
  return true;
}
async function api(path, data, signal = operationAbort?.signal) {
  const options =
    data === undefined
      ? {}
      : {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ revision: state.revision, ...data }),
        };
  const result=await requestJSON(path,{...options,signal},{progress:stage=>status(`Processing document · ${stage}… Cancel is available.`)});
  if(signal?.aborted&&signal===operationAbort?.signal)operationAbort=new AbortController();
  return result;
}
function setBusy(value) {
  if (value) { nodeMenu.close(false); operationAbort ||= new AbortController(); }
  else operationAbort = null;
  $("cancel-operation").hidden = !value || refreshingCanonical;
  $("cancel-operation").disabled = refreshingCanonical;
  $("operation-progress").hidden = !value;
  busy = value;
  $("expand-levels").disabled = value;
  $("expand-selected").disabled = value;
  sourceEditor.readOnly = value || showOriginal;
  document
    .querySelectorAll(".toolbar-actions button,#apply,#example")
    .forEach((b) => (b.disabled = value));
  if (!value) {
    $("undo").disabled = !state.canUndo;
    $("redo").disabled = !state.canRedo;
    $("save").disabled = $("save-as").disabled = !state.root;
    $("apply").disabled = showOriginal;
    $("discard").disabled = showOriginal;
    $("source-diff-tab").disabled = !state.root;
    $("original-tab").disabled = !state.root;
  }
}
// After a mutation returns metadata, cancellation can no longer roll it back.
// Finish the canonical view refresh independently of the mutation's old signal.
function beginCanonicalRefresh() {
  operationAbort=null;
  refreshingCanonical=true;
  $("cancel-operation").hidden=true;
  $("cancel-operation").disabled=true;
  status('Refreshing the applied document…');
}
async function withProgress(action) {
  const started=!busy;
  if(started){setBusy(true);status('Processing document… Cancel is available.');}
  try{return await action();}finally{if(started)setBusy(false);}
}
function ingest(n, nested = false) {
  // A depth-2 response carries only the first child page. Keep independently
  // loaded child pages until that child is explicitly refreshed or paged.
  if(nested&&records.has(n.id))return;
  records.set(n.id, n);
  for (const [index,c] of (n.children || []).entries()) {
    nodeIndices.set(c.id,{id:c.id,parent:n.id,index:(n.offset||0)+index});
    if (c.children) ingest(c, true);
  }
}
async function page(id, offset = 0) {
  const revision = state.revision;
  const n = await api(
    `/api/tree?node=${encodeURIComponent(id)}&offset=${offset}&limit=20&revision=${state.revision}`,
  );
  if (revision !== state.revision) throw new Error("Selection changed while loading the tree.");
  ingest(n);
  return n;
}
function updateControls() {
  $("filename").textContent = state.filename || "Untitled document";
  $("filename").title = state.filename || "";
  const git = $("git-context");
  git.hidden = !state.git;
  git.textContent = state.git
    ? `${state.git.branch || "Detached HEAD"} · ${state.git.commit.slice(0, 8)}` : "";
  git.title = state.git
    ? `${state.git.branch || "Detached HEAD"} · Git HEAD: ${state.git.commit} (working edits stay in memory)` : "";
  $("format-badge").textContent = (
    state.format || $("input-format").value
  ).toUpperCase();
  if (state.format) $("input-format").value = state.format;
  $("input-format").disabled = !!state.format;
  $("node-count").textContent = state.nodes
    ? `${state.nodes.toLocaleString()} nodes`
    : "";
  $("revision").textContent = state.root
    ? `Revision ${state.revision} · Changes in memory`
    : "Ready when you are.";
  $("undo").disabled = busy || !state.canUndo;
  $("redo").disabled = busy || !state.canRedo;
  $("save").disabled = $("save-as").disabled = busy || !state.root;
  $("empty").hidden = !!state.root;
  $("draft-state").textContent = sourceDirty
    ? "Unapplied changes"
    : "No changes";
  $("apply").disabled = busy || showOriginal;
  $("discard").disabled = busy || showOriginal;
  $("source-diff-tab").disabled = busy || !state.root;
  $("original-tab").disabled = busy || !state.root;
  $("working-tab").classList.toggle("active", !showOriginal);
  $("original-tab").classList.toggle("active", showOriginal && !showDiff);
  $("working-tab").setAttribute("aria-pressed", String(!showOriginal));
  $("original-tab").setAttribute("aria-pressed", String(showOriginal && !showDiff));
  $("source-diff-tab").classList.toggle("active", showDiff);
  $("source-diff-tab").setAttribute("aria-pressed", String(showDiff));
  sourceEditor.readOnly = showOriginal || busy;
  sourceEditor.refresh();
}
// Record only the loaded tree's index paths. Node IDs change with revisions.
function savedTreePaths() {
  const path=id=>{
    if(!id)return null;
    const indices=[],seen=new Set();
    while(id!==state.root){
      if(seen.has(id))return null;seen.add(id);
      const node=nodeIndices.get(id);if(!node)return null;
      indices.push(node.index);id=node.parent;
    }
    return indices.reverse();
  };
  return {expanded:[...expanded].map(path).filter(Boolean),selected:path(selection)};
}
async function restoreTreePath(path) {
  let id=state.root;
  for(const index of path){
    const offset=Math.floor(index/20)*20;
    let parent=records.get(id);
    if(!parent?.children||(parent.offset||0)!==offset)parent=await page(id,offset);
    const child=parent.children[index-offset];if(!child)return null;
    expanded.add(id);id=child.id;
  }
  return id;
}
async function installStableDraft(next) {
  const depth=id=>{
    const seen=new Set();let count=0;
    for(let node=nodeIndices.get(id);node&&!seen.has(node.id);node=nodeIndices.get(node.parent)){
      seen.add(node.id);count++;
    }
    return count;
  };
  const changedPages=[...new Set(next.changedNodes)].filter(id=>records.has(id))
    .map(id=>({id,offset:records.get(id).offset||0,depth:depth(id)}))
    .sort((a,b)=>a.depth-b.depth);
  const refreshed=[];
  // Snapshot offsets first and refresh root-to-leaf. Root summaries must not
  // overwrite a previously selected page in a descendant or unrelated branch.
  for(const {id,offset} of changedPages){
    refreshed.push(await api(`/api/tree?node=${encodeURIComponent(id)}&offset=${offset}&limit=20&revision=${next.revision}`,undefined,null));
  }
  inspectorAbort?.abort();
  findPanel.controller.clear();
  diffPanel.controller.invalidateDocument();
  inspectorEditors?.destroy();inspectorEditors=null;
  inspector.replaceChildren();
  sourceSync++;
  state=next;
  for(const location of sourceByID.values())location.revision=next.revision;
  validationPanel.controller.install(state.schema);
  sourceDirty=false;inspectorDirty=false;showOriginal=false;showDiff=false;
  sourceEditor.markClean();
  cleanDraft=sourceEditor.snapshot();workingDraft=cleanDraft;
  for(const record of refreshed)ingest(record);
  tree.root=records.get(next.root);
  tree.selected=selection;
  updateControls();
  tree.relayout();
  if(selection)await select(selection,false,false);
  if(diffPanel.controller.visible)await diffPanel.controller.refresh();
}
async function install(next, { fit = false, imported = false, appliedDraft = false, committed = false } = {}) {
  const wasBusy=busy;
  if(!wasBusy)setBusy(true);
  beginCanonicalRefresh();
  try {
    nodeMenu.close(false);
    if(appliedDraft&&next.revision===state.revision+1&&next.stableIDs===true&&Array.isArray(next.changedNodes)&&next.root===state.root){
      await installStableDraft(next);return;
    }
    const saved=imported?null:savedTreePaths();
    // Complete the two initial reads before replacing the current, possibly dirty UI.
    let sourceText=null,root=null;
    if(next.root){
      if(!appliedDraft){
        const response=await fetch(`/api/source?revision=${next.revision}`,{signal:null});
        if(!response.ok)throw new Error(`Source request failed (${response.status})`);
        sourceText=await response.text();
      }
      root=await api(`/api/tree?node=${encodeURIComponent(next.root)}&offset=0&limit=20&revision=${next.revision}`,undefined,null);
    }
    inspectorAbort?.abort();
    findPanel.controller.clear();
    diffPanel.controller.invalidateDocument(imported);
    inspectorEditors?.destroy();inspectorEditors=null;
    inspector.replaceChildren();
    sourceSync++;
    sourceByID = new Map();
    state = next;
    validationPanel.controller.install(state.schema);
    sourceDirty = false;
    inspectorDirty = false;
    showOriginal=false;showDiff=false;
    if(imported)original=null;
    if(appliedDraft){sourceEditor.markClean();sourceEditor.setFormat(state.format);}
    else sourceEditor.setText(sourceText||'',state.format);
    cleanDraft=sourceEditor.snapshot();workingDraft=cleanDraft;
    selection = null;
    $("selection-expansion").hidden = true;
    records = new Map();nodeIndices = new Map();expanded = new Set();
    updateControls();
    if (state.root) {
      ingest(root);
      expanded.add(root.id);
      if(saved){
        for(const path of saved.expanded){const id=await restoreTreePath(path);if(id){if(!records.has(id))await page(id);expanded.add(id);}}
        if(saved.selected)selection=await restoreTreePath(saved.selected);
      }
      tree.install(root, records, expanded);
      tree.selected=selection;
      if (fit) tree.fit();
      if(selection)await select(selection,false);
      if (diffPanel.controller.visible) await diffPanel.controller.refresh();
    }
  } catch(error) {
    throw new Error(committed?`Changes committed at revision ${next.revision}, but refreshing the view failed: ${error.message}`:`Document revision ${next.revision} could not be loaded: ${error.message}`,{cause:error});
  } finally {
    refreshingCanonical=false;
    if(!wasBusy)setBusy(false);
  }
}
async function operation(path, data, options = {}) {
  if (!ready() || !canLeaveInspector()) return;
  setBusy(true);
  status("Processing document… Cancel is available.");
  try {
    await install(await api(path, data), {...options,committed:true});
    status(
      options.imported
        ? (state.schema ? `File opened. Attached ${state.schema.filename}; press Validate to check it.` : "File opened. Click a field or card to inspect it.")
        : "Changes applied. Use Save to write changes to disk.",
    );
  } catch (e) {
    status(e.name === "AbortError" ? "Operation cancelled." : e.message, true);
  } finally {
    setBusy(false);
  }
}
async function select(id, guard = true, syncSource = true) {
  if (guard && !canLeaveInspector()) return;
  if (syncSource) sourceSync++;
  const revision=state.revision;
  inspectorAbort?.abort();inspectorAbort=new AbortController();
  const signal=inspectorAbort.signal;
  selection = id;
  tree.selected = id;
  document.querySelectorAll('#node-list button[data-node-id]').forEach(button => {
    button.setAttribute('aria-pressed', String(button.dataset.nodeId === id));
  });
  const node = records.get(id) || tree.graph.cards
    .flatMap((card) => [card, ...card.rows]).find((node) => node.id === id);
  $("selection-expansion").hidden = !node ||
    !["object", "array", "stream"].includes(node.kind);
  tree.draw();
  try {
    inspectorEditors?.destroy(); inspectorEditors=null;
    if(syncSource&&sourceSynchronized()){
      const location=await nodeLocation(id);
      if(signal.aborted||revision!==state.revision||selection!==id)return;
      sourceEditor.highlight(location.range);
    }
    const response=await fetch(`/inspector?node=${encodeURIComponent(id)}&revision=${revision}`,{signal});
    if(!response.ok)throw new Error(`Inspector request failed (${response.status})`);
    const html=await response.text();
    if(signal.aborted||revision!==state.revision||selection!==id)return;
    inspector.innerHTML=html;
    globalThis.htmx?.process(inspector);
    mountSelectedInspector();
    inspectorDirty = false;
    validationPanel.selectionChanged();
  } catch (e) {
    if(!signal.aborted&&revision===state.revision)status(e.message, true);
  }
}

const nodeMenu = new NodeContextMenu($("node-context-menu"), async (op, target) => {
  if (target.revision !== state.revision) return;
  await runNodeAction(op, target, state.format, {
    ready: () => ready() && canLeaveInspector(),
    prompt: (...args) => window.prompt(...args),
    confirm: (message) => window.confirm(message),
    mutate: (edit) => operation("/api/edit", edit),
  });
});
function openNodeMenu(id, x, y, returnFocus = $("canvas-wrap")) {
  nodeMenu.close(false);
  if (!ready() || !canLeaveInspector()) return;
  const target = menuTarget(tree.graph, id);
  if (!target) return;
  void select(id, false);
  nodeMenu.open({ ...target, revision: state.revision }, state.format, x, y, returnFocus);
}
document.addEventListener("pointerdown", (event) => {
  if (!nodeMenu.element.contains(event.target)) nodeMenu.close(false);
});
document.addEventListener("scroll", () => nodeMenu.close(), true);
window.addEventListener("resize", () => nodeMenu.close());
window.addEventListener("blur", () => nodeMenu.close(false));
$("canvas").addEventListener("wheel", () => nodeMenu.close(), { passive: true });

const tree = new TreeCanvas($("canvas"), {
  zoom: (n) => ($("zoom-label").textContent = `${n}%`),
  layout: (graph) => {
    const list = $("node-list");
    list.replaceChildren();
    for (const c of graph.cards) {
      for (const n of [c, ...c.rows]) {
        const b = document.createElement("button");
        b.dataset.nodeId = n.id;
        b.setAttribute('aria-pressed', String(n.id === selection));
        const badge = diffBadge(tree.diffMarks?.[n.id]);
        const validation = validationBadge(tree.validationMarks?.[n.id]);
        b.textContent = `${"· ".repeat(c.depth)}${validation ? validation.label + " · " : ""}${badge ? badge.label + " · " : ""}${n.label} (${n.kind})`;
        b.addEventListener("click", async () => {
          await select(n.id);
          if (selection === n.id) tree.focus(n.id);
        });
        b.addEventListener("dblclick", () => {
          if (["object", "array", "stream"].includes(n.kind))
            void action({ type: "toggle", id: n.id });
        });
        b.addEventListener("contextmenu", (event) => {
          event.preventDefault();
          openNodeMenu(n.id, event.clientX, event.clientY, b);
        });
        b.addEventListener("keydown", (event) => {
          if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
            event.preventDefault();
            const box = b.getBoundingClientRect();
            openNodeMenu(n.id, box.left, box.bottom, b);
          }
        });
        list.append(b);
      }
      if (["object", "array", "stream"].includes(c.kind)) {
        const b = document.createElement("button");
        b.textContent = `${c.open ? "Collapse" : "Expand"} ${c.label}`;
        b.addEventListener("click", () => action({ type: "toggle", id: c.id }));
        list.append(b);
      }
    }
    if (graph.truncated)
      status(
        "Visible tree limited to 1,000 cards. Collapse branches to explore others.",
      );
  },
  action,
  context: openNodeMenu,
});
function sourceSynchronized() {
  return !showDiff && !!state.root && (showOriginal ? state.originalMatches === true : !sourceDirty);
}
async function nodeLocation(id) {
  if(sourceByID.has(id))return sourceByID.get(id);
  const result=await api(`/api/node-location?revision=${state.revision}&node=${encodeURIComponent(id)}`);
  if(result.revision===state.revision)sourceByID.set(id,result);
  return result;
}
async function revealLocation(location,current) {
  if(!location.range||!current())return false;
  for(const step of location.ancestors||[]){
    const record=records.get(step.id);
    if(!record?.children||(record.offset||0)!==step.offset)await page(step.id,step.offset);
    if(!current())return false;
    expanded.add(step.id);
  }
  tree.relayout(new Set([...(location.ancestors||[]).map(step=>step.id),location.range.id]));
  return current();
}
const sourceEditor = new SourceEditor(source, null, $("source-position"), async (start, end) => {
  if (busy || !sourceSynchronized()) return;
  const ticket=++sourceSync,revision=state.revision;
  const current=()=>ticket===sourceSync&&revision===state.revision&&sourceSynchronized();
  try{
    const target=await api(`/api/locate?revision=${revision}&start=${start}&end=${end}`);
    if(!current()||!target.range||target.range.id===selection||!canLeaveInspector())return;
    sourceByID.set(target.range.id,target);
    if(!await revealLocation(target,current))return;
    await select(target.range.id,false,false);
    if(current())tree.focus(target.range.id);
  }catch(error){if(current())status(error.message,true);}
}, dirty => {
  sourceSync++;nodeMenu.close(false);
  if(showOriginal)return;
  sourceDirty=dirty;
  $("draft-state").textContent=sourceDirty?"Unapplied changes":"No changes";
});
// Indexed lookup reveals only the ancestor pages of a match.
const findPanel = mountFindPanel(document, {
  invalidate: () => { sourceSync++; },
  revision: () => state.revision,
  load: async (options, revision, signal) => {
    if (!state.root) throw new Error("Open a file first.");
    const query = new URLSearchParams({ ...options, revision });
    return api(`/api/find?${query}`, undefined, signal);
  },
  navigate: navigateNode,
});
const diffPanel = mountDiffPanel(document, {
  revision: () => state.revision,
  invalidate: () => { sourceSync++; },
  load: (baseline, revision) => withProgress(()=>api(`/api/diff?${new URLSearchParams({ baseline, revision })}`)),
  upload: (payload, revision) => withProgress(()=>api("/api/diff-baseline", { ...payload, revision })),
  pickFile: () => fileBrowser.pick('comparison'),
  navigate: navigateNode,
  revert: async (payload, revision) => {
    if (!ready() || !canLeaveInspector()) return false;
    if (revision !== state.revision) throw new Error("Document changed; refresh Changes before reverting.");
    setBusy(true);
    try {
      const next = await api("/api/diff-revert", { ...payload, revision });
      await install(next,{committed:true});
      status("Change reverted. Undo restores it; use Save to write changes to disk.");
      return true;
    } finally {
      setBusy(false);
    }
  },
  highlight: (marks) => tree.setDiffMarks(marks),
});
const validationPanel = mountValidationPanel(document, {
  revision: () => state.revision,
  available: () => canValidateDocument(state),
  attach: (payload, revision) => api('/api/schema', { ...payload, revision }),
  pickFile: () => fileBrowser.pick('schema'),
  clear: revision => api('/api/schema-clear', { revision }),
  validate: (revision, schemaToken) => withProgress(()=>api('/api/validate', { revision, schemaToken })),
  navigate: navigateNode,
  selected: () => selection,
  highlight: marks => tree.setValidationMarks(marks),
});
async function navigateNode(id) {
  if (!ready() || !canLeaveInspector()) return false;
  const ticket = ++sourceSync, revision = state.revision;
  const current = () => ticket === sourceSync && revision === state.revision && !sourceDirty;
  setBusy(true);
  try {
    const location=await nodeLocation(id);
    if(!await revealLocation(location,current))return false;
    if(sourceSynchronized())sourceEditor.highlight(location.range);
    await select(id, false, false);
    if (!current()) return false;
    tree.focus(id);
    return true;
  } finally {
    setBusy(false);
  }
}
async function action(hit) {
  nodeMenu.close(false);
  try {
    if (hit.type === "select") {
      await select(hit.id);
      return;
    }
    if (busy) return;
    if (hit.type === "expand") {
      if (!ready()) return;
      setBusy(true);
      try {
        const result = await expandLevels(hit.id, hit.levels, records, expanded, page);
        if (result.truncated) status("Expansion limited to 1,000 collections. Select a branch to explore further.");
      } finally {
        tree.relayout();
        setBusy(false);
      }
    }
    if (hit.type === "toggle") {
      if (expanded.has(hit.id)) expanded.delete(hit.id);
      else {
        await page(hit.id);
        expanded.add(hit.id);
      }
      tree.relayout();
    }
    if (hit.type === "page") {
      await page(hit.id, hit.offset);
      tree.relayout();
    }
  } catch (e) {
    status(e.name === "AbortError" ? "Operation cancelled." : e.message, true);
  }
}
function mountSelectedInspector() {
  inspectorEditors?.destroy();
  inspectorEditors=mountInspectorEditors(inspector,{
    revision:state.revision,format:state.format,
    load:(node,format,full,signal)=>api(`/api/representation?${new URLSearchParams({revision:state.revision,node,format,full})}`,undefined,signal),
    changed:()=>{inspectorDirty=true;},status,
  });
}
document.body.addEventListener("htmx:afterSwap", (event) => {
  if (event.detail.target === inspector) mountSelectedInspector();
});
inspector.addEventListener("input", () => {
  inspectorDirty = true;
});
$("apply").addEventListener("click", async () => {
  if (busy || showOriginal || !canLeaveInspector()) return;
  setBusy(true);
  try {
    const imported = !state.root;
    status("Validating source… Cancel is available.");
    const next = imported ? await api("/api/import", {text:sourceEditor.getText(),format:$("input-format").value}) :
      await api("/api/patch", {patches:sourceEditor.patches()});
    await install(next, { fit: imported, imported, appliedDraft:true, committed:true });
    status("Source applied. The tree shows the validated document.");
  } catch (e) {
    status(e.name === "AbortError" ? "Operation cancelled." : e.message, true);
  } finally {
    setBusy(false);
  }
});
$("discard").addEventListener("click", () => {
  if(cleanDraft)sourceEditor.restore(cleanDraft);else sourceEditor.setText("",state.format);
  workingDraft=sourceEditor.snapshot();
  sourceDirty = false;
  updateControls();
  status("Source draft discarded.");
});
async function sourceTab(mode) {
  const isOriginal=mode!=="working", isDiff=mode==="diff";
  if(busy||(showOriginal===isOriginal&&showDiff===isDiff))return;
  sourceSync++;
  if(!showOriginal)workingDraft=sourceEditor.snapshot();
  if(isOriginal){
    setBusy(true);status("Loading original source…");
    try{
      if(original===null){
        const response=await fetch(`/api/source?revision=${state.revision}&original=true`,{signal:operationAbort.signal});
        if(!response.ok)throw new Error(`Original source request failed (${response.status})`);
        original=await response.text();
      }
      sourceEditor.setText(isDiff ? unifiedDiff(original.replace(/\r\n?/g,"\n"),workingDraft.state.doc.toString()) : original,isDiff ? "diff" : state.format);showOriginal=true;showDiff=isDiff;
      status(isDiff ? "Original → Working diff, including unapplied source changes." : "Original source.");
    }catch(error){status(error.message,true);}
    finally{setBusy(false);}
  }else{sourceEditor.restore(workingDraft);showOriginal=false;showDiff=false;}
  updateControls();
}
$("working-tab").addEventListener("click", () => void sourceTab("working"));
$("original-tab").addEventListener("click", () => void sourceTab("original"));
$("source-diff-tab").addEventListener("click", () => void sourceTab("diff"));
$("cancel-operation").addEventListener("click",()=>{operationAbort?.abort();$("cancel-operation").disabled=true;status("Cancelling operation…");});
const fileBrowser = mountFileBrowser(document, {
  revision: () => state.revision,
  browse: payload => api('/api/files', payload),
  ready: () => ready() && canLeaveInspector(),
  canPick: () => !busy,
  open: async payload => {
    setBusy(true);
    try {
      await install(await api('/api/open', payload), { fit: true, imported: true, committed:true });
      status(state.schema ? `File opened. Attached ${state.schema.filename}; press Validate to check it.` : 'File opened.');
      return true;
    } finally { setBusy(false); }
  },
});
$("open-file").addEventListener("click", () => {
  if (!ready() || !canLeaveInspector()) return;
  $("file-open-error").hidden = true;
  $("open-document").showModal();
  void fileBrowser.show();
});
$("close-open-document").addEventListener("click", () => $("open-document").close());
$("example").addEventListener("click", async () => {
  await operation(
    "/api/import",
    {
      text: JSON.stringify(JSON.parse($("example-data").textContent), null, 2),
      format: "json",
      filename: "Super hero squad · example",
    },
    { fit: true, imported: true },
  );
});
$("undo").addEventListener("click", () =>
  operation("/api/history", { direction: "undo" }),
);
$("redo").addEventListener("click", () =>
  operation("/api/history", { direction: "redo" }),
);
async function saveDocument(as = false) {
  if (!ready() || !canLeaveInspector()) return;
  if (!state.root) { status("Open a file first.", true); return; }
  as = as || !state.canSaveLocal;
  let path;
  if (as) {
    const choice = await fileBrowser.pick('save');
    if (!choice || !ready()) return;
    path = choice.path;
  }
  setBusy(true);
  try {
    const next = await api(as ? '/api/save-as' : '/api/save', path ? { path } : {});
    state = next;
    updateControls();
    status(`Saved ${next.savedPath}.`);
  } catch (error) {
    status(error.name === 'AbortError' ? 'Save cancelled; check the destination before retrying.' : error.message, true);
  } finally { setBusy(false); }
}
$("save").addEventListener("click", () => void saveDocument());
$("save-as").addEventListener("click", () => void saveDocument(true));
const conversionDialog = mountConversionDialog(document, {
  revision: () => state.revision,
  preview: format => withProgress(()=>api('/api/preview', { format })),
  convert: payload => api('/api/convert', payload),
  install:next=>install(next,{committed:true}),
  getSource:async revision=>{
    const response=await fetch(`/api/source?revision=${revision}`,{signal:operationAbort?.signal});
    if(!response.ok)throw new Error(`Download source request failed (${response.status})`);
    return response.text();
  },
  download: (text, filename) => downloadConvertedCopy(document, text, filename),
  cancelOperation:()=>operationAbort?.abort(),
  busy: setBusy,
  status,
  ready: () => ready() && canLeaveInspector(),
});
$("convert").addEventListener("click", async () => {
  if (!ready() || !canLeaveInspector()) return;
  try {
    await conversionDialog.open($("conversion-format").value);
  } catch (e) {
    status(e.name === "AbortError" ? "Operation cancelled." : e.message, true);
  }
});
$("zoom-in").addEventListener("click", () => tree.zoom(1.2));
$("zoom-out").addEventListener("click", () => tree.zoom(1 / 1.2));
$("fit").addEventListener("click", () => tree.fit());
$("expand-levels").addEventListener("input", () => {
  const input = $("expand-levels");
  $("expand-selected").textContent = input.validity.valid
    ? `Expand ${input.value}` : "Expand X";
});
$("expand-selected").addEventListener("click", () => {
  const input = $("expand-levels");
  if (selection && input.reportValidity())
    void action({ type: "expand", id: selection, levels: Number(input.value) });
});
$("collapse").addEventListener("click", () => {
  if (busy) return;
  collapseAll(selection, [...nodeIndices.values()], expanded);
  tree.relayout();
  tree.fit();
});
document.body.addEventListener("click", async (e) => {
  const b = e.target.closest("[data-copy],[data-reference]");
  if (!b) return;
  if (b.dataset.copy) {
    try {
      const n = $(b.dataset.copy);
      await navigator.clipboard.writeText(n.dataset.representationNode ? inspectorEditors.copyText(n) : n.value ?? n.textContent);
      status("Copied to clipboard.");
    } catch {
      status(
        "Clipboard access is unavailable. Select and copy the text manually.",
        true,
      );
    }
  }
  if (b.dataset.reference) {
    await select(b.dataset.reference);
    tree.focus(b.dataset.reference);
  }
});
function inspectorError(form,message) {
  let error=form.querySelector('[data-inspector-error]');
  if(!error){
    error=document.createElement('p');error.dataset.inspectorError='true';
    error.className='error';error.setAttribute('role','alert');form.prepend(error);
  }
  error.textContent=message;
  status(message,true);
}
function inspectorAction(form,config={}) {
  const submitter=config.triggeringEvent?.submitter;
  const parameters=config.parameters??Object.fromEntries(new form.ownerDocument.defaultView.FormData(form,submitter));
  const value=name=>{const entry=parameters[name];return String((Array.isArray(entry)?entry.at(-1):entry)??'');};
  const revision=Number(value('revision'));
  if(!value('revision')||!Number.isSafeInteger(revision)||revision!==state.revision)throw new Error('Selection is stale. Select the node again.');
  const action={revision,op:value('op'),node:value('node'),key:value('key'),text:value('text'),format:value('format'),confirm:value('confirm')==='true',structured:value('structured')==='true'};
  if(!action.op||!action.node)throw new Error('Choose a node and an edit action.');
  const placeholder=form.querySelector('[data-representation-node][data-format="native"]');
  if(placeholder){
    if(placeholder.dataset.representationFull!=='true')throw new Error('Load complete text before applying this subtree.');
    action.text=inspectorEditors.copyText(placeholder);
    placeholder.value=action.text;
  }
  if(action.structured){
    const kind=value('kind'),raw=value('value');
    if(kind==='string')action.text=JSON.stringify(raw);
    else if(kind==='null')action.text='null';
    else{
      let parsed;
      try{parsed=JSON.parse(raw);}catch{throw new Error('Value does not match the selected type.');}
      if(!['number','boolean'].includes(kind)||typeof parsed!==kind)throw new Error('Value does not match the selected type.');
      // Validate the kind without reserializing numbers through JavaScript floats.
      action.text=raw;
    }
    action.format='json';
  }
  return action;
}
async function submitInspector(form,config) {
  if(!ready())return;
  try{
    const action=inspectorAction(form,config);
    if(action.op==='delete'&&!confirm('Delete this node and its subtree? You can undo this change.'))return;
    const error=form.querySelector('[data-inspector-error]');if(error)error.textContent='';
    setBusy(true);status('Validating Inspector edit… Cancel is available.');
    const next=await api('/api/edit',action);
    await install(next,{committed:true});
    status('Tree edit applied. Use Save to write changes to disk.');
  }catch(error){
    inspectorDirty=true;
    inspectorError(form,error.name==='AbortError'?'Inspector edit cancelled. Unapplied edit retained.':error.message);
  }finally{setBusy(false);}
}
document.body.addEventListener('htmx:confirm',event=>{
  const form=event.detail.elt?.closest?.('form');
  if(!form?.closest('#inspector'))return;
  if(!ready()){event.preventDefault();return;}
  const placeholder=form.querySelector('[data-representation-node][data-format="native"]');
  if(placeholder&&placeholder.dataset.representationFull!=='true'){
    event.preventDefault();inspectorError(form,'Load complete text before applying this subtree.');
  }
});
document.body.addEventListener('htmx:beforeRequest',event=>{
  const form=event.detail.elt?.closest?.('form');
  if(!form?.closest('#inspector'))return;
  // All Inspector writes use the same tracked job protocol as other mutations.
  // Stop htmx before its XHR starts; its collected parameters include submitter op.
  event.preventDefault();
  void submitInspector(form,event.detail.requestConfig);
});
for (const [id, property, sign, min, max] of [
  ["resize-source", "--source-width", 1, 180, 550],
  ["resize-inspector", "--inspector-width", -1, 220, 550],
]) {
  const handle = $(id);
  handle.addEventListener("pointerdown", (e) => {
    const start = e.clientX;
    const initial =
      id === "resize-source"
        ? $("source-pane").offsetWidth
        : $("inspector-pane").offsetWidth;
    handle.setPointerCapture(e.pointerId);
    const move = (ev) =>
      document.documentElement.style.setProperty(
        property,
        Math.min(max, Math.max(min, initial + (ev.clientX - start) * sign)) +
          "px",
      );
    const up = () => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", up);
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", up);
  });
  handle.addEventListener("keydown", (e) => {
    if (!["ArrowLeft", "ArrowRight"].includes(e.key)) return;
    e.preventDefault();
    const pane =
      id === "resize-source" ? $("source-pane") : $("inspector-pane");
    document.documentElement.style.setProperty(
      property,
      Math.min(
        max,
        Math.max(
          min,
          pane.offsetWidth + (e.key === "ArrowRight" ? 10 : -10) * sign,
        ),
      ) + "px",
    );
  });
}
window.addEventListener("beforeunload", (e) => {
  if (sourceDirty || inspectorDirty) {
    e.preventDefault();
    e.returnValue = "";
  }
});
try {
  const initial = await api("/api/state");
  if (initial.root) await install(initial, { fit: true, imported: true });
  else updateControls();
} catch (e) {
  status(e.message, true);
}
