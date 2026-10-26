import test from "node:test";
import assert from "node:assert/strict";
import { NodeContextMenu, menuTarget, nodeActions, runNodeAction } from "./context-menu.js";

const graph = { cards: [
  { id: "1", kind: "object", label: "$", rows: [{ id: "2", label: "name", kind: "string" }], children: [{ id: "2" }, { id: "3" }] },
  { id: "3", label: "items", kind: "array", rows: [], children: [{ id: "4" }] },
  { id: "4", label: "[0]", kind: "object", rows: [], children: [] },
] };

test("menu targets card headers and scalar rows with their actual parent", () => {
  assert.equal(menuTarget(graph, "2").parentKind, "object");
  assert.equal(menuTarget(graph, "2").label, "name");
  assert.equal(menuTarget(graph, "4").parentKind, "array");
  assert.equal(menuTarget(graph, "1").parentKind, undefined);
  assert.equal(menuTarget(graph, "missing"), null);
});

test("root, array items, and CSV cells expose only valid actions", () => {
  assert.deepEqual(nodeActions(menuTarget(graph, "1"), "json"), []);
  assert.deepEqual(nodeActions(menuTarget(graph, "2"), "json"), ["clone", "rename", "delete"]);
  assert.deepEqual(nodeActions(menuTarget(graph, "4"), "json"), ["clone", "delete"]);
  assert.deepEqual(nodeActions(menuTarget(graph, "2"), "csv"), []);
  assert.deepEqual(nodeActions(menuTarget(graph, "4"), "csv"), ["clone", "delete"]);
});

function harness(overrides = {}) {
  const calls = [];
  return { calls, ready: () => true, prompt: () => "new key", confirm: () => true,
    mutate: async (a) => calls.push(a), ...overrides };
}

test("duplicate and rename send the clicked node and requested key", async () => {
  for (const op of ["clone", "rename"]) {
    const h = harness();
    await runNodeAction(op, menuTarget(graph, "2"), "json", h);
    assert.deepEqual(h.calls, [{ op, node: "2", key: "new key" }]);
  }
});

test("duplicating an array item does not ask for a key", async () => {
  const h = harness({ prompt: () => assert.fail("array item prompted for key") });
  await runNodeAction("clone", menuTarget(graph, "4"), "json", h);
  assert.deepEqual(h.calls, [{ op: "clone", node: "4", key: "" }]);
});

test("cancelled prompts, rejected deletion, drafts, and invalid actions never mutate", async () => {
  for (const op of ["clone", "rename"]) {
    const h = harness({ prompt: () => null });
    await runNodeAction(op, menuTarget(graph, "2"), "json", h);
    assert.deepEqual(h.calls, []);
  }
  for (const [op, id, format, overrides] of [
    ["delete", "2", "json", { confirm: () => false }],
    ["delete", "2", "json", { ready: () => false }],
    ["delete", "1", "json", {}],
    ["rename", "4", "json", {}],
    ["delete", "2", "csv", {}],
  ]) {
    const h = harness(overrides);
    await runNodeAction(op, menuTarget(graph, id), format, h);
    assert.deepEqual(h.calls, []);
  }
});

test("deletion confirms and submits the clicked node", async () => {
  const h = harness();
  await runNodeAction("delete", menuTarget(graph, "2"), "json", h);
  assert.deepEqual(h.calls, [{ op: "delete", node: "2", key: "" }]);
});

// Minimal DOM surface for exercising the menu's event handlers and focus behavior.
function menuFixture() {
  const doc = { activeElement: null, defaultView: { innerWidth: 800, innerHeight: 600 } };
  const listeners = {};
  const buttons = ["clone", "rename", "delete"].map((op) => ({
    dataset: { nodeAction: op }, disabled: false,
    focus() { doc.activeElement = this; },
    closest() { return this; },
  }));
  const label = {};
  const element = {
    ownerDocument: doc, hidden: true, style: {},
    querySelectorAll: () => buttons, querySelector: () => label,
    addEventListener: (name, fn) => { listeners[name] = fn; },
    getBoundingClientRect: () => ({ width: 180, height: 140 }),
    focus() { doc.activeElement = this; },
  };
  const calls = [];
  const menu = new NodeContextMenu(element, (...args) => calls.push(args));
  let returned = 0;
  const opener = { focus() { returned++; } };
  const key = (key) => listeners.keydown({ key, preventDefault() {} });
  return { menu, element, label, buttons, doc, calls, opener, key, listeners, returned: () => returned };
}

test("menu stays in the viewport and keyboard navigation skips unavailable rename", () => {
  const f = menuFixture();
  const target = menuTarget(graph, "4");
  f.menu.open(target, "json", 790, 590, f.opener);
  assert.equal(f.element.hidden, false);
  assert.equal(f.element.style.left, "616px");
  assert.equal(f.element.style.top, "456px");
  assert.equal(f.label.textContent, "[0]");
  assert.equal(f.buttons[1].disabled, true);
  assert.equal(f.doc.activeElement, f.buttons[0]);
  f.key("ArrowDown");
  assert.equal(f.doc.activeElement, f.buttons[2]);
  f.key("ArrowDown");
  assert.equal(f.doc.activeElement, f.buttons[0]);
  f.key("End");
  assert.equal(f.doc.activeElement, f.buttons[2]);
  f.key("Home");
  assert.equal(f.doc.activeElement, f.buttons[0]);
  f.key("ArrowUp");
  assert.equal(f.doc.activeElement, f.buttons[2]);
  f.key("Escape");
  assert.equal(f.element.hidden, true);
  assert.equal(f.returned(), 1);
  assert.equal(f.menu.target, null);
});

test("menu activation uses its captured target and dismisses before editing", () => {
  const f = menuFixture();
  const target = menuTarget(graph, "2");
  f.menu.open(target, "json", 20, 30, f.opener);
  f.listeners.click({ target: f.buttons[2] });
  assert.deepEqual(f.calls, [["delete", target]]);
  assert.equal(f.element.hidden, true);
  f.menu.open(menuTarget(graph, "1"), "json", 20, 30, f.opener);
  assert.equal(f.doc.activeElement, f.element);
  f.listeners.click({ target: f.buttons[2] });
  assert.equal(f.calls.length, 1);
  f.key("Tab");
  assert.equal(f.element.hidden, true);
});
