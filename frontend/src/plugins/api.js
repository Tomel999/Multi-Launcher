import { registerSlot, unregisterSlot, unregisterAllSlots } from "./slots";
import { setThemeTokens, resetThemeTokens } from "./theme";
import { setView, removeViews, addMenuItem, removeMenuItems, setOverride, removeOverrides } from "./registry";
import { openDialog } from "./dialog";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { PluginSend, PluginFetch } from "../../wailsjs/go/main/App";
const uiUnsubs = /* @__PURE__ */ new Map();
const openDialogs = /* @__PURE__ */ new Map();
function requirePerm(manifest, perm, method) {
  if (!manifest.permissions || !manifest.permissions.includes(perm)) {
    throw new Error(`Plugin "${manifest.id}" needs permission "${perm}" for ${method}`);
  }
}
export function createPluginAPI(id, manifest, settings = {}) {
  const api = {
    theme: {
      set(tokens) {
        requirePerm(manifest, "ui:theme", "theme.set");
        setThemeTokens(id, tokens);
      }
    },
    injectCSS(css) {
      requirePerm(manifest, "ui:theme", "injectCSS");
      const style = document.createElement("style");
      style.id = `ml-plugin-css-${id}`;
      style.textContent = css;
      document.head.appendChild(style);
    },
    registerSlot(name, render) {
      requirePerm(manifest, "ui:slots", "registerSlot");
      registerSlot(name, id, render);
      return () => unregisterSlot(name, id);
    },
    notify(message) {
      requirePerm(manifest, "ui:notifications", "notify");
      const el = document.createElement("div");
      Object.assign(el.style, {
        position: "fixed",
        bottom: "20px",
        right: "20px",
        zIndex: 9999,
        background: "var(--color-surface)",
        color: "var(--color-text)",
        border: "1px solid var(--color-border)",
        borderRadius: "8px",
        padding: "10px 16px",
        fontSize: "13px",
        boxShadow: "0 4px 12px rgba(0,0,0,.4)"
      });
      el.textContent = message;
      document.body.appendChild(el);
      setTimeout(() => el.remove(), 4e3);
    },
    ui: {
      on(name, cb) {
        const off = EventsOn(`plugin:${id}:${name}`, cb);
        let offs = uiUnsubs.get(id);
        if (!offs) {
          offs = [];
          uiUnsubs.set(id, offs);
        }
        offs.push(off);
        return off;
      },
      send(name, data) {
        return PluginSend(id, name, data ?? null);
      }
    },
    registerView(view) {
      requirePerm(manifest, "ui:views", "registerView");
      setView(id, view);
    },
    registerMenuItem(target, item) {
      requirePerm(manifest, "ui:slots", "registerMenuItem");
      addMenuItem(target, { ...item, pluginId: id });
    },
    dialog(opts) {
      requirePerm(manifest, "ui:slots", "dialog");
      const dlg = openDialog(opts);
      let arr = openDialogs.get(id);
      if (!arr) {
        arr = [];
        openDialogs.set(id, arr);
      }
      arr.push(dlg);
      return dlg;
    },
    setOverride(key, mode) {
      requirePerm(manifest, "ui:override", "setOverride");
      setOverride(id, key, mode);
    },
        async fetch(url, opts = {}) {
      requirePerm(manifest, "network:custom", "fetch");
      return await PluginFetch(id, opts.method || "GET", url, opts.body ?? "");
    }
  };
  api.settings = settings || {};
  return api;
}
export function unloadPlugin(id) {
  document.getElementById(`ml-plugin-css-${id}`)?.remove();
  resetThemeTokens(id);
  unregisterAllSlots(id);
  removeViews(id);
  removeMenuItems(id);
  removeOverrides(id);
  for (const dlg of openDialogs.get(id) || []) dlg.close();
  openDialogs.delete(id);
  for (const off of uiUnsubs.get(id) || []) off();
  uiUnsubs.delete(id);
}
