import { EditorState, ChangeSet, Compartment } from '@codemirror/state';
import { EditorView, ViewPlugin, Decoration, lineNumbers, highlightActiveLineGutter, drawSelection, keymap, placeholder } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { foldGutter, foldKeymap, syntaxHighlighting, defaultHighlightStyle, foldAll, unfoldAll } from '@codemirror/language';
import { json } from '@codemirror/lang-json';
import { yaml } from '@codemirror/lang-yaml';
const diffLines = ViewPlugin.fromClass(class {
 constructor(view) { this.decorations=this.build(view); }
 update(update) { if(update.docChanged||update.viewportChanged)this.decorations=this.build(update.view); }
 build(view) {
  const marks=[];
  for(const {from,to} of view.visibleRanges){
   for(let pos=from;pos<=to;){
    const line=view.state.doc.lineAt(pos),first=line.text[0];
    const kind=first==='+'?'added':first==='-'?'removed':first==='@'?'hunk':null;
    if(kind)marks.push(Decoration.line({class:`source-diff-${kind}`}).range(line.from));
    pos=line.to+1;
   }
  }
  return Decoration.set(marks,true);
 }
},{decorations:plugin=>plugin.decorations});
export const normalizedSource = text => text.replace(/\r\n?/g, '\n');
export function lineStarts(text) {
  const starts = [0];
  for (let i = 0; i < text.length; i++) if (text[i] === '\n') starts.push(i + 1);
  return starts;
}
function lineIndex(starts, offset) {
  let low = 0, high = starts.length;
  while (low + 1 < high) {
    const mid = (low + high) >>> 1;
    if (starts[mid] <= offset) low = mid;
    else high = mid;
  }
  return low;
}
export function cursorPosition(text, offset) {
  const starts = lineStarts(text), line = lineIndex(starts, offset);
  return { line: line + 1, column: offset - starts[line] + 1 };
}
export function nodeAt(ranges, start, end) {
  let best = null;
  for (const range of ranges) {
    const contains = range.start <= start && end <= range.end &&
      (start < range.end || range.start === range.end || !range.parent);
    if (contains && (!best || range.end - range.start <= best.end - best.start)) best = range;
  }
  return best;
}
export function revealPath(ranges, id) {
  const byID = ranges instanceof Map ? ranges : new Map(ranges.map(r => [r.id, r]));
  const path = [];
  for (let node = byID.get(id); node?.parent; node = byID.get(node.parent))
    path.push({ id: node.parent, offset: Math.floor(node.index / 20) * 20 });
  return path.reverse();
}

// The CM rope and composed ChangeSet keep ordinary editing proportional to the
// changed region. Serializing the whole document is reserved for explicit export/import.
export class SourceEditor {
 constructor(host, gutter, position, onSelection = () => {}, onChange = () => {}) {
  Object.assign(this,{host,position,onSelection,onChange});
  this.readOnlyConfig = new Compartment();
  this.languageConfig = new Compartment();
  this.changes = ChangeSet.empty(0);
  this.silent = false;
  this.extensions = [placeholder(host.dataset.placeholder||''),lineNumbers(),highlightActiveLineGutter(),drawSelection(),history(),foldGutter(),
   keymap.of([indentWithTab,...defaultKeymap,...historyKeymap,...foldKeymap]),
   syntaxHighlighting(defaultHighlightStyle),
   this.readOnlyConfig.of(EditorState.readOnly.of(false)),this.languageConfig.of([]),
   EditorView.contentAttributes.of({'aria-label':host.getAttribute('aria-label') || 'Document source',spellcheck:'false'}),
   EditorView.updateListener.of(update => {
    if (update.docChanged && !this.silent) {
     this.changes=this.changes.compose(update.changes);
     this.onChange(this.dirty);
    }
    if (update.docChanged || update.selectionSet) this.refresh();
    if (update.selectionSet && !this.silent) {
     clearTimeout(this.timer);
     const range=update.state.selection.main;
     this.timer=setTimeout(()=>this.onSelection(range.from,range.to),100);
    }
   }),
   EditorView.theme({'&':{height:'100%',fontSize:'11px'},'.cm-scroller':{overflow:'auto',fontFamily:'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'},'.cm-content':{padding:'18px 0'},'.cm-gutters':{backgroundColor:'#f2f5ee',color:'#748174'},'.cm-selectionBackground':{backgroundColor:'#bdd7bd !important'},'&.cm-focused .cm-selectionBackground':{backgroundColor:'#bdd7bd'}}),
  ];
  this.view = new EditorView({state:EditorState.create({extensions:this.extensions}),parent:host});
  this.refresh();
 }
 get dirty() { return !this.changes.empty; }
 getText() { return this.view.state.doc.toString(); }
 patches() {
  const patches=[];
  this.changes.iterChanges((start,end,_from,_to,insert)=>patches.push({start,end,text:insert.toString()}));
  return patches;
 }
 setText(text,format) {
  clearTimeout(this.timer);
  this.format=format;
  this.changes=ChangeSet.empty(normalizedSource(text).length);
  this.silent=true;
  this.view.setState(EditorState.create({doc:text,extensions:this.extensions}));
  this.setFormat(format);
  this.silent=false;
  this.refresh();
 }
 setFormat(format) {
  this.format=format;
  this.view.dispatch({effects:this.languageConfig.reconfigure(format==='json'?json():format==='yaml'?yaml():format==='diff'?diffLines:[])});
 }
 markClean() { this.changes=ChangeSet.empty(this.view.state.doc.length); }
 snapshot() { return {state:this.view.state,changes:this.changes,format:this.format}; }
 restore(snapshot) {
  clearTimeout(this.timer);
  this.silent=true;
  this.view.setState(snapshot.state);
  this.changes=snapshot.changes;
  this.format=snapshot.format;
  this.silent=false;
  this.refresh();
 }
 set readOnly(value) { this.view.dispatch({effects:this.readOnlyConfig.reconfigure([EditorState.readOnly.of(value),EditorView.editable.of(!value)])}); }
 refresh() {
  const caret=this.view.state.selection.main.head,line=this.view.state.doc.lineAt(caret);
  if (this.position) this.position.textContent=`Ln ${line.number}, Col ${caret-line.from+1}`;
 }
 highlight(range) {
  if (!range) return;
  clearTimeout(this.timer);
  this.silent=true;
  this.view.dispatch({selection:{anchor:range.start,head:range.end},effects:EditorView.scrollIntoView(range.start,{y:'center',x:'center'})});
  this.silent=false;
  this.refresh();
 }
 collapse() { foldAll(this.view); }
 expand() { unfoldAll(this.view); }
 destroy() { clearTimeout(this.timer);this.view.destroy(); }
}
