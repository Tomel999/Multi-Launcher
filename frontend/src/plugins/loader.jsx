import { GetEnabledPlugins, GetPluginSettings } from "../../wailsjs/go/main/App";
import { createPluginAPI, unloadPlugin } from "./api";
import { setView } from "./registry";
export { unloadPlugin };
const loaded = /* @__PURE__ */ new Set();
export async function syncPlugins() {
  let plugins;
  try {
    plugins = await GetEnabledPlugins();
  } catch (e) {
    console.error("Failed to fetch plugins:", e);
    return;
  }
  const enabled = new Set(
    plugins.filter((p) => p.enabled && p.manifest && p.manifest.frontend).map((p) => p.manifest.id)
  );
  for (const id of loaded) {
    if (!enabled.has(id)) {
      unloadPlugin(id);
      loaded.delete(id);
    }
  }
  for (const p of plugins) {
    if (!p.enabled || !p.manifest || !p.manifest.frontend || loaded.has(p.manifest.id)) continue;
    try {
      const settings = await GetPluginSettings(p.manifest.id).catch(() => ({}));
      const mod = await import(
        /* @vite-ignore */
        `/plugins/${p.manifest.id}/${p.manifest.frontend}`
      );
      if (mod.default && typeof mod.default.onLoad === "function") {
        mod.default.onLoad(createPluginAPI(p.manifest.id, p.manifest, settings));
      }
      if (mod.default && Array.isArray(mod.default.views)) {
        for (const v of mod.default.views) setView(p.manifest.id, v);
      }
      loaded.add(p.manifest.id);
    } catch (e) {
      console.error(`Plugin ${p.manifest.id} failed to load:`, e);
    }
  }
}
