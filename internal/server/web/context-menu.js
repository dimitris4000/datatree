// Resolve visible cards and object fields, including children not in the page cache.
export function menuTarget(graph, id) {
  for (const card of graph.cards) {
    const node = card.id === id ? card : card.rows.find((row) => row.id === id);
    if (!node) continue;
    const parent = card.id === id
      ? graph.cards.find((candidate) => candidate.children?.some((child) => child.id === id))
      : card;
    return { id, label: node.label, parentKind: parent?.kind };
  }
  return null;
}

export function nodeActions(target, format) {
  if (!target?.parentKind || (format === "csv" && target.parentKind !== "array")) return [];
  return target.parentKind === "object"
    ? ["clone", "rename", "delete"] : ["clone", "delete"];
}

export async function runNodeAction(op, target, format, controls) {
  if (!nodeActions(target, format).includes(op) || !controls.ready()) return;
  let key = "";
  if (op === "rename" || (op === "clone" && target.parentKind === "object")) {
    key = controls.prompt(op === "rename" ? "Rename key:" : "Key for the duplicate:",
      op === "rename" ? target.label : `${target.label}_copy`);
    if (key === null) return;
  }
  if (op === "delete" && !controls.confirm("Delete this node and its subtree? You can undo this change.")) return;
  await controls.mutate({ op, node: target.id, key });
}

export class NodeContextMenu {
  constructor(element, activate) {
    this.element = element;
    this.activate = activate;
    this.buttons = [...element.querySelectorAll("[data-node-action]")];
    element.addEventListener("click", (event) => {
      const button = event.target.closest("[data-node-action]");
      if (!button || button.disabled || !this.target) return;
      const target = this.target;
      this.close();
      this.activate(button.dataset.nodeAction, target);
    });
    element.addEventListener("keydown", (event) => {
      if (["Escape", "Tab"].includes(event.key)) {
        if (event.key === "Escape") event.preventDefault();
        this.close();
        return;
      }
      const buttons = this.buttons.filter((button) => !button.disabled);
      if (!buttons.length || !["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
      event.preventDefault();
      const index = buttons.indexOf(element.ownerDocument.activeElement);
      const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1
        : (index + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
      buttons[next].focus();
    });
  }
  open(target, format, x, y, returnFocus) {
    this.close(false);
    this.target = target;
    this.returnFocus = returnFocus;
    this.element.querySelector("[data-node-label]").textContent = target.label;
    const allowed = nodeActions(target, format);
    for (const button of this.buttons) button.disabled = !allowed.includes(button.dataset.nodeAction);
    this.element.hidden = false;
    const box = this.element.getBoundingClientRect();
    const viewport = this.element.ownerDocument.defaultView;
    this.element.style.left = `${Math.max(4, Math.min(x, viewport.innerWidth - box.width - 4))}px`;
    this.element.style.top = `${Math.max(4, Math.min(y, viewport.innerHeight - box.height - 4))}px`;
    (this.buttons.find((button) => !button.disabled) || this.element).focus();
  }
  close(restoreFocus = true) {
    if (this.element.hidden) return;
    this.element.hidden = true;
    this.target = null;
    if (restoreFocus) this.returnFocus?.focus();
  }
}
