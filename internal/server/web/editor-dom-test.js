// Shared DOM harness. These checks exercise CodeMirror, not a native browser.
import { JSDOM } from 'jsdom';
export function editorDOM() {
 const dom = new JSDOM('<!doctype html><div id="host"></div><span id="position"></span>', {pretendToBeVisual:true});
 const win = dom.window;
 for (const name of ['window','document','navigator','MutationObserver','HTMLElement','Node','Range','DOMRect','Window'])
  Object.defineProperty(globalThis,name,{value:name === 'window' ? win : win[name],configurable:true,writable:true});
 globalThis.ResizeObserver=class {observe(){} disconnect(){}};
 globalThis.getComputedStyle=win.getComputedStyle.bind(win);
 globalThis.requestAnimationFrame=win.requestAnimationFrame.bind(win);
 globalThis.cancelAnimationFrame=win.cancelAnimationFrame.bind(win);
 win.Range.prototype.getClientRects ??= () => [];
 win.Range.prototype.getBoundingClientRect ??= () => ({top:0,bottom:0,left:0,right:0,width:0,height:0});
 return {dom,host:win.document.getElementById('host'),position:win.document.getElementById('position')};
}
