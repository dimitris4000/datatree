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

globalThis.getComputedStyle ??= () => ({});
globalThis.ResizeObserver ??= class { observe() {} };

function editorFixture(text) {
 const listeners = new Map();
 const doc = {
  createElement: tag => ({tagName:tag,style:{},children:[],setAttribute(){},append(...children){this.children.push(...children);},replaceChildren(...children){this.children=children;}}),
  createTextNode: text => ({textContent:text}),
  createDocumentFragment: () => ({children:[],append(child){this.children.push(child);}}),
  addEventListener() {},
 };
 const source = {value:text,ownerDocument:doc,selectionStart:0,selectionEnd:0,scrollTop:0,scrollLeft:0,clientHeight:60,clientWidth:200,
  before(){},addEventListener(type,fn){listeners.set(type,fn);},
  setSelectionRange(start,end){this.selectionStart=start;this.selectionEnd=end;listeners.get('select')?.();},
 };
 const gutter=doc.createElement('div');gutter.parentElement={style:{setProperty(){}}};
 const position={textContent:''}, selections=[];
 return {source,gutter,position,listeners,selections};
}

test('editor highlights and scrolls tree selections without stealing focus or feeding back', async (t) => {
 t.mock.method(globalThis,'getComputedStyle',() => ({lineHeight:'20',paddingTop:'18',fontSize:'11'}));
 t.mock.method(globalThis,'ResizeObserver',class { observe() {} });
 const fixture=editorFixture('first\nsecond\nthird\nfourth');
 const {source,gutter,position,selections}=fixture;
 const view=new editor.SourceEditor(source,gutter,position,(...range)=>selections.push(range));
 view.highlight({start:6,end:12});
 assert.deepEqual([source.selectionStart,source.selectionEnd],[6,12]);
 assert.equal(position.textContent,'Ln 2, Col 7');
 assert.equal(view.mirrorText.children[1].textContent,'second');
 assert.equal(gutter.children[0].children[1].className,'source-line active');
 await new Promise(resolve=>setTimeout(resolve,130));
 assert.deepEqual(selections,[],'programmatic selection must not loop back');
});

test('editor user selections notify once, line numbers follow scrolling and drafts', async (t) => {
 t.mock.method(globalThis,'getComputedStyle',() => ({lineHeight:'20',paddingTop:'18',fontSize:'11'}));
 t.mock.method(globalThis,'ResizeObserver',class { observe() {} });
 const fixture=editorFixture('a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n');
 const {source,gutter,position,listeners,selections}=fixture;
 const view=new editor.SourceEditor(source,gutter,position,(...range)=>selections.push(range));
 source.selectionStart=2;source.selectionEnd=3;
 listeners.get('select')();listeners.get('keyup')();
 await new Promise(resolve=>setTimeout(resolve,130));
 assert.deepEqual(selections,[[2,3]]);
 source.scrollTop=100;listeners.get('scroll')();
 assert.equal(gutter.children[0].children[0].textContent,'5');
 assert.ok(gutter.children[0].children.length<11,'gutter renders visible lines only');
 source.value='a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nnew\n';
 listeners.get('input')();
 assert.equal(view.starts.length,12);
 assert.equal(position.textContent,'Ln 2, Col 2');
});
