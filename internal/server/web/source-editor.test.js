import test from 'node:test';
import assert from 'node:assert/strict';
import * as editor from './source-editor.js';

test('source selection resolves the smallest enclosing node including keys and empty cells', () => {
 const ranges = [{id:'1',start:0,end:40},{id:'2',parent:'1',index:0,start:2,end:30},{id:'3',parent:'2',index:0,start:5,end:12},{id:'4',parent:'2',index:1,start:16,end:16}];
 assert.equal(editor.nodeAt(ranges,6,6)?.id,'3');
 assert.equal(editor.nodeAt(ranges,6,20)?.id,'2');
 assert.equal(editor.nodeAt(ranges,16,16)?.id,'4');
 assert.equal(editor.nodeAt(ranges,40,40)?.id,'1');
 assert.equal(editor.nodeAt(ranges,41,41),null);
});

test('reveal path calculates pages for unloaded ancestors and preserves the selected leaf', () => {
 const ranges=[{id:'1',index:0},{id:'2',parent:'1',index:47},{id:'3',parent:'2',index:23}];
 assert.deepEqual(editor.revealPath(ranges,'3'),[{id:'1',offset:40},{id:'2',offset:20}]);
});

test('line coordinates include trailing blank lines, Unicode and CRLF normalization', () => {
 assert.deepEqual(editor.lineStarts('a\n😀\n'),[0,2,5]);
 assert.deepEqual(editor.cursorPosition('a\n😀\n',4),{line:2,column:3});
 assert.equal(editor.normalizedSource('a\r\nb\rc'),'a\nb\nc');
});

import { editorDOM } from './editor-dom-test.js';

test('CodeMirror keeps viewport DOM bounded and incremental edits produce base UTF16 patches', () => {
 const {dom,host,position}=editorDOM();
 const changes=[];
 const view=new editor.SourceEditor(host,null,position,()=>{},()=>changes.push(true));
 const text='😀 first\n'+ 'row\n'.repeat(100000);
 view.setText(text,'json');
 assert.ok(host.querySelectorAll('.cm-line').length<200,'only viewport lines rendered');
 assert.equal(host.querySelector('textarea'),null,'no whole-document textarea or mirror');
 view.view.dispatch({changes:{from:3,to:8,insert:'next'}});
 view.view.dispatch({changes:{from:3,to:7,insert:'changed'}});
 assert.deepEqual(view.patches(),[{start:3,end:8,text:'changed'}]);
 assert.equal(changes.length,2);
 assert.equal(view.dirty,true);
 view.destroy();dom.window.close();
});

test('selection highlight does not steal focus or echo and readOnly is enforced by editor state', async () => {
 const {dom,host,position}=editorDOM();
 const selections=[];
 const view=new editor.SourceEditor(host,null,position,(...range)=>selections.push(range));
 view.setText('first\n😀 next','json');
 view.highlight({start:6,end:8});
 assert.equal(view.view.state.selection.main.from,6);
 assert.equal(position.textContent,'Ln 2, Col 3');
 assert.notEqual(dom.window.document.activeElement,view.view.contentDOM);
 await new Promise(resolve=>setTimeout(resolve,130));
 assert.deepEqual(selections,[]);
 view.readOnly=true;
 assert.equal(view.view.state.readOnly,true);
 view.view.dispatch({selection:{anchor:1}});
 await new Promise(resolve=>setTimeout(resolve,130));
 assert.deepEqual(selections,[[1,1]]);
 view.destroy();dom.window.close();
});

test('editor snapshot preserves a draft when switching original source', () => {
 const {dom,host,position}=editorDOM();
 const view=new editor.SourceEditor(host,null,position,()=>{});
 view.setText('a\r\nb','yaml');
 view.view.dispatch({changes:{from:2,insert:'x'}});
 const draft=view.snapshot();
 view.setText('original','yaml');
 view.restore(draft);
 assert.equal(view.getText(),'a\nxb');
 assert.deepEqual(view.patches(),[{start:2,end:2,text:'x'}]);
 view.destroy();dom.window.close();
});

test('keyboard Tab uses the editor command and exposes a labelled textbox', () => {
 const {dom,host,position}=editorDOM();
 const view=new editor.SourceEditor(host,null,position);
 view.setText('value','json');
 const content=host.querySelector('.cm-content');
 assert.equal(content.getAttribute('aria-label'),'Document source');
 assert.equal(content.getAttribute('role'),'textbox');
 content.dispatchEvent(new dom.window.KeyboardEvent('keydown',{key:'Tab',keyCode:9,bubbles:true,cancelable:true}));
 assert.equal(view.getText(),'  value');
 assert.deepEqual(view.patches(),[{start:0,end:0,text:'  '}]);
 view.destroy();dom.window.close();
});
