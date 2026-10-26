import test from "node:test";
import assert from "node:assert/strict";
import { layout, hitTest, worldPoint } from "./tree.js";
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
