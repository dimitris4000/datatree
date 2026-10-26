const collection = (n) => ["object", "array", "stream"].includes(n.kind);
const short = (s, n = 44) =>
  String(s).length > n ? String(s).slice(0, n - 1) + "…" : String(s);
// Expand loaded pages only; match the renderer's bounded exploration model.
export async function expandLevels(id, levels, records, expanded, load) {
  if (!Number.isSafeInteger(levels) || levels < 1)
    throw new Error("Choose a positive whole number of levels.");
  const queue = [{ id, depth: 0 }], seen = new Set();
  let count = 0;
  for (let i = 0; i < queue.length; i++) {
    const item = queue[i];
    if (seen.has(item.id)) continue;
    seen.add(item.id);
    let node = records.get(item.id);
    if (node && !collection(node)) continue;
    if (count >= 1000) return { truncated: true };
    if (!node?.children) node = await load(item.id);
    if (!collection(node)) continue;
    expanded.add(item.id);
    count++;
    if (item.depth + 1 < levels)
      for (const child of node.children || [])
        if (collection(child)) queue.push({ id: child.id, depth: item.depth + 1 });
  }
  return { truncated: false };
}
export function layout(root, records, expanded) {
  const cards = [],
    edges = [],
    widths = [];
  let count = 0;
  function build(node, depth) {
    if (++count > 1000) return null;
    const n = records.get(node.id) || node;
    const children = n.children || [];
    const open = expanded.has(n.id);
    const rows =
      n.kind === "object" && open ? children.filter((c) => !collection(c)) : [];
    const branches = open
      ? children.filter((c) => n.kind !== "object" || collection(c))
      : [];
    const texts = [n.label, ...rows.map((c) => c.label + ": " + c.value)];
    const w = Math.min(
      360,
      Math.max(180, ...texts.map((t) => short(t).length * 6.8 + 38)),
    );
    const h =
      (collection(n) ? 40 : 66) +
      rows.length * 23 +
      (open && n.total > (n.children?.length || 0) ? 27 : 0);
    const card = { ...n, depth, w, h, rows, open, branches: [], x: 0, y: 0 };
    cards.push(card);
    widths[depth] = Math.max(widths[depth] || 0, w);
    let sum = 0;
    for (const child of branches) {
      const branch = build(child, depth + 1);
      if (branch) {
        card.branches.push(branch);
        sum += branch.span + 26;
      }
    }
    if (card.branches.length) sum -= 26;
    card.span = Math.max(h, sum);
    return card;
  }
  const tree = build(root, 0);
  const columns = [40];
  for (let i = 1; i < widths.length; i++)
    columns[i] = columns[i - 1] + widths[i - 1] + 96;
  function place(card, top) {
    card.x = columns[card.depth];
    card.y = top + (card.span - card.h) / 2;
    const sum =
      card.branches.reduce((s, c) => s + c.span, 0) +
      Math.max(0, card.branches.length - 1) * 26;
    let y = top + (card.span - sum) / 2;
    for (const child of card.branches) {
      place(child, y);
      edges.push({ from: card, to: child });
      y += child.span + 26;
    }
  }
  if (tree) place(tree, 40);
  return {
    cards,
    edges,
    width: Math.max(0, ...cards.map((c) => c.x + c.w)) + 40,
    height: (tree?.span || 0) + 80,
    truncated: count > 1000,
  };
}
export function worldPoint(x, y, view) {
  return { x: (x - view.x) / view.scale, y: (y - view.y) / view.scale };
}
export function hitTest(graph, x, y) {
  for (let i = graph.cards.length - 1; i >= 0; i--) {
    const c = graph.cards[i];
    if (x < c.x || x > c.x + c.w || y < c.y || y > c.y + c.h) continue;
    if (y < c.y + 36) {
      if (x < c.x + 29 && collection(c)) return { type: "toggle", id: c.id };
      return { type: "select", id: c.id };
    }
    const row = Math.floor((y - c.y - 40) / 23);
    if (row >= 0 && row < c.rows.length)
      return { type: "select", id: c.rows[row].id };
    if (c.open && c.total > (c.children?.length || 0) && y > c.y + c.h - 27) {
      const step = 20;
      return {
        type: "page",
        id: c.id,
        offset:
          x > c.x + c.w / 2
            ? (c.offset || 0) + (c.children?.length || 0) < c.total
              ? (c.offset || 0) + step
              : c.offset || 0
            : Math.max(0, (c.offset || 0) - step),
      };
    }
    return { type: "select", id: c.id };
  }
  return null;
}
function rounded(ctx, x, y, w, h, r = 6) {
  ctx.beginPath();
  ctx.roundRect(x, y, w, h, r);
}
const colors = {
  string: "#526647",
  number: "#bd5b72",
  boolean: "#828a3e",
  null: "#8b8791",
  tagged: "#96704d",
  alias: "#708c92",
};
export class TreeCanvas {
  constructor(host, callbacks) {
    this.host = host;
    this.callbacks = callbacks;
    this.view = { x: 20, y: 20, scale: 1 };
    this.records = new Map();
    this.expanded = new Set();
    this.graph = { cards: [], edges: [] };
    this.selected = null;
    if (globalThis.p5) {
      this.engine = new globalThis.p5((p) => {
        p.setup = () => {
          const c = p.createCanvas(host.clientWidth, host.clientHeight);
          this.canvas = c.elt;
          p.noLoop();
          this.bind();
          this.draw();
        };
        p.draw = () => this.draw();
      }, host);
    } else {
      this.canvas = document.createElement("canvas");
      host.append(this.canvas);
      this.bind();
    }
    this.observer = new ResizeObserver(() => this.resize());
    this.observer.observe(host);
  }
  bind() {
    const c = this.canvas;
    c.setAttribute("aria-hidden", "true");
    let down = null;
    c.addEventListener("pointerdown", (e) => {
      if (e.button !== 0) return;
      down = {
        x: e.clientX,
        y: e.clientY,
        vx: this.view.x,
        vy: this.view.y,
        moved: false,
      };
      c.setPointerCapture(e.pointerId);
    });
    c.addEventListener("pointermove", (e) => {
      if (!down) return;
      const dx = e.clientX - down.x,
        dy = e.clientY - down.y;
      if (Math.hypot(dx, dy) > 4) down.moved = true;
      if (down.moved) {
        this.view.x = down.vx + dx;
        this.view.y = down.vy + dy;
        this.draw();
      }
    });
    c.addEventListener("pointerup", (e) => {
      if (!down) return;
      const moved = down.moved;
      down = null;
      if (moved) return;
      const box = c.getBoundingClientRect();
      const p = worldPoint(
        e.clientX - box.left,
        e.clientY - box.top,
        this.view,
      );
      const hit = hitTest(this.graph, p.x, p.y);
      if (hit) this.callbacks.action(hit);
    });
    c.addEventListener("dblclick", (e) => {
      const box = c.getBoundingClientRect();
      const p = worldPoint(e.clientX - box.left, e.clientY - box.top, this.view);
      const hit = hitTest(this.graph, p.x, p.y);
      const card = this.graph.cards.find((card) => card.id === hit?.id);
      if (hit?.type !== "select" || !card || !collection(card) || p.y >= card.y + 36)
        return;
      e.preventDefault();
      this.callbacks.action({ type: "expand", id: card.id, levels: 1 });
    });
    c.addEventListener("contextmenu", (e) => {
      const box = c.getBoundingClientRect();
      const p = worldPoint(e.clientX - box.left, e.clientY - box.top, this.view);
      const hit = hitTest(this.graph, p.x, p.y);
      // Pagination controls are navigation, not a node action target.
      if (!hit || hit.type === "page") return;
      e.preventDefault();
      down = null;
      this.callbacks.context?.(hit.id, e.clientX, e.clientY);
    });
    c.addEventListener("pointercancel", () => {
      down = null;
    });
    c.addEventListener(
      "wheel",
      (e) => {
        e.preventDefault();
        const r = c.getBoundingClientRect();
        this.zoom(
          Math.exp(-e.deltaY * 0.001),
          e.clientX - r.left,
          e.clientY - r.top,
        );
      },
      { passive: false },
    );
    this.resize();
  }
  resize() {
    if (!this.canvas) return;
    const r = this.host.getBoundingClientRect();
    this.width = r.width;
    this.height = r.height;
    const dpr = Math.min(devicePixelRatio || 1, 2);
    if (this.engine) this.engine.resizeCanvas(this.width, this.height, true);
    else {
      this.canvas.width = Math.round(this.width * dpr);
      this.canvas.height = Math.round(this.height * dpr);
    }
    this.draw();
  }
  install(root, records, expanded) {
    this.root = root;
    this.records = records;
    this.expanded = expanded;
    this.relayout();
  }
  relayout() {
    if (this.root) this.graph = layout(this.root, this.records, this.expanded);
    this.draw();
    this.callbacks.layout?.(this.graph);
  }
  zoom(factor, x = this.width / 2, y = this.height / 2) {
    const old = this.view.scale;
    const scale = Math.max(0.08, Math.min(3, old * factor));
    this.view.x = x - ((x - this.view.x) * scale) / old;
    this.view.y = y - ((y - this.view.y) * scale) / old;
    this.view.scale = scale;
    this.draw();
  }
  fit() {
    if (!this.root) return;
    this.view.scale = Math.min(
      1.2,
      Math.max(
        0.08,
        Math.min(
          (this.width - 70) / this.graph.width,
          (this.height - 100) / this.graph.height,
        ),
      ),
    );
    this.view.x = (this.width - this.graph.width * this.view.scale) / 2;
    this.view.y = (this.height - this.graph.height * this.view.scale) / 2 - 15;
    this.draw();
  }
  focus(id) {
    const card = this.graph.cards.find(
      (c) => c.id === id || c.rows.some((r) => r.id === id),
    );
    if (card) {
      this.view.x = this.width / 2 - (card.x + card.w / 2) * this.view.scale;
      this.view.y = this.height / 2 - (card.y + card.h / 2) * this.view.scale;
      this.draw();
    }
  }
  draw() {
    if (!this.canvas) return;
    const ctx = this.canvas.getContext("2d");
    const ratio = this.canvas.width / (this.width || 1);
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    ctx.clearRect(0, 0, this.width, this.height);
    ctx.fillStyle = "#f6f8f3";
    ctx.fillRect(0, 0, this.width, this.height);
    const { x, y, scale } = this.view;
    const grid = 24 * scale;
    ctx.strokeStyle = "#dfe5da";
    ctx.lineWidth = 0.5;
    ctx.beginPath();
    if (grid > 5) {
      for (let a = x % grid; a < this.width; a += grid) {
        ctx.moveTo(a, 0);
        ctx.lineTo(a, this.height);
      }
      for (let b = y % grid; b < this.height; b += grid) {
        ctx.moveTo(0, b);
        ctx.lineTo(this.width, b);
      }
    }
    ctx.stroke();
    ctx.save();
    ctx.translate(x, y);
    ctx.scale(scale, scale);
    for (const { from: a, to: b } of this.graph.edges) {
      const ax = a.x + a.w,
        ay = a.y + a.h / 2,
        bx = b.x,
        by = b.y + b.h / 2;
      ctx.strokeStyle = "#8b9c90";
      ctx.lineWidth = 1.5;
      ctx.beginPath();
      ctx.moveTo(ax, ay);
      ctx.bezierCurveTo(ax + 48, ay, bx - 48, by, bx - 8, by);
      ctx.stroke();
      ctx.fillStyle = "#8b9c90";
      ctx.beginPath();
      ctx.moveTo(bx, by);
      ctx.lineTo(bx - 8, by - 4);
      ctx.lineTo(bx - 8, by + 4);
      ctx.closePath();
      ctx.fill();
    }
    ctx.font = "11px ui-monospace, SFMono-Regular, Menlo, monospace";
    for (const c of this.graph.cards) {
      if (
        (c.x + c.w) * scale + x < 0 ||
        c.x * scale + x > this.width ||
        (c.y + c.h) * scale + y < 0 ||
        c.y * scale + y > this.height
      )
        continue;
      rounded(ctx, c.x, c.y, c.w, c.h);
      ctx.fillStyle = "#ffffff";
      ctx.shadowColor = "#1f3e2410";
      ctx.shadowBlur = 8;
      ctx.shadowOffsetY = 2;
      ctx.fill();
      ctx.shadowBlur = 0;
      ctx.shadowOffsetY = 0;
      ctx.strokeStyle = this.selected === c.id ? "#64846e" : "#a7b5a5";
      ctx.lineWidth = this.selected === c.id ? 2 : 1;
      ctx.stroke();
      ctx.save();
      rounded(ctx, c.x, c.y, c.w, c.h);
      ctx.clip();
      ctx.fillStyle = "#f0f4ec";
      ctx.fillRect(c.x, c.y, c.w, 35);
      ctx.fillStyle = "#839479";
      ctx.fillText(
        collection(c) ? (c.open ? "▾" : "▸") : "·",
        c.x + 11,
        c.y + 22,
      );
      ctx.fillStyle = "#b67943";
      ctx.font = "bold 12px ui-monospace, SFMono-Regular, Menlo, monospace";
      ctx.fillText(
        short(c.label, Math.max(8, Math.floor((c.w - 85) / 7))),
        c.x + 31,
        c.y + 22,
      );
      ctx.font = "10px ui-monospace, SFMono-Regular, Menlo, monospace";
      ctx.fillStyle = "#7f8e7c";
      const count = collection(c) ? `(${c.total})` : "";
      ctx.fillText(
        count,
        c.x + c.w - ctx.measureText(count).width - 13,
        c.y + 22,
      );
      c.rows.forEach((r, i) => {
        const top = c.y + 40 + i * 23;
        if (this.selected === r.id) {
          ctx.fillStyle = "#e9f1df";
          ctx.fillRect(c.x + 1, top, c.w - 2, 23);
        }
        ctx.font = "bold 11px ui-monospace, SFMono-Regular, Menlo, monospace";
        const key = short(r.label, 22) + ": ";
        ctx.fillStyle = "#8a639c";
        ctx.fillText(key, c.x + 13, top + 15);
        const kw = ctx.measureText(key).width;
        ctx.font = "11px ui-monospace, SFMono-Regular, Menlo, monospace";
        ctx.fillStyle = colors[r.kind] || "#586c53";
        const value = r.kind === "string" ? JSON.stringify(r.value) : r.value;
        ctx.fillText(
          short(value, Math.max(3, Math.floor((c.w - kw - 28) / 6.7))),
          c.x + 13 + kw,
          top + 15,
        );
      });
      if (!collection(c)) {
        ctx.fillStyle = colors[c.kind] || "#586c53";
        ctx.font = "11px ui-monospace, SFMono-Regular, Menlo, monospace";
        ctx.fillText(short(c.value), c.x + 12, c.y + 54);
      }
      if (c.open && c.total > (c.children?.length || 0)) {
        ctx.fillStyle = "#f6f8f2";
        ctx.fillRect(c.x, c.y + c.h - 27, c.w, 27);
        ctx.fillStyle = "#73876b";
        ctx.font = "10px ui-monospace, SFMono-Regular, Menlo, monospace";
        ctx.fillText("‹ Previous", c.x + 12, c.y + c.h - 10);
        const label = `${(c.offset || 0) + 1}–${Math.min((c.offset || 0) + (c.children?.length || 0), c.total)} / ${c.total}  Next ›`;
        ctx.fillText(
          label,
          c.x + c.w - ctx.measureText(label).width - 10,
          c.y + c.h - 10,
        );
      }
      ctx.restore();
    }
    ctx.restore();
    this.callbacks.zoom?.(Math.round(scale * 100));
  }
}
