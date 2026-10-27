import { mountDiffPanel } from "./diff.js";
import { mountFileBrowser } from "./file-browser.js";
import { mountConversionDialog, downloadConvertedCopy } from "./conversion.js";
import { mountValidationPanel, canValidateDocument } from "./validation.js";
import { revealResult, mountFindPanel } from "./find.js";
import { SourceEditor, normalizedSource, nodeAt, revealPath } from "./source-editor.js";
import { setupInspectorFolding } from "./folding.js";
import { TreeCanvas, expandLevels, collapseAll, diffBadge, validationBadge } from "./tree.js";
import { NodeContextMenu, menuTarget, runNodeAction } from "./context-menu.js";
const $ = (id) => document.getElementById(id);
const source = $("source"),
  inspector = $("inspector");
let state = { revision: 0 },
  original = "",
  workingDraft = "",
  sourceDirty = false,
  inspectorDirty = false,
  showOriginal = false,
  busy = false,
  selection = null;
let records = new Map(),
  expanded = new Set();
let sourceRanges = [], sourceByID = new Map(), sourceSync = 0;
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
async function api(path, data) {
  const options =
    data === undefined
      ? {}
      : {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ revision: state.revision, ...data }),
        };
  const r = await fetch(path, options);
  const result = await r.json();
  if (!r.ok) throw new Error(result.error || `Request failed (${r.status})`);
  return result;
}
function setBusy(value) {
  if (value) nodeMenu.close(false);
  busy = value;
  $("expand-levels").disabled = value;
  $("expand-selected").disabled = value;
  source.readOnly = value || showOriginal;
  document
    .querySelectorAll(".toolbar-actions button,#apply,#example")
    .forEach((b) => (b.disabled = value));
  if (!value) {
    $("undo").disabled = !state.canUndo;
    $("redo").disabled = !state.canRedo;
    $("apply").disabled = showOriginal;
  }
}
function ingest(n) {
  records.set(n.id, n);
  for (const c of n.children || []) {
    if (c.children) ingest(c);
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
  source.value = showOriginal ? original : state.source || workingDraft;
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
  $("undo").disabled = !state.canUndo;
  $("redo").disabled = !state.canRedo;
  $("empty").hidden = !!state.root;
  $("draft-state").textContent = sourceDirty
    ? "Unapplied changes"
    : "No changes";
  $("apply").disabled = showOriginal;
  $("working-tab").classList.toggle("active", !showOriginal);
  $("original-tab").classList.toggle("active", showOriginal);
  $("working-tab").setAttribute("aria-pressed", String(!showOriginal));
  $("original-tab").setAttribute("aria-pressed", String(showOriginal));
  source.readOnly = showOriginal || busy;
  sourceEditor.refresh();
}
async function install(next, { fit = false, imported = false } = {}) {
  nodeMenu.close(false);
  findPanel.controller.clear();
  diffPanel.controller.invalidateDocument(imported);
  sourceSync++;
  sourceRanges = [];
  sourceByID = new Map();
  state = next;
  validationPanel.controller.install(state.schema);
  sourceDirty = false;
  inspectorDirty = false;
  workingDraft = state.source || "";
  selection = null;
  $("selection-expansion").hidden = true;
  records = new Map();
  expanded = new Set();
  if (imported) original = await fetch("/api/original").then((r) => r.text());
  updateControls();
  if (state.root) {
    const mapping = await api(`/api/source-map?revision=${state.revision}`);
    sourceRanges = mapping.ranges;
    sourceByID = new Map(sourceRanges.map(range => [range.id, range]));
    const root = await page(state.root);
    expanded.add(root.id);
    // Open the first level of containers, bounded by the first page.
    for (const child of root.children || []) {
      if (["object", "array", "stream"].includes(child.kind)) {
        await page(child.id);
        expanded.add(child.id);
        const childData = records.get(child.id);
        for (const grandchild of childData.children || []) {
          if (grandchild.kind === "object") expanded.add(grandchild.id);
        }
      }
    }
    tree.install(root, records, expanded);
    if (fit) tree.fit();
    await select(root.id, false);
    if (diffPanel.controller.visible) await diffPanel.controller.refresh();
  }
}
async function operation(path, data, options = {}) {
  if (!ready() || !canLeaveInspector()) return;
  setBusy(true);
  try {
    await install(await api(path, data), options);
    status(
      options.imported
        ? (state.schema ? `File opened. Attached ${state.schema.filename}; press Validate to check it.` : "File opened. Click a field or card to inspect it.")
        : "Changes applied. Download to save a copy.",
    );
  } catch (e) {
    status(e.message, true);
  } finally {
    setBusy(false);
  }
}
async function select(id, guard = true, syncSource = true) {
  if (guard && !canLeaveInspector()) return;
  if (syncSource) {
    sourceSync++;
    if (sourceSynchronized()) sourceEditor.highlight(sourceByID.get(id));
  }
  selection = id;
  tree.selected = id;
  const node = records.get(id) || tree.graph.cards
    .flatMap((card) => [card, ...card.rows]).find((node) => node.id === id);
  $("selection-expansion").hidden = !node ||
    !["object", "array", "stream"].includes(node.kind);
  tree.draw();
  try {
    if (globalThis.htmx) {
      await htmx.ajax(
        "GET",
        `/inspector?node=${encodeURIComponent(id)}&revision=${state.revision}`,
        { target: "#inspector", swap: "innerHTML" },
      );
    } else {
      throw new Error("htmx failed to load. Reload this page.");
    }
    inspectorDirty = false;
    validationPanel.selectionChanged();
  } catch (e) {
    status(e.message, true);
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
        const badge = diffBadge(tree.diffMarks?.[n.id]);
        const validation = validationBadge(tree.validationMarks?.[n.id]);
        b.textContent = `${"· ".repeat(c.depth)}${validation ? validation.label + " · " : ""}${badge ? badge.label + " · " : ""}${n.label} (${n.kind})`;
        b.addEventListener("click", () => select(n.id));
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
  return !!state.root && source.value === normalizedSource(state.source || "");
}
const sourceEditor = new SourceEditor(source, $("source-lines"), $("source-position"), async (start, end) => {
  if (busy || !sourceSynchronized()) return;
  const target = nodeAt(sourceRanges, start, end);
  if (!target || target.id === selection || !canLeaveInspector()) return;
  const ticket = ++sourceSync, revision = state.revision;
  const current = () => ticket === sourceSync && revision === state.revision && sourceSynchronized();
  try {
    for (const step of revealPath(sourceByID, target.id)) {
      const record = records.get(step.id);
      if (!record?.children || (record.offset || 0) !== step.offset) await page(step.id, step.offset);
      if (!current()) return;
      expanded.add(step.id);
    }
    if (!current()) return;
    tree.relayout();
    await select(target.id, false, false);
    if (current()) tree.focus(target.id);
  } catch (error) {
    if (current()) status(error.message, true);
  }
});
// Reuse the source map to reveal matches without downloading an expanded tree.
const findPanel = mountFindPanel(document, {
  invalidate: () => { sourceSync++; },
  revision: () => state.revision,
  load: async (options, revision) => {
    if (!state.root) throw new Error("Open a file first.");
    const query = new URLSearchParams({ ...options, revision });
    return api(`/api/find?${query}`);
  },
  navigate: navigateNode,
});
const diffPanel = mountDiffPanel(document, {
  revision: () => state.revision,
  invalidate: () => { sourceSync++; },
  load: (baseline, revision) => api(`/api/diff?${new URLSearchParams({ baseline, revision })}`),
  upload: (payload, revision) => api("/api/diff-baseline", { ...payload, revision }),
  navigate: navigateNode,
  revert: async (payload, revision) => {
    if (!ready() || !canLeaveInspector()) return false;
    if (revision !== state.revision) throw new Error("Document changed; refresh Changes before reverting.");
    setBusy(true);
    try {
      const next = await api("/api/diff-revert", { ...payload, revision });
      await install(next);
      status("Change reverted. Undo restores it; download to save a copy.");
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
  clear: revision => api('/api/schema-clear', { revision }),
  validate: (revision, schemaToken) => api('/api/validate', { revision, schemaToken }),
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
    const revealed = await revealResult(id, {
      ranges: sourceByID, records, expanded, load: page, current,
      relayout: (priority) => { tree.relayout(priority); return tree.graph; },
    });
    if (!revealed) return false;
    if (sourceSynchronized()) sourceEditor.highlight(sourceByID.get(id));
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
    status(e.message, true);
  }
}
source.addEventListener("input", () => {
  sourceSync++;
  nodeMenu.close(false);
  if (showOriginal) return;
  workingDraft = source.value;
  sourceDirty = source.value !== normalizedSource(state.source || "");
  $("draft-state").textContent = sourceDirty
    ? "Unapplied changes"
    : "No changes";
});
source.addEventListener("keydown", (e) => {
  if (e.key === "Tab") {
    e.preventDefault();
    source.setRangeText(
      "  ",
      source.selectionStart,
      source.selectionEnd,
      "end",
    );
    source.dispatchEvent(new Event("input"));
  }
});
document.body.addEventListener("htmx:afterSwap", (event) => {
  if (event.detail.target === inspector) setupInspectorFolding(inspector);
});
inspector.addEventListener("input", () => {
  inspectorDirty = true;
});
$("apply").addEventListener("click", async () => {
  if (busy || showOriginal || !canLeaveInspector()) return;
  setBusy(true);
  try {
    const imported = !state.root;
    const next = await api(imported ? "/api/import" : "/api/apply", {
      text: source.value,
      format: state.format || $("input-format").value,
    });
    await install(next, { fit: imported, imported });
    status("Source applied. The tree shows the validated document.");
  } catch (e) {
    status(e.message, true);
  } finally {
    setBusy(false);
  }
});
$("discard").addEventListener("click", () => {
  workingDraft = state.source || "";
  sourceDirty = false;
  updateControls();
  status("Source draft discarded.");
});
function sourceTab(isOriginal) {
  sourceSync++;
  if (!showOriginal) workingDraft = source.value;
  showOriginal = isOriginal;
  updateControls();
  if (!isOriginal) {
    source.value = workingDraft;
    $("draft-state").textContent = sourceDirty
      ? "Unapplied changes"
      : "No changes";
  }
  sourceEditor.refresh();
  if (sourceSynchronized()) sourceEditor.highlight(sourceByID.get(selection));
}
$("working-tab").addEventListener("click", () => sourceTab(false));
$("original-tab").addEventListener("click", () => sourceTab(true));
const fileBrowser = mountFileBrowser(document, {
  revision: () => state.revision,
  browse: payload => api('/api/files', payload),
  ready: () => ready() && canLeaveInspector(),
  open: async payload => {
    setBusy(true);
    try {
      await install(await api('/api/open', payload), { fit: true, imported: true });
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
$("download").addEventListener("click", () => {
  if (!ready() || !canLeaveInspector()) return;
  if (!state.root) {
    status("Open a file first.", true);
    return;
  }
  const a = document.createElement("a");
  a.href = "/api/download";
  a.download = `document.${state.format}`;
  a.click();
});
const conversionDialog = mountConversionDialog(document, {
  revision: () => state.revision,
  preview: format => api('/api/preview', { format }),
  convert: payload => api('/api/convert', payload),
  install,
  download: (text, filename) => downloadConvertedCopy(document, text, filename),
  busy: setBusy,
  status,
  ready: () => ready() && canLeaveInspector(),
});
$("convert").addEventListener("click", async () => {
  if (!ready() || !canLeaveInspector()) return;
  try {
    await conversionDialog.open($("conversion-format").value);
  } catch (e) {
    status(e.message, true);
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
  collapseAll(selection, sourceRanges, expanded);
  tree.relayout();
  tree.fit();
});
document.body.addEventListener("click", async (e) => {
  const b = e.target.closest("[data-copy],[data-reference]");
  if (!b) return;
  if (b.dataset.copy) {
    try {
      const n = $(b.dataset.copy);
      await navigator.clipboard.writeText(n.value ?? n.textContent);
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
document.body.addEventListener("htmx:confirm", (e) => {
  const form = e.detail.elt;
  if (!form.closest("#inspector")) return;
  if (!ready()) {
    e.preventDefault();
    return;
  }
  const button = e.detail.triggeringEvent?.submitter;
  if (
    button?.dataset.destructive &&
    !confirm("Delete this node and its subtree? You can undo this change.")
  ) {
    e.preventDefault();
    return;
  }
  setBusy(true);
});
document.body.addEventListener("htmx:afterRequest", (e) => {
  if (e.detail.elt.closest?.("#inspector")) setBusy(false);
});
document.body.addEventListener("htmx:responseError", () => {
  setBusy(false);
  status("Inspector request failed. Your working document is unchanged.", true);
});
document.body.addEventListener("documentChanged", async () => {
  try {
    await install(await api("/api/state"));
    status("Tree edit applied. Download to save a copy.");
  } catch (e) {
    status(e.message, true);
  } finally {
    setBusy(false);
  }
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
