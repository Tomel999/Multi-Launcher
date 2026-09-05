const pluginTokens = /* @__PURE__ */ new Map();
export function setThemeTokens(id, tokens) {
  const root = document.documentElement;
  const prev = pluginTokens.get(id) || {};
  for (const [key, value] of Object.entries(tokens)) {
    root.style.setProperty(key, value);
    prev[key] = value;
  }
  pluginTokens.set(id, prev);
}
export function resetThemeTokens(id) {
  const tokens = pluginTokens.get(id);
  if (!tokens) return;
  const root = document.documentElement;
  for (const key of Object.keys(tokens)) {
    root.style.removeProperty(key);
  }
  pluginTokens.delete(id);
}
