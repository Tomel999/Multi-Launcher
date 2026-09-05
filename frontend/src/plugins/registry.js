const views = /* @__PURE__ */ new Map();
const menuItems = /* @__PURE__ */ new Map();
const overrides = /* @__PURE__ */ new Map();
const listeners = /* @__PURE__ */ new Set();
function bump() {
  for (const l of listeners) l();
}
export function subscribe(l) {
  listeners.add(l);
  return () => listeners.delete(l);
}
export function setView(pluginId, view) {
  if (!view || !view.id || typeof view.render !== "function") {
    throw new Error(`registerView: id and render(container) are required`);
  }
  views.set(view.id, { ...view, pluginId });
  bump();
}
export function getView(id) {
  return views.get(id);
}
export function removeViews(pluginId) {
  let changed = false;
  for (const [k, v] of views) {
    if (v.pluginId === pluginId) {
      views.delete(k);
      changed = true;
    }
  }
  if (changed) bump();
}
export function pluginSidebarViews(coreIds) {
  const out = [];
  for (const [id, v] of views) {
    if (!coreIds.includes(id)) out.push({ id, label: v.label || id, icon: v.icon || "ph-puzzle-piece" });
  }
  return out;
}
export function addMenuItem(target, item) {
  if (!item || !item.label || typeof item.onClick !== "function") {
    throw new Error(`registerMenuItem: label and onClick(ctx) are required`);
  }
  if (!menuItems.has(target)) menuItems.set(target, []);
  const arr = menuItems.get(target);
  const id = `${item.pluginId}/${item.label}`;
  const filtered = arr.filter((x) => x.id !== id);
  filtered.push({ ...item, id });
  menuItems.set(target, filtered);
  bump();
}
export function getMenuItems(target) {
  return menuItems.get(target) || [];
}
export function removeMenuItems(pluginId) {
  let changed = false;
  for (const [k, arr] of menuItems) {
    const filtered = arr.filter((x) => x.pluginId !== pluginId);
    if (filtered.length !== arr.length) {
      menuItems.set(k, filtered);
      changed = true;
    }
  }
  if (changed) bump();
}
const PROTECTED_VIEWS = /* @__PURE__ */ new Set(["home", "instances", "accounts", "skins", "plugins", "settings"]);
export function setOverride(pluginId, key, mode) {
  if (mode !== "hide" && mode !== "replace") {
    throw new Error(`setOverride: mode must be "hide" or "replace"`);
  }
  if (key.startsWith("view:") && PROTECTED_VIEWS.has(key.slice(5))) {
    throw new Error(`setOverride: "${key.slice(5)}" is a core view and cannot be overridden`);
  }
  overrides.set(key, { pluginId, mode });
  bump();
}
export function getOverride(key) {
  return overrides.get(key);
}
export function removeOverrides(pluginId) {
  let changed = false;
  for (const [k, v] of overrides) {
    if (v.pluginId === pluginId) {
      overrides.delete(k);
      changed = true;
    }
  }
  if (changed) bump();
}
