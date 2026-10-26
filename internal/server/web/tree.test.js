import test from "node:test";
import assert from "node:assert/strict";
import { TreeCanvas, layout, hitTest, worldPoint } from "./tree.js";
import * as treeModule from "./tree.js";
const expandLevels = (...args) => {
  assert.equal(typeof treeModule.expandLevels, "function", "missing subtree expansion");
  return treeModule.expandLevels(...args);
};
const scalar = (id, label, value) => ({
  id,
  label,
  value,
  kind: "string",
  total: 0,
});
const obj = (id, label, children) => ({
  id,
  label,
  kind: "object",
  total: children.length,
  children,
});

test("collapse all closes the selected subtree including hidden and off-page descendants", () => {
  const ranges = [
    { id: "root" }, { id: "selected", parent: "root" },
    { id: "hidden", parent: "selected" }, { id: "deep", parent: "hidden" },
    { id: "off-page", parent: "selected" },
    { id: "sibling", parent: "root" }, { id: "other", parent: "sibling" },
  ];
  const expanded = new Set(["root", "selected", "deep", "off-page", "sibling", "other"]);
  treeModule.collapseAll("selected", ranges, expanded);
  assert.deepEqual([...expanded], ["root", "sibling", "other"]);
  expanded.add("selected");
  assert.equal(expanded.has("deep"), false, "reopening must not reopen descendants");
});

test("collapse all on a scalar leaves other branches expanded", () => {
  const expanded = new Set(["root", "sibling"]);
  treeModule.collapseAll("scalar", [
    { id: "root" }, { id: "scalar", parent: "root" }, { id: "sibling", parent: "root" },
  ], expanded);
  assert.deepEqual([...expanded], ["root", "sibling"]);
});

test("collapse all at the root or without a selection collapses the whole tree", () => {
  const ranges = [{ id: "root" }, { id: "child", parent: "root" }];
  for (const selection of ["root", null]) {
    const expanded = new Set(["root", "child"]);
    treeModule.collapseAll(selection, ranges, expanded);
    assert.equal(expanded.size, 0);
  }
});
test("depth columns align despite unequal card widths and heights", () => {
  const a = obj("2", "short", [scalar("5", "x", "1")]);
  const b = obj("3", "much longer label", [
    scalar("6", "description", "a".repeat(180)),
    scalar("7", "ok", "true"),
  ]);
  const c = obj("4", "grandchild", []);
  b.children.push(c);
  b.total++;
  const root = obj("1", "$", [a, b]);
  const records = new Map([root, a, b, c].map((n) => [n.id, n]));
  const result = layout(root, records, new Set(["1", "2", "3", "4"]));
  const cards = result.cards;
  assert.equal(
    cards.find((n) => n.id === "2").x,
    cards.find((n) => n.id === "3").x,
  );
  for (let i = 0; i < cards.length; i++)
    for (let j = i + 1; j < cards.length; j++) {
      const a = cards[i],
        b = cards[j];
      assert.ok(
        a.x + a.w <= b.x ||
          b.x + b.w <= a.x ||
          a.y + a.h <= b.y ||
          b.y + b.h <= a.y,
        "cards overlap",
      );
    }
  assert.equal(result.edges.length, 3);
});
test("clicking field hits the value and honors viewport transforms", () => {
  const root = obj("1", "$", [scalar("2", "name", "Alice")]);
  const result = layout(root, new Map([["1", root]]), new Set(["1"]));
  const c = result.cards[0];
  const p = worldPoint((c.x + 70) * 2 + 50, (c.y + 50) * 2 + 20, {
    x: 50,
    y: 20,
    scale: 2,
  });
  assert.deepEqual(hitTest(result, p.x, p.y), { type: "select", id: "2" });
  assert.deepEqual(hitTest(result, c.x + 10, c.y + 15), {
    type: "toggle",
    id: "1",
  });
});
test("collapsed and paginated collections remain bounded", () => {
  const root = {
    id: "1",
    label: "rows",
    kind: "array",
    total: 15000,
    offset: 0,
    children: Array.from({ length: 20 }, (_, i) =>
      obj(String(i + 2), `[${i}]`, []),
    ),
  };
  assert.equal(layout(root, new Map([["1", root]]), new Set()).cards.length, 1);
  const expanded = layout(root, new Map([["1", root]]), new Set(["1"]));
  assert.equal(expanded.cards.length, 21);
  const c = expanded.cards[0];
  assert.deepEqual(hitTest(expanded, c.x + c.w - 18, c.y + c.h - 10), {
    type: "page",
    id: "1",
    offset: 20,
  });
});

test("right click resolves the transformed node without selecting, toggling, or paging", () => {
  const root = obj("1", "$", [scalar("2", "name", "Alice")]);
  const graph = layout(root, new Map([["1", root]]), new Set(["1"]));
  const listeners = new Map();
  const calls = [];
  const canvas = { setAttribute() {}, addEventListener: (type, fn) => listeners.set(type, fn),
    getBoundingClientRect: () => ({ left: 100, top: 50 }) };
  const tree = Object.create(TreeCanvas.prototype);
  Object.assign(tree, { canvas, graph, view: { x: 30, y: 20, scale: 2 }, resize() {},
    callbacks: { action: () => assert.fail("right click triggered left-click action"), context: (...args) => calls.push(args) } });
  tree.bind();
  const c = graph.cards[0];
  const event = (x, y) => ({ clientX: 100 + 30 + x * 2, clientY: 50 + 20 + y * 2,
    preventDefault() { this.prevented = true; } });
  const row = event(c.x + 70, c.y + 50);
  assert.ok(listeners.has("contextmenu"), "no right-click handler");
  listeners.get("contextmenu")(row);
  assert.deepEqual(calls[0], ["2", row.clientX, row.clientY]);
  assert.equal(row.prevented, true);
  const toggle = event(c.x + 10, c.y + 15);
  listeners.get("contextmenu")(toggle);
  assert.equal(calls[1][0], "1");
  const empty = event(-500, -500);
  listeners.get("contextmenu")(empty);
  assert.equal(calls.length, 2);
  assert.equal(empty.prevented, undefined);
});


test("double-click toggles collection titles under a transformed viewport", () => {
  const root = obj("1", "$", [scalar("2", "name", "Alice")]);
  const records = new Map([["1", root]]), expanded = new Set(["1"]);
  const graph = layout(root, records, expanded);
  const listeners = new Map(), calls = [];
  const canvas = { setAttribute() {}, addEventListener: (type, fn) => listeners.set(type, fn),
    getBoundingClientRect: () => ({ left: 100, top: 50 }) };
  const tree = Object.create(TreeCanvas.prototype);
  Object.assign(tree, { canvas, graph, view: { x: 30, y: 20, scale: 2 }, resize() {},
    callbacks: { action: (hit) => {
      calls.push(hit);
      if (hit.type === "toggle") {
        if (expanded.has(hit.id)) expanded.delete(hit.id);
        else expanded.add(hit.id);
        tree.graph = layout(root, records, expanded);
      }
    } } });
  tree.bind();
  assert.ok(listeners.has("dblclick"), "missing title double-click handler");
  const c = graph.cards[0];
  const click = (x, y) => listeners.get("dblclick")({
    clientX: 130 + x * 2, clientY: 70 + y * 2, preventDefault() {} });
  click(c.x + 70, c.y + 15);
  assert.deepEqual(calls, [{ type: "toggle", id: "1" }]);
  assert.equal(tree.graph.cards[0].open, false, "expanded node should collapse");
  assert.equal(tree.graph.cards[0].rows.length, 0);
  click(c.x + 70, c.y + 15);
  assert.equal(tree.graph.cards[0].open, true, "collapsed node should expand");
  assert.equal(tree.graph.cards[0].rows.length, 1);
  click(c.x + 10, c.y + 15);
  click(c.x + 70, c.y + 50);
  click(-100, -100);
  assert.equal(calls.length, 2);
});

test("expansion opens exactly X levels beneath the target and preserves other branches", async () => {
  const leaf = obj("4", "leaf", []);
  const grandchild = obj("3", "grandchild", [leaf]);
  const child = obj("2", "child", [grandchild]);
  const root = obj("1", "$", [child]);
  const all = new Map([root, child, grandchild, leaf].map(n => [n.id, n]));
  const records = new Map([["1", root], ["2", { ...child, children: undefined }]]);
  const expanded = new Set(["1", "unrelated"]), loaded = [];
  const load = async (id) => { loaded.push(id); const n = all.get(id); records.set(id, n); return n; };
  await expandLevels("2", 2, records, expanded, load);
  assert.deepEqual([...expanded].sort(), ["1", "2", "3", "unrelated"]);
  assert.deepEqual(loaded, ["2", "3"]);
  await expandLevels("2", 1, records, expanded, load);
  assert.ok(expanded.has("3"), "expansion must not collapse deeper branches");
  assert.deepEqual(loaded, ["2", "3"], "loaded pages should be reused");
});

test("expansion ignores scalars and rejects invalid depths without loading", async () => {
  const records = new Map([["1", scalar("1", "name", "Alice")]]), expanded = new Set();
  const load = () => assert.fail("unexpected page load");
  await expandLevels("1", 2, records, expanded, load);
  assert.equal(expanded.size, 0);
  for (const depth of [0, -1, 1.5, NaN, Infinity])
    await assert.rejects(() => expandLevels("1", depth, records, expanded, load));
});

test("expansion stays on the current page and bounds large traversals", async () => {
  const children = Array.from({ length: 1100 }, (_, i) => obj(String(i + 2), "item", []));
  const root = { ...obj("1", "$", children), offset: 20, total: 2000 };
  const records = new Map([["1", root]]), expanded = new Set();
  const load = async (id) => { const n = children.find(n => n.id === id); records.set(id, n); return n; };
  const result = await expandLevels("1", 2, records, expanded, load);
  assert.equal(result.truncated, true);
  assert.equal(expanded.size, 1000);
  assert.equal(records.get("1").offset, 20);
});
