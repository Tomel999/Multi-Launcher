import { useEffect, useState } from "preact/hooks";
import { syncPlugins } from "./plugins/loader";
import { PERM_LABELS } from "./plugins/perms";
import { InstallModal } from "./plugins/InstallModal";
import {
  GetEnabledPlugins,
  GetPluginPermissions,
  GetPluginCommands,
  GetPluginSettings,
  SavePluginSettings,
  RunPluginCommand,
  InspectPluginZip,
  InstallPluginFromZip,
  InstallPluginFromURL,
  SetPluginEnabled,
  RemovePlugin,
  PickPluginFile,
  FetchVerifiedPlugins
} from "../wailsjs/go/main/App";

export function PluginsPanel() {
  const [plugins, setPlugins] = useState([]);
  const [perms, setPerms] = useState([]);
  const [commands, setCommands] = useState({});
  const [cmdResult, setCmdResult] = useState("");
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [pending, setPending] = useState(null);
  const [confirming, setConfirming] = useState(null);
  const [configuring, setConfiguring] = useState(null);
  const [settingsDraft, setSettingsDraft] = useState({});
  const [browse, setBrowse] = useState(false);
  const [catalog, setCatalog] = useState(null);
  const [dirBusy, setDirBusy] = useState(false);
  const [installingId, setInstallingId] = useState("");
  const refresh = () => {
    GetEnabledPlugins().then(setPlugins).catch((e) => setError(String(e)));
    GetPluginCommands().then(setCommands).catch(() => {
    });
  };
  useEffect(() => {
    GetPluginPermissions().then(setPerms).catch(() => {
    });
    refresh();
  }, []);
  const pickFile = async () => {
    setError("");
    const path = await PickPluginFile();
    if (!path) return;
    try {
      const manifest = await InspectPluginZip(path);
      setPending({ path, manifest });
    } catch (e) {
      setError(String(e));
    }
  };
  const installUrl = async () => {
    setError("");
    if (!url.trim()) return;
    setBusy(true);
    try {
      await InstallPluginFromURL(url.trim());
      setUrl("");
      refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };
  const confirmInstall = async () => {
    setBusy(true);
    try {
      await InstallPluginFromZip(pending.path);
      setPending(null);
      refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };
  const toggle = async (p) => {
    try {
      await SetPluginEnabled(p.manifest.id, !p.enabled);
      refresh();
      syncPlugins();
    } catch (e) {
      setError(String(e));
    }
  };
  const remove = async (p) => {
    if (confirming !== p.manifest.id) {
      setConfirming(p.manifest.id);
      setTimeout(() => setConfirming((c) => c === p.manifest.id ? null : c), 3e3);
      return;
    }
    setConfirming(null);
    try {
      await RemovePlugin(p.manifest.id);
      refresh();
      syncPlugins();
    } catch (e) {
      setError(String(e));
    }
  };
  const runCommand = async (id, name) => {
    setError("");
    setCmdResult("");
    try {
      const res = await RunPluginCommand(id, name);
      setCmdResult(`[${id}] ${name} \u2192 ${res === void 0 ? "ok" : JSON.stringify(res)}`);
    } catch (e) {
      setError(String(e));
    }
  };
  const openConfig = async (p) => {
    if (configuring === p.manifest.id) {
      setConfiguring(null);
      return;
    }
    try {
      const saved = await GetPluginSettings(p.manifest.id);
      setSettingsDraft(saved || {});
      setConfiguring(p.manifest.id);
    } catch (e) {
      setError(String(e));
    }
  };
  const saveConfig = async (p) => {
    try {
      await SavePluginSettings(p.manifest.id, JSON.stringify(settingsDraft));
      setConfiguring(null);
      syncPlugins();
    } catch (e) {
      setError(String(e));
    }
  };
  const setField = (key, value) => setSettingsDraft((d) => ({ ...d, [key]: value }));
  const loadDir = async () => {
    setError("");
    setDirBusy(true);
    try {
      const cat = await FetchVerifiedPlugins();
      setCatalog(cat);
    } catch (e) {
      setError(String(e));
    } finally {
      setDirBusy(false);
    }
  };
  const installVerified = async (p) => {
    setError("");
    setInstallingId(p.id);
    try {
      await InstallPluginFromURL(p.downloadUrl);
      refresh();
      syncPlugins();
    } catch (e) {
      setError(String(e));
    } finally {
      setInstallingId("");
    }
  };
  const installedIDs = new Set((plugins || []).map((p) => p.manifest.id));
  const byCat = {};
  for (const p of plugins || []) (byCat[p.manifest.category] ||= []).push(p);
  const catNames = Object.keys(byCat).sort();
  const commandEntries = Object.entries(commands).flatMap(
    ([id, names]) => (names || []).map((name) => ({ id, name }))
  );
  return <div key="plugins" className="settings-view view-enter">
            <div className="topbar">
                <span className="topbar-title">Plugins</span>
                <button
    className="topbar-btn"
    style={{ marginLeft: "auto" }}
    onClick={() => {
      setBrowse(true);
      if (!catalog && !dirBusy) loadDir();
    }}
  >
                    <i className="ph ph-seal-check" />
                    <span>Verified Plugins</span>
                </button>
            </div>
            <div className="plugins-content">
                <div className="plugins-install">
                    <button className="topbar-btn" onClick={pickFile} disabled={busy}>
                        <i className="ph ph-file-arrow-up" />
                        <span>Install from file</span>
                    </button>
                    <form className="inst-search" style={{ maxWidth: "360px", flex: "1 1 auto" }} onSubmit={(e) => {
    e.preventDefault();
    installUrl();
  }}>
                        <input
    value={url}
    onInput={(e) => setUrl(e.target.value)}
    placeholder="https://…/plugin.mlplugin"
    disabled={busy}
  />
                        <button className="topbar-btn" type="submit" disabled={busy || !url.trim()}>
                            <i className="ph ph-download-simple" />
                            <span>Install</span>
                        </button>
                    </form>
                </div>
                {error && <div className="plugins-error">{error}</div>}
                {!plugins?.length && !error && <div className="plugins-empty">
                        <i className="ph ph-plugs plugins-empty-icon" />
                        <div className="plugins-empty-title">No plugins installed</div>
                        <div className="plugins-empty-sub">Install from file or paste a URL above.</div>
                    </div>}
                <div className="plugins-list">
                    {catNames.map((cat) => <div key={cat} className="plugins-cat">
                            <div className="plugins-cat-header">{cat}</div>
                            {byCat[cat].map((p) => <div key={p.manifest.id} className="plugin-card">
                            <div className={`plugin-card-status${p.enabled ? " on" : ""}`} />
                            <div className="plugin-card-body">
                                <div className="plugin-card-head">
                                    <span className="plugin-card-name">{p.manifest.name}</span>
                                    <span className="plugin-card-version">v{p.manifest.version}</span>
                                    {p.manifest.author && <span className="plugin-card-author">by {p.manifest.author}</span>}
                                </div>
                                {p.manifest.description && <div className="plugin-card-desc">{p.manifest.description}</div>}
                                {p.manifest.permissions.length > 0 && <div className="plugin-card-perms">
                                        {p.manifest.permissions.map((perm) => <span key={perm} className="plugin-perm" title={PERM_LABELS[perm] || perm}>
                                                {PERM_LABELS[perm] || perm}
                                            </span>)}
                                    </div>}
                            </div>
                            <div className="plugin-card-actions">
                                {p.manifest.settings?.length > 0 && p.enabled && <button
    className={`topbar-btn${configuring === p.manifest.id ? " active" : ""}`}
    onClick={() => openConfig(p)}
  >
                                        <i className="ph ph-sliders" />
                                        <span>Configure</span>
                                    </button>}
                                <button
    className={`topbar-btn${p.enabled ? " active" : ""}`}
    onClick={() => toggle(p)}
  >
                                    <i className={`ph ph-${p.enabled ? "toggle-right" : "toggle-left"}`} />
                                    <span>{p.enabled ? "On" : "Off"}</span>
                                </button>
                                <button className="topbar-btn danger" onClick={() => remove(p)}>
                                    <i className="ph ph-trash" />
                                    <span>{confirming === p.manifest.id ? "Confirm?" : "Remove"}</span>
                                </button>
                            </div>
                            {configuring === p.manifest.id && <div style={{ width: "100%", display: "flex", flexDirection: "column", gap: 8, padding: "8px 12px", borderTop: "1px solid var(--color-border)" }}>
                                    {p.manifest.settings.map((f) => <label key={f.key} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 10, fontSize: 13, color: "var(--color-text)" }}>
                                            <span>{f.label}</span>
                                            {f.type === "bool" ? <input type="checkbox" checked={!!settingsDraft[f.key]} onChange={(e) => setField(f.key, e.target.checked)} /> : f.type === "select" ? <select value={settingsDraft[f.key] ?? ""} onChange={(e) => setField(f.key, e.target.value)}>
                                                    {f.options.map((o) => <option key={o} value={o}>{o}</option>)}
                                                </select> : <input
    type={f.type === "number" ? "number" : "text"}
    value={settingsDraft[f.key] ?? ""}
    onChange={(e) => setField(f.key, f.type === "number" ? Number(e.target.value) : e.target.value)}
    style={{ maxWidth: 180 }}
  />}
                                        </label>)}
                                    <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                                        <button className="modal-btn" onClick={() => setConfiguring(null)}>Cancel</button>
                                        <button className="modal-btn primary" onClick={() => saveConfig(p)}>Save</button>
                                    </div>
                                </div>}
                        </div>)}
                    </div>)}
                </div>
                {commandEntries.length > 0 && <div className="plugins-commands">
                        {commandEntries.map(({ id, name }) => <div key={`${id}/${name}`} className="plugin-card" style={{ padding: "8px 12px" }}>
                                <div className="plugin-card-head">
                                    <span className="plugin-card-name">{id}</span>
                                    <span className="plugin-card-version">› {name}</span>
                                </div>
                                <div className="plugin-card-actions">
                                    <button className="topbar-btn" onClick={() => runCommand(id, name)}>
                                        <i className="ph ph-play" />
                                        <span>Run</span>
                                    </button>
                                </div>
                            </div>)}
                        {cmdResult && <div style={{ fontSize: 13, color: "var(--color-text-dim)", marginTop: 6 }}>{cmdResult}</div>}
                    </div>}
            </div>
            <InstallModal pending={pending} busy={busy} onConfirm={confirmInstall} onCancel={() => setPending(null)} />
            {browse && <div className="modal-overlay" onClick={() => setBrowse(false)}>
                    <div className="modal catalog-modal" onClick={(e) => e.stopPropagation()}>
                        <div className="catalog-head">
                            <span className="catalog-title"><i className="ph ph-seal-check" /> Verified Plugins</span>
                            <button className="topbar-btn" onClick={() => setBrowse(false)}>
                                <i className="ph ph-x" />
                            </button>
                        </div>
                        <div className="catalog-list">
                            {catalog && Object.keys(catalog.categories).length === 0 && <div className="plugins-empty-sub">No plugins found under plugins/ in {catalog.repo}.</div>}
                            {catalog && Object.keys(catalog.categories).sort().map((cat) => <div key={cat} className="plugins-cat">
                                    <div className="plugins-cat-header">{cat}</div>
                                    {(catalog.categories[cat] || []).map((p) => <div key={p.id} className="plugin-card">
                                            <div className="plugin-card-body">
                                                <div className="plugin-card-head">
                                                    <span className="plugin-card-name">{p.name}</span>
                                                    <span className="plugin-card-version">v{p.version}</span>
                                                    {p.author && <span className="plugin-card-author">by {p.author}</span>}
                                                </div>
                                                {p.description && <div className="plugin-card-desc">{p.description}</div>}
                                            </div>
                                            <div className="plugin-card-actions">
                                                <button
    className="topbar-btn"
    onClick={() => installVerified(p)}
    disabled={installedIDs.has(p.id) || installingId === p.id}
  >
                                                    <i className="ph ph-download-simple" />
                                                    <span>{installedIDs.has(p.id) ? "Installed" : installingId === p.id ? "Installing…" : "Download"}</span>
                                                </button>
                                            </div>
                                        </div>)}
                                </div>)}
                            {catalog?.skipped?.length > 0 && <div className="plugins-empty-sub">Skipped (invalid manifest): {catalog.skipped.join(", ")}</div>}
                        </div>
                    </div>
                </div>}
        </div>;
}
