import "./app.css";
import { useEffect, useRef, useState } from "preact/hooks";
import { HttpGet, CurseForgeSearch, CurseForgeCategories, CurseForgeFiles, GetPanoramaVersions, LaunchInstance, LaunchClientInstance, Clients, ClientVersions, StopInstance, IsGameRunning, GetProgress, GetState, GetLogs, SetDownloadConcurrency, GetSystemMemory, CancelInstall, GetPersistedState, SaveState, DeleteInstance, DeleteLunarFolder, DeleteFeatherFolder, DeleteDawnFolder, InstallModpack, GetAccounts, GetActiveAccount, AddOfflineAccount, AddMicrosoftAccount, PollMicrosoftLogin, SetActiveAccount, DeleteAccount, UploadSkin, ListLocalSkins, DeleteLocalSkin, PickJavaFile, TestJavaPath, ListContent, DownloadContent, InspectMod, OpenInstanceFolder, DuplicateInstance, ExportInstance, ListWorlds, GetWorldInfo, GetItemIcon, OpenWorldFolder, DuplicateWorld, RenameWorld, DeleteWorld, ExportWorld, ImportWorld, ListScreenshots, GetScreenshot, DeleteScreenshot, CopyScreenshotToClipboard, OpenScreenshotsFolder, InstallPluginFromZip, DiskUsage, CleanCache, InstanceSize, CreateInstance, PruneOrphanedVersions } from "../wailsjs/go/main/App";
import { EventsOn, WindowMinimise, WindowUnminimise, Quit, BrowserOpenURL } from "../wailsjs/runtime/runtime";
import { PanoramaBackground } from "./PanoramaBackground";
import { SkinViewer3D } from "./SkinViewer3D";
import { Slot } from "./plugins/slots";
import { syncPlugins } from "./plugins/loader";
import { InstallModal } from "./plugins/InstallModal";
import { UpdateModal } from "./UpdateModal";
import { subscribe as subscribeRegistry, getView, getOverride, pluginSidebarViews, getMenuItems } from "./plugins/registry";
import { PluginsPanel } from "./PluginsPanel";
import grassIcon from "./assets/grassblock.png";
import fabricIcon from "./assets/fabricmc.svg";
import forgeIcon from "./assets/forge.png";
import quiltIcon from "./assets/quiltmc.svg";
import neoforgeIcon from "./assets/neoforged.svg";
import lunarIcon from "./assets/lunar.png";
import featherIcon from "./assets/feather.png";
import dawnIcon from "./assets/dawn.png";
import ogulniegaIcon from "./assets/ogulniega.png";
const icons = [
  { icon: <i className="ph ph-house" />, label: "Home", id: "home" },
  { icon: <i className="ph ph-cube" />, label: "Instances", id: "instances" },
  { icon: <i className="ph ph-user" />, label: "Accounts", id: "accounts" },
  { icon: <i className="ph ph-t-shirt" />, label: "Skin", id: "skins" },
  { icon: <i className="ph ph-plugs" />, label: "Plugins", id: "plugins" },
  { icon: <i className="ph ph-gear" />, label: "Settings", id: "settings" }
];
const PluginViewHost = ({ view }) => {
  const ref = useRef(null);
  useEffect(() => {
    const host = document.createElement("div");
    host.className = "settings-view view-enter";
    ref.current.appendChild(host);
    let cleanup;
    try {
      cleanup = view.render(host);
    } catch (e) {
      console.error(`plugin view "${view.id}" failed:`, e);
    }
    return () => {
      if (typeof cleanup === "function") cleanup();
      host.remove();
    };
  }, [view]);
  return <div ref={ref} style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0 }} />;
};
const MsLogo = ({ size = 24 }) => <svg width={size} height={size} viewBox="0 0 23 23" style={{ flex: "none" }} aria-label="Microsoft">
        <path fill="#F25022" d="M0 0h11v11H0z" />
        <path fill="#7FBA00" d="M12 0h11v11H12z" />
        <path fill="#00A4EF" d="M0 12h11v11H0z" />
        <path fill="#FFB900" d="M12 12h11v11H12z" />
    </svg>;
const STEVE_UUID = "c06f89064c8a49119c29ea1dbd1aab82";
const AccountAvatar = ({ acc }) => {
  const [err, setErr] = useState(false);
  if (err) {
    return <div className="acc-avatar">{acc.name.slice(0, 1).toUpperCase()}</div>;
  }
  const uuid = acc.type === "microsoft" ? acc.id : STEVE_UUID;
  return <img
    className="acc-avatar"
    src={`https://minotar.net/helm/${uuid}/64`}
    alt=""
    onError={() => setErr(true)}
  />;
};
const loaders = ["Vanilla", "Fabric", "Forge", "Quilt", "NeoForge"];
const loaderIcon = {
  Vanilla: grassIcon,
  Fabric: fabricIcon,
  Forge: forgeIcon,
  Quilt: quiltIcon,
  NeoForge: neoforgeIcon
};
const clientIcon = {
  lunar: lunarIcon,
  feather: featherIcon,
  dawn: dawnIcon,
  ogulniega: ogulniegaIcon
};
const neoPrefix = (mc) => {
  const old = mc.match(/^1\.(\d+)(?:\.(\d+))?$/);
  if (old) return old[2] ? `${old[1]}.${old[2]}.` : `${old[1]}.0.`;
  const neu = mc.match(/^26\.(\d+)(?:\.(\d+))?$/);
  if (neu) return neu[2] ? `26.${neu[1]}.${neu[2]}.` : `26.${neu[1]}.0.`;
  return "";
};
const cfSortFields = { Featured: "1", Popularity: "2", "Last Updated": "3", Name: "4", "Total Downloads": "6" };
const uid = () => Math.random().toString(36).slice(2) + Date.now().toString(36);
const loaderVersionCache = /* @__PURE__ */ new Map();
const fetchLoaderVersions = (loader, mcVersion) => {
  const key = `${loader}:${mcVersion}`;
  if (loaderVersionCache.has(key)) return loaderVersionCache.get(key);
  let url;
  if (loader === "Fabric") url = `https://meta.fabricmc.net/v2/versions/loader/${mcVersion}`;
  else if (loader === "Quilt") url = `https://meta.quiltmc.org/v3/versions/loader/${mcVersion}`;
  else if (loader === "Forge") url = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json";
  else if (loader === "NeoForge") url = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml";
  const p = HttpGet(url).then((body) => {
    let versions = [];
    if (loader === "Fabric" || loader === "Quilt") {
      const data = JSON.parse(body);
      versions = data.map((v) => v.loader.version);
    } else if (loader === "Forge") {
      const data = JSON.parse(body);
      versions = [data.promos[`${mcVersion}-recommended`], data.promos[`${mcVersion}-latest`]].filter(Boolean).filter((v, i, a) => a.indexOf(v) === i);
    } else if (loader === "NeoForge") {
      const prefix = neoPrefix(mcVersion);
      if (!prefix) return versions;
      const matches = body.match(/<version>([^<]+)<\/version>/g) || [];
      versions = matches.map((m) => m.replace(/<\/?version>/g, "")).filter((v) => v.startsWith(prefix));
    }
    versions.sort((a, b) => b.localeCompare(a, void 0, { numeric: true }));
    return versions;
  }).catch(() => []);
  loaderVersionCache.set(key, p);
  return p;
};
const ModrinthIcon = () => <svg width="16" height="16" viewBox="0 0 24 24" aria-label="Modrinth">
        <path fill="#1bd96a" d="M12.252.004a11.78 11.768 0 0 0-8.92 3.73 11 10.999 0 0 0-2.17 3.11 11.37 11.359 0 0 0-1.16 5.169c0 1.42.17 2.5.6 3.77.24.759.77 1.899 1.17 2.529a12.3 12.298 0 0 0 8.85 5.639c.44.05 2.54.07 2.76.02.2-.04.22.1-.26-1.7l-.36-1.37-1.01-.06a8.5 8.489 0 0 1-5.18-1.8 5.34 5.34 0 0 1-1.3-1.26c0-.05.34-.28.74-.5a37.572 37.545 0 0 1 2.88-1.629c.03 0 .5.45 1.06.98l1 .97 2.07-.43 2.06-.43 1.47-1.47c.8-.8 1.48-1.5 1.48-1.52 0-.09-.42-1.63-.46-1.7-.04-.06-.2-.03-1.02.18-.53.13-1.2.3-1.45.4l-.48.15-.53.53-.53.53-.93.1-.93.07-.52-.5a2.7 2.7 0 0 1-.96-1.7l-.13-.6.43-.57c.68-.9.68-.9 1.46-1.1.4-.1.65-.2.83-.33.13-.099.65-.579 1.14-1.069l.9-.9-.7-.7-.7-.7-1.95.54c-1.07.3-1.96.53-1.97.53-.03 0-2.23 2.48-2.63 2.97l-.29.35.28 1.03c.16.56.3 1.16.31 1.34l.03.3-.34.23c-.37.23-2.22 1.3-2.84 1.63-.36.2-.37.2-.44.1-.08-.1-.23-.6-.32-1.03-.18-.86-.17-2.75.02-3.73a8.84 8.839 0 0 1 7.9-6.93c.43-.03.77-.08.78-.1.06-.17.5-2.999.47-3.039-.01-.02-.1-.02-.2-.03Zm3.68.67c-.2 0-.3.1-.37.38-.06.23-.46 2.42-.46 2.52 0 .04.1.11.22.16a8.51 8.499 0 0 1 2.99 2 8.38 8.379 0 0 1 2.16 3.449 6.9 6.9 0 0 1 .4 2.8c0 1.07 0 1.27-.1 1.73a9.37 9.369 0 0 1-1.76 3.769c-.32.4-.98 1.06-1.37 1.38-.38.32-1.54 1.1-1.7 1.14-.1.03-.1.06-.07.26.03.18.64 2.56.7 2.78l.06.06a12.07 12.058 0 0 0 7.27-9.4c.13-.77.13-2.58 0-3.4a11.96 11.948 0 0 0-5.73-8.578c-.7-.42-2.05-1.06-2.25-1.06Z" />
    </svg>;
const CurseForgeIcon = () => <svg width="16" height="16" viewBox="0 0 24 24" aria-label="CurseForge">
        <path fill="#f16436" d="M18.326 9.2145S23.2261 8.4418 24 6.1882h-7.5066V4.4H0l2.0318 2.3576V9.173s5.1267-.2665 7.1098 1.2372c2.7146 2.516-3.053 5.917-3.053 5.917L5.0995 19.6c1.5465-1.4726 4.494-3.3775 9.8983-3.2857-2.0565.65-4.1245 1.6651-5.7344 3.2857h10.9248l-1.0288-3.2726s-7.918-4.6688-.8336-7.1127z" />
    </svg>;
const cfSorts = { Popularity: "2", "Last Updated": "3", Name: "4", "Total Downloads": "6" };
const escHtml = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
const mdToHtml = (md) => {
  if (!md) return "";
  const codeBlocks = [];
  let text = String(md).replace(/```[a-zA-Z0-9]*\r?\n([\s\S]*?)```/g, (_, code) => {
    codeBlocks.push(`<pre class="md-code"><code>${escHtml(code.replace(/\n$/, ""))}</code></pre>`);
    return `\u0000CB${codeBlocks.length - 1}\u0000`;
  });
  text = text.split(/(<\/?[a-zA-Z][^>]*>)/).map((part, i) =>
    i % 2 === 1 ? part : escHtml(part)
  ).join("");
  const inline = (s) => s
    .replace(/`([^`]+)`/g, '<code class="md-inline">$1</code>')
    .replace(/!\[([^\]]*)\]\((https?:[^)\s]+)\)/g, '<img class="md-img" src="$2" alt="$1" loading="lazy" />')
    .replace(/\[([^\]]+)\]\((https?:[^)\s]+)\)/g, '<a class="md-link" href="$2" target="_blank" rel="noopener noreferrer">$1</a>')
    .replace(/\*\*\*([^*]+)\*\*\*/g, "<strong><em>$1</em></strong>")
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
    .replace(/(^|[^*\w])\*([^*\n]+)\*/g, "$1<em>$2</em>")
    .replace(/__([^_]+)__/g, "<strong>$1</strong>")
    .replace(/~~([^~]+)~~/g, "<del>$1</del>");
  const lines = text.split("\n");
  const out = [];
  let inUl = false, inOl = false, para = [];
  const closeLists = () => { if (inUl) { out.push("</ul>"); inUl = false; } if (inOl) { out.push("</ol>"); inOl = false; } };
  const closePara = () => { if (para.length) { out.push(`<p>${inline(para.join(" "))}</p>`); para = []; } };
  for (const raw of lines) {
    const line = raw.replace(/\s+$/, "");
    const cb = line.trim().match(/^\u0000CB(\d+)\u0000$/);
    if (cb) { closePara(); closeLists(); out.push(codeBlocks[parseInt(cb[1], 10)]); continue; }
    if (!line.trim()) { closePara(); closeLists(); continue; }
    const h = line.match(/^(#{1,6})\s+(.*)/);
    if (h) { closePara(); closeLists(); const lv = Math.min(h[1].length + 2, 6); out.push(`<h${lv} class="md-h">${inline(h[2])}</h${lv}>`); continue; }
    if (/^(-{3,}|\*{3,})$/.test(line.trim())) { closePara(); closeLists(); out.push('<hr class="md-hr" />'); continue; }
    if (/^>\s?/.test(line)) { closePara(); closeLists(); out.push(`<blockquote class="md-quote">${inline(line.replace(/^>\s?/, ""))}</blockquote>`); continue; }
    const ul = line.match(/^[-*+]\s+(.*)/);
    if (ul) { closePara(); if (!inUl) { closeLists(); out.push('<ul class="md-list">'); inUl = true; } out.push(`<li>${inline(ul[1])}</li>`); continue; }
    const ol = line.match(/^\d+[.)]\s+(.*)/);
    if (ol) { closePara(); if (!inOl) { closeLists(); out.push('<ol class="md-list">'); inOl = true; } out.push(`<li>${inline(ol[1])}</li>`); continue; }
    // Lines containing HTML tags are passed through raw
    if (/<[a-zA-Z]/.test(line)) { closePara(); closeLists(); out.push(inline(line)); continue; }
    para.push(line.trim());
  }
  closePara();
  closeLists();
  return out.join("\n");
};
const ContentTab = ({ instName, subfolder, facet, searchable, mcVersion, instLoader }) => {
  const addLabel = { mod: "Add mods", resourcepack: "Add resourcepacks", shaderpack: "Add shaders" }[facet] || "Add mods";
  const browseLabel = { mod: "Browse mods", resourcepack: "Browse resourcepacks", shaderpack: "Browse shaders" }[facet] || "Browse mods";
  const [showBrowse, setShowBrowse] = useState(false);
  const [source, setSource] = useState("modrinth");
  const [query, setQuery] = useState("");
  const [version, setVersion] = useState(mcVersion || "");
  const [sort, setSort] = useState("relevance");
  const [cats, setCats] = useState([]);
  const [mrCategories, setMrCategories] = useState([]);
  const [cfCategories, setCfCategories] = useState([]);
  const [page, setPage] = useState(1);
  const [results, setResults] = useState([]);
  const [loading, setLoading] = useState(false);
  const [downloading, setDownloading] = useState("");
  const [installed, setInstalled] = useState([]);
  const [instMeta, setInstMeta] = useState({});
  const [msg, setMsg] = useState("");
  const [selected, setSelected] = useState(null);
  const [versions, setVersions] = useState([]);
  const [selVersion, setSelVersion] = useState(0);
  const [detail, setDetail] = useState("");
  const [loadingVers, setLoadingVers] = useState(false);
  const [instPage, setInstPage] = useState(1);
  const [instQuery, setInstQuery] = useState("");
  const refreshInstalled = () => ListContent(instName, subfolder).then((list) => {
    setInstalled(list);
    const mrType = { mods: "mod", resourcepacks: "resourcepack", shaderpacks: "shader" }[subfolder] || "mod";
    const meta = {};
    list.forEach((p) => {
      const base = p.filename.replace(/\.disabled$/i, "");
      InspectMod(instName, subfolder, p.filename).then((m) => {
        meta[base] = m;
        setInstMeta({ ...meta });
        if (!m.icon) {
          const name = (m.name || p.filename).replace(/\.(zip|jar)$/i, "").replace(/[-_][a-z]?\d[\d.]*$/i, "").replace(/([a-z])([A-Z])/g, "$1 $2");
          HttpGet(`https://api.modrinth.com/v2/search?query=${encodeURIComponent(name)}&limit=1&facets=${encodeURIComponent(`[["project_type:${mrType}"]]`)}`).then((body) => {
            const hit = (JSON.parse(body).hits || [])[0];
            if (hit?.icon_url) {
              meta[base] = { ...m, icon: hit.icon_url };
              setInstMeta({ ...meta });
            }
          }).catch(() => {
          });
        }
      }).catch(() => {
      });
    });
  }).catch(() => setInstalled([]));
  useEffect(() => {
    refreshInstalled();
  }, [instName, subfolder]);
  useEffect(() => {
    if (showBrowse) doSearch(1);
  }, [showBrowse, source, cats, sort]);
  useEffect(() => {
    const mrType = { mod: "mod", resourcepack: "resourcepack", shaderpack: "shader" }[facet] || facet;
    HttpGet("https://api.modrinth.com/v2/tag/category").then((body) => setMrCategories((JSON.parse(body) || []).filter((c) => c.project_type === mrType))).catch(() => {
    });
  }, [facet]);
  useEffect(() => {
    if (!showBrowse) return;
    const cfClass = { mod: 6, resourcepack: 12, shaderpack: 6552 }[facet] || 6;
    CurseForgeCategories(cfClass).then((body) => setCfCategories((JSON.parse(body).data || []).filter((c) => !c.isClass))).catch(() => {
    });
  }, [showBrowse, facet]);
  const search = (e) => {
    e?.preventDefault();
    setPage(1);
    doSearch(1);
  };
  const doSearch = (pg) => {
    setLoading(true);
    setResults([]);
    setSelected(null);
    setVersions([]);
    const mrType = { mod: "mod", resourcepack: "resourcepack", shaderpack: "shader" }[facet] || facet;
    const cfClass = { mod: 6, resourcepack: 12, shaderpack: 6552 }[facet] || 6;
    if (source === "modrinth") {
      const facets = [[`project_type:${mrType}`]];
      if (facet === "mod" && instLoader && instLoader !== "Vanilla") facets.push([`categories:${instLoader.toLowerCase()}`]);
      if (facet === "mod" && version) facets.push([`versions:${version}`]);
      if (cats.length) facets.push(cats.map((c) => `categories:${c}`));
      const offset = (pg - 1) * 20;
      const doMr = (fs) => HttpGet(`https://api.modrinth.com/v2/search?facets=${encodeURIComponent(JSON.stringify(fs))}&query=${encodeURIComponent(query.trim())}&limit=20&offset=${offset}&index=${sort}`).then((body) => JSON.parse(body).hits || []).catch(() => []);
      doMr(facets).then((hits) => {
        if (!hits.length && version) return doMr(facets.filter((f) => !f[0].startsWith("versions:")));
        return hits;
      }).then(setResults).finally(() => setLoading(false));
    } else {
      const cfLoader = facet === "mod" && instLoader && instLoader !== "Vanilla" ? { fabric: 4, forge: 1, quilt: 5, neoforge: 6 }[instLoader.toLowerCase()] || 0 : 0;
      const catIds = cats.map(Number).filter(Boolean);
      const fetchCat = (catId) => CurseForgeSearch(query.trim(), facet === "mod" ? version : "", cfSorts[sort] || "", catId ? 0 : (pg - 1) * 50, cfClass, catId, cfLoader).then((body) => JSON.parse(body).data || []).catch(() => []);
      Promise.all(catIds.length ? catIds.map(fetchCat) : [fetchCat(0)]).then((lists) => {
        const seen = /* @__PURE__ */ new Set();
        const merged = [];
        lists.flat().forEach((m) => {
          if (seen.has(m.id)) return;
          seen.add(m.id);
          merged.push(m);
        });
        setResults(merged.map((m) => ({
          project_id: String(m.id),
          title: m.name,
          description: m.summary || "",
          icon_url: m.logo?.url || "",
          _cf: true,
          _modId: String(m.id)
        })));
      }).finally(() => setLoading(false));
    }
  };
  const selectHit = async (hit) => {
    setSelected(hit);
    setSelVersion(0);
    setDetail(hit.description || "");
    setLoadingVers(true);
    try {
      if (hit._cf) {
        const body = await CurseForgeFiles(hit._modId);
        const files = (JSON.parse(body).data || []).filter((x) => x.downloadUrl);
        let list = files;
        if (facet === "mod" && version) {
          const m = list.filter((x) => (x.gameVersions || []).includes(version));
          if (m.length) list = m;
        }
        if (facet === "mod" && instLoader && instLoader !== "Vanilla") {
          list = list.filter((x) => (x.gameVersions || []).some((g) => g.toLowerCase() === instLoader.toLowerCase()));
        }
        setVersions(list.map((x) => ({
          id: x.id,
          label: `${x.displayName || x.fileName} (${(x.gameVersions || []).slice(0, 3).join(", ")})`,
          url: x.downloadUrl,
          filename: x.fileName
        })));
      } else {
        const [vb, pb] = await Promise.all([
          HttpGet(`https://api.modrinth.com/v2/project/${hit.project_id}/version`),
          HttpGet(`https://api.modrinth.com/v2/project/${hit.project_id}`)
        ]);
        let vl = JSON.parse(vb).filter((x) => x.files?.length);
        if (facet === "mod" && version) {
          const m = vl.filter((x) => x.game_versions.includes(version));
          if (m.length) vl = m;
        }
        if (facet === "mod" && instLoader && instLoader !== "Vanilla") {
          vl = vl.filter((x) => x.loaders.includes(instLoader.toLowerCase()));
        }
        setVersions(vl.map((x) => {
          const f = x.files.find((f2) => f2.primary) || x.files[0];
          return {
            id: x.id,
            label: `${x.version_number} (${x.game_versions.slice(0, 3).join(", ")}) \u2014 ${x.loaders.join(", ")}`,
            url: f.url,
            filename: f.filename
          };
        }));
        setDetail(JSON.parse(pb).body || hit.description || "");
      }
    } catch {
      setVersions([]);
    } finally {
      setLoadingVers(false);
    }
  };
  const download = async () => {
    const v = versions[selVersion];
    if (!v?.url) {
      setMsg("No downloadable file for this version.");
      return;
    }
    setDownloading(selected?.project_id);
    setMsg("");
    try {
      await DownloadContent(instName, subfolder, v.url, v.filename);
      setMsg(`Installed ${v.filename}`);
      refreshInstalled();
    } catch (err) {
      setMsg(String(err));
    } finally {
      setDownloading("");
    }
  };
  const switchSource = (s2) => {
    setSource(s2);
    setSort(s2 === "modrinth" ? "relevance" : "Popularity");
    setPage(1);
    setResults([]);
    setSelected(null);
    setVersions([]);
    setMsg("");
  };
  const PAGE_SIZE = 20;
  const q = instQuery.trim().toLowerCase();
  const filtered = installed.filter((p) => !q || (instMeta[p.filename.replace(/\.disabled$/i, "")]?.name || p.name).toLowerCase().includes(q) || p.filename.toLowerCase().includes(q)).sort((a, b) => {
    const na = (instMeta[a.filename.replace(/\.disabled$/i, "")]?.name || a.name).toLowerCase();
    const nb = (instMeta[b.filename.replace(/\.disabled$/i, "")]?.name || b.name).toLowerCase();
    return na.localeCompare(nb);
  });
  const enabledCount = installed.filter((p) => !p.disabled).length;
  const disabledCount = installed.length - enabledCount;
  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const curPage = Math.min(instPage, pageCount);
  const shown = filtered.slice((curPage - 1) * PAGE_SIZE, curPage * PAGE_SIZE);
  return <div className="ctab">
            {
  }
            <div className="ctab-toolbar">
                <div className="ctab-toolbar-left">
                    <span className="ctab-label">Installed</span>
                    {installed.length > 0 && <span className="ctab-count">{installed.length}</span>}
                </div>
                {installed.length > 0 && <div className="ctab-search">
                        <i className="ph ph-magnifying-glass ctab-search-icon" />
                        <input
    className="ctab-search-input"
    placeholder="Search installed..."
    value={instQuery}
    onInput={(e) => setInstQuery(e.target.value)}
  />
                        {instQuery && <button className="ctab-search-clear" onClick={() => setInstQuery("")} title="Clear search">
                                <i className="ph ph-x" />
                            </button>}
                    </div>}
                {searchable && <button className="ctab-browse-btn" onClick={() => {
    setMsg("");
    setShowBrowse(true);
  }}>
                        <i className="ph ph-plus-circle" />
                        {addLabel}
                    </button>}
            </div>

            {
  }
            {installed.length > 0 && subfolder === "mods" && <div className="ctab-stats">
                    <span className="ctab-stat"><i className="ph ph-check-circle" /> Enabled: {enabledCount}</span>
                    <span className="ctab-stat muted"><i className="ph ph-minus-circle" /> Disabled: {disabledCount}</span>
                </div>}

            {
  }
            {installed.length === 0 ? <div className="ctab-empty">
                    <i className="ph ph-tray" />
                    <span className="ctab-empty-text">Nothing installed yet</span>
                    {searchable && <button className="ctab-empty-action" onClick={() => {
    setMsg("");
    setShowBrowse(true);
  }}>
                            <i className="ph ph-plus" /> {browseLabel}
                        </button>}
                  </div> : filtered.length === 0 ? <div className="ctab-empty">
                        <i className="ph ph-magnifying-glass" />
                        <span className="ctab-empty-text">No matches for "{instQuery}"</span>
                      </div> : <div className="ctab-installed">
                    {shown.map((p) => {
    const m = instMeta[p.filename.replace(/\.disabled$/i, "")] || {};
    const icon = m.icon ? m.icon.startsWith("http") ? m.icon : `data:image/png;base64,${m.icon}` : "";
    return <div key={p.filename.replace(/\.disabled$/i, "")} className={`ctab-file${p.disabled ? " disabled" : ""}`}>
                                {icon ? <img className="ctab-file-icon-img" src={icon} alt="" /> : <div className="ctab-file-icon"><i className="ph ph-puzzle-piece" /></div>}
                                <div className="ctab-file-body">
                                    <span className="ctab-file-name">{m.name || p.name}</span>
                                    <span className="ctab-file-sub">{p.filename}</span>
                                </div>
                                <div className="ctab-file-actions">
                                    {subfolder === "mods" && <button
      className={`ctab-switch${p.disabled ? " off" : " on"}`}
      role="switch"
      aria-checked={!p.disabled}
      title={p.disabled ? "Enable" : "Disable"}
      onClick={async (e) => {
        e.stopPropagation();
        setInstalled((prev) => prev.map((x) => {
          if (x.filename !== p.filename) return x;
          const disabled = !x.disabled;
          let filename = x.filename;
          if (disabled && !filename.toLowerCase().endsWith(".disabled")) filename += ".disabled";
          else if (!disabled && filename.toLowerCase().endsWith(".disabled")) filename = filename.slice(0, -".disabled".length);
          return { ...x, disabled, filename };
        }));
        await window.go.main.App.ToggleContent(instName, subfolder, p.filename, !p.disabled);
      }}
    >
                                            <span className="ctab-switch-knob" />
                                        </button>}
                                    <button
      className="ctab-file-action danger"
      title="Delete"
      onClick={async (e) => {
        e.stopPropagation();
        setInstalled((prev) => prev.filter((x) => x.filename !== p.filename));
        await window.go.main.App.DeleteContent(instName, subfolder, p.filename);
      }}
    >
                                        <i className="ph ph-trash" />
                                    </button>
                                </div>
                            </div>;
  })}
                  </div>}

            {
  }
            {pageCount > 1 && <div className="ctab-pager">
                    <button className="ctab-pager-btn" disabled={curPage === 1} onClick={() => setInstPage(curPage - 1)} title="Previous page">
                        <i className="ph ph-caret-left" />
                    </button>
                    <span className="ctab-pager-info">{curPage} / {pageCount}</span>
                    <button className="ctab-pager-btn" disabled={curPage === pageCount} onClick={() => setInstPage(curPage + 1)} title="Next page">
                        <i className="ph ph-caret-right" />
                    </button>
                </div>}

            {
  }
            {showBrowse && <div className="modal-overlay" onClick={() => {
    setShowBrowse(false);
    refreshInstalled();
  }}>
                    <div className="br-modal" onClick={(e) => e.stopPropagation()}>

                        {
  }
                        <div className="br-header">
                            <span className="br-header-title">Browse & download {facet}s</span>
                            <button className="br-close" onClick={() => {
    setShowBrowse(false);
    refreshInstalled();
  }} title="Close"><i className="ph ph-x" /></button>
                        </div>

                        {
  }
                        <form className="br-search-row" onSubmit={search}>
                            <div className="br-search-box">
                                <i className="ph ph-magnifying-glass br-search-icon" />
                                <input
    className="br-input"
    placeholder={`Search ${facet}s...`}
    value={query}
    onChange={(e) => setQuery(e.target.value)}
    autoFocus
  />
                            </div>
                            <button type="submit" className="br-btn-primary">Search</button>
                        </form>

                        {
  }
                        <div className="br-body">
                            <div className="br-list">
                                {loading && <div className="br-loading">
                                        <i className="ph ph-circle-notch br-spin" />
                                        <span>Searching...</span>
                                    </div>}
                                {!loading && results.map((hit) => <div
    key={hit.project_id}
    className={`br-item${selected?.project_id === hit.project_id ? " active" : ""}`}
    onClick={() => selectHit(hit)}
  >
                                        {hit.icon_url ? <img className="br-item-icon" src={hit.icon_url} alt="" /> : <div className="br-item-icon-ph"><i className="ph ph-puzzle-piece" /></div>}
                                        <div className="br-item-body">
                                            <span className="br-item-name">{hit.title}</span>
                                            <span className="br-item-desc">{hit.description}</span>
                                        </div>
                                        <i className="ph ph-caret-right br-item-arrow" />
                                    </div>)}
                                {!loading && !results.length && query.trim() && <div className="br-empty">
                                        <i className="ph ph-warning-circle" />
                                        <span>No results for "{query}"</span>
                                    </div>}
                                {!loading && !results.length && !query.trim() && <div className="br-empty">
                                        <i className="ph ph-magnifying-glass" />
                                        <span>No results found</span>
                                    </div>}
                            </div>

                            {selected ? <div className="br-detail">
                                    <div className="br-detail-head">
                                        <button className="br-back-btn" onClick={() => setSelected(null)} title="Back to filters">
                                            <i className="ph ph-arrow-left" />
                                        </button>
                                        {selected.icon_url && <img className="br-detail-icon" src={selected.icon_url} alt="" />}
                                        <div className="br-detail-info">
                                            <span className="br-detail-name">{selected.title}</span>
                                            <span className="br-detail-sub">{selected.description}</span>
                                        </div>
                                    </div>
                                    <div className="br-detail-body" dangerouslySetInnerHTML={{ __html: detail ? mdToHtml(detail) : "Loading description..." }} onClick={(e) => {
                                      const a = e.target.closest("a");
                                      if (a?.href) { e.preventDefault(); BrowserOpenURL(a.href); }
                                    }} />
                                    <div className="br-detail-footer">
                                        {msg && <span className="br-msg">{msg}</span>}
                                        <select
    className="br-select br-detail-ver"
    value={selVersion}
    onChange={(e) => setSelVersion(Number(e.target.value))}
  >
                                            {loadingVers ? <option value={0}>Loading versions...</option> : versions.map((v, i) => <option key={v.id} value={i}>{v.label}</option>)}
                                            {!loadingVers && !versions.length && <option value={0}>No versions available</option>}
                                        </select>
                                        <button
    className="br-btn-primary br-btn-install"
    disabled={!!downloading || loadingVers || !versions.length}
    onClick={download}
  >
                                            {downloading ? <><i className="ph ph-circle-notch br-spin" /> Installing...</> : <><i className="ph ph-download-simple" /> Install</>}
                                        </button>
                                    </div>
                                </div> : <div className="br-filters-pane">
                                    <div className="br-filters-head">
                                        <i className="ph ph-faders" /> Filters
                                    </div>
                                    <div className="br-filter-group">
                                        <label>Provider</label>
                                        <div className="br-sources">
                                            <button className={`br-src${source === "modrinth" ? " active" : ""}`} onClick={() => switchSource("modrinth")}>
                                                <ModrinthIcon /> Modrinth
                                            </button>
                                            <button className={`br-src${source === "curseforge" ? " active" : ""}`} onClick={() => switchSource("curseforge")}>
                                                <CurseForgeIcon /> CurseForge
                                            </button>
                                        </div>
                                    </div>
                                    <div className="br-filter-group">
                                        <label>Sort by</label>
                                        <div className="br-sources br-sort">
                                            {source === "modrinth" ? ["relevance", "downloads", "follows", "newest", "updated"].map((s2) => <button key={s2} className={`br-src${sort === s2 ? " active" : ""}`} onClick={() => setSort(s2)}>
                                                        {s2[0].toUpperCase() + s2.slice(1)}
                                                    </button>) : Object.keys(cfSorts).map((s2) => <button key={s2} className={`br-src${sort === s2 ? " active" : ""}`} onClick={() => setSort(s2)}>
                                                        {s2}
                                                    </button>)}
                                        </div>
                                    </div>
                                    <div className="br-filter-group">
                                        <label>Category</label>
                                        <div className="br-cats">
                                            {source === "modrinth" ? mrCategories.map((c) => <button key={c.name} className={`br-cat${cats.includes(c.name) ? " active" : ""}`} onClick={() => setCats((prev) => prev.includes(c.name) ? prev.filter((x) => x !== c.name) : [...prev, c.name])}>
                                                        {c.icon && c.icon.startsWith("<svg") ? <span className="br-cat-icon" dangerouslySetInnerHTML={{ __html: c.icon }} /> : c.icon && <img className="br-cat-icon" src={c.icon} alt="" />}
                                                        <span>{c.title || c.name}</span>
                                                    </button>) : cfCategories.map((c) => <button key={c.id} className={`br-cat${cats.includes(String(c.id)) ? " active" : ""}`} onClick={() => setCats((prev) => prev.includes(String(c.id)) ? prev.filter((x) => x !== String(c.id)) : [...prev, String(c.id)])}>
                                                        {c.iconUrl && <img className="br-cat-icon" src={c.iconUrl} alt="" />}
                                                        <span>{c.name}</span>
                                                    </button>)}
                                        </div>
                                    </div>
                                </div>}
                        </div>

                        {
  }
                        {results.length > 0 && <div className="br-pager">
                                <button className="br-page-btn" disabled={page <= 1} onClick={() => {
    const p = page - 1;
    setPage(p);
    doSearch(p);
  }}>
                                    <i className="ph ph-caret-left" /> Prev
                                </button>
                                <span className="br-page-num">Page {page}</span>
                                <button className="br-page-btn" disabled={results.length < 20} onClick={() => {
    const p = page + 1;
    setPage(p);
    doSearch(p);
  }}>
                                    Next <i className="ph ph-caret-right" />
                                </button>
                            </div>}
                    </div>
                </div>}
        </div>;
};
const fmtSize = (bytes) => {
  if (!bytes) return "0 B";
  const mb = bytes / (1024 * 1024);
  return mb >= 1 ? `${mb.toFixed(1)} MB` : `${(bytes / 1024).toFixed(0)} KB`;
};
const fmtPlayTime = (ticks) => {
  const totalMin = Math.floor(ticks / 20 / 60);
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
};
const itemIconCache = {};
const WorldsTab = ({ instName, mcVersion }) => {
  const [worlds, setWorlds] = useState([]);
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [renameTarget, setRenameTarget] = useState(null);
  const [renameName, setRenameName] = useState("");
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [selected, setSelected] = useState(null);
  const [loadingInfo, setLoadingInfo] = useState(false);
  const [icons2, setIcons] = useState({});
  const refresh = () => ListWorlds(instName).then(setWorlds).catch(() => setWorlds([]));
  useEffect(() => {
    refresh();
  }, [instName]);
  const openDashboard = async (w) => {
    setLoadingInfo(true);
    setSelected(null);
    try {
      const info = await GetWorldInfo(instName, w.name);
      setSelected(info);
    } catch (err) {
      setMsg(String(err));
    } finally {
      setLoadingInfo(false);
    }
  };
  useEffect(() => {
    if (!selected) return;
    const ids = [...new Set((selected.inventory || []).map((it) => it.id))];
    let cancelled = false;
    ids.forEach((id) => {
      const key = instName + "|" + id;
      if (itemIconCache[key] !== void 0) {
        setIcons((prev) => ({ ...prev, [id]: itemIconCache[key] }));
        return;
      }
      GetItemIcon(instName, mcVersion, id).then((b64) => {
        if (b64) itemIconCache[key] = b64;
        if (!cancelled) setIcons((prev) => ({ ...prev, [id]: b64 }));
      }).catch(() => {
        if (!cancelled) setIcons((prev) => ({ ...prev, [id]: "" }));
      });
    });
    return () => {
      cancelled = true;
    };
  }, [selected, instName, mcVersion]);
  const doDuplicate = async (w) => {
    try {
      const name = await DuplicateWorld(instName, w.name);
      setMsg(`Duplicated as "${name}"`);
      refresh();
    } catch (err) {
      setMsg(String(err));
    }
  };
  const doRename = async () => {
    if (!renameTarget || !renameName.trim()) return;
    try {
      await RenameWorld(instName, renameTarget.name, renameName);
      setMsg(`Renamed to "${renameName.trim()}"`);
      setRenameTarget(null);
      refresh();
      if (selected) setSelected({ ...selected, name: renameName.trim() });
    } catch (err) {
      setMsg(String(err));
    }
  };
  const doDelete = async () => {
    if (!deleteTarget) return;
    try {
      await DeleteWorld(instName, deleteTarget.name);
      setMsg(`Deleted "${deleteTarget.name}"`);
      setDeleteTarget(null);
      setSelected(null);
      refresh();
    } catch (err) {
      setMsg(String(err));
    }
  };
  const doExport = async (w) => {
    try {
      const path = await ExportWorld(instName, w.name);
      setMsg(path === "export cancelled" ? "" : `Exported to ${path}`);
    } catch (err) {
      setMsg(String(err));
    }
  };
  const doImport = async () => {
    setBusy(true);
    setMsg("");
    try {
      const name = await ImportWorld(instName);
      setMsg(name === "import cancelled" ? "" : `Imported "${name}"`);
      refresh();
    } catch (err) {
      setMsg(String(err));
    } finally {
      setBusy(false);
    }
  };
  const difficultyName = ["Peaceful", "Easy", "Normal", "Hard"];
  const gameModeName = ["Survival", "Creative", "Adventure", "Spectator"];
  const dimName = (d) => ({
    "minecraft:overworld": "Overworld",
    "minecraft:the_nether": "The Nether",
    "minecraft:the_end": "The End"
  })[d] || d || "Unknown";
  const itemName = (id) => id.replace(/^minecraft:/, "").replace(/_/g, " ");
  const invSlots = (info) => {
    const slots = {};
    (info.inventory || []).forEach((it) => {
      slots[it.slot] = it;
    });
    return slots;
  };
  const invSlot = (slots, s2) => {
    const it = slots[s2];
    return <div key={s2} className={`wd-slot${it ? " filled" : ""}`} title={it ? `${itemName(it.id)}${it.enchantments?.length ? " (" + it.enchantments.map((e) => itemName(e)).join(", ") + ")" : ""}` : ""}>
                {it && <>
                    {icons2[it.id] ? <img className="wd-slot-icon" src={`data:image/png;base64,${icons2[it.id]}`} alt="" /> : <span className="wd-slot-name">{itemName(it.id)}</span>}
                    {it.count > 1 && <span className="wd-slot-count">{it.count}</span>}
                </>}
            </div>;
  };
  const invRow = (slots, from, cls) => <div className={`wd-inv-row${cls ? " " + cls : ""}`}>
            {Array.from({ length: 9 }, (_, i) => invSlot(slots, from + i))}
        </div>;
  if (selected) {
    const w = selected;
    return <div className="ctab">
                <div className="ctab-toolbar">
                    <div className="ctab-toolbar-left">
                        <button className="ctab-browse-btn" onClick={() => setSelected(null)}>
                            <i className="ph ph-arrow-left" /> Back
                        </button>
                        <span className="ctab-label wd-title">{w.displayName || w.name}</span>
                    </div>
                    <div className="ctab-file-actions">
                        <button className="ctab-file-action" title="Open world folder" onClick={() => OpenWorldFolder(instName, w.name)}>
                            <i className="ph ph-folder-open" />
                        </button>
                        <button className="ctab-file-action" title="Duplicate (backup)" onClick={() => doDuplicate(w)}>
                            <i className="ph ph-copy" />
                        </button>
                        <button className="ctab-file-action" title="Rename" onClick={() => {
      setRenameTarget(w);
      setRenameName(w.name);
    }}>
                            <i className="ph ph-pencil-simple" />
                        </button>
                        <button className="ctab-file-action" title="Export as ZIP" onClick={() => doExport(w)}>
                            <i className="ph ph-export" />
                        </button>
                        <button className="ctab-file-action danger" title="Delete" onClick={() => setDeleteTarget(w)}>
                            <i className="ph ph-trash" />
                        </button>
                    </div>
                </div>

                <div className="wd-grid">
                    <div className="wd-card">
                        <div className="wd-card-title"><i className="ph ph-info" /> World</div>
                        <div className="wd-kv">
                            <span>Seed</span><b>{w.seed || "\u2014"}</b>
                            <span>Version</span><b>{w.version || "\u2014"}</b>
                            <span>Difficulty</span><b>{difficultyName[w.difficulty] || "\u2014"}</b>
                            <span>Game mode</span><b>{gameModeName[w.gameMode] || "\u2014"}</b>
                            <span>Hardcore</span><b>{w.hardcore ? "Yes" : "No"}</b>
                            <span>Size</span><b>{fmtSize(w.size)}</b>
                            <span>Last played</span><b>{w.lastPlayed ? new Date(w.lastPlayed).toLocaleString() : "\u2014"}</b>
                        </div>
                    </div>

                    <div className="wd-card">
                        <div className="wd-card-title"><i className="ph ph-user" /> Player</div>
                        <div className="wd-kv">
                            <span>Position</span><b>{w.pos ? `${w.pos[0].toFixed(1)} / ${w.pos[1].toFixed(1)} / ${w.pos[2].toFixed(1)}` : "\u2014"}</b>
                            <span>Dimension</span><b>{dimName(w.dimension)}</b>
                            <span>Health</span><b>{w.health ? `${w.health} \u2764` : "\u2014"}</b>
                            <span>Food</span><b>{w.foodLevel ? `${w.foodLevel}/20` : "\u2014"}</b>
                            <span>XP level</span><b>{w.xpLevel || "\u2014"}</b>
                        </div>
                    </div>

                    <div className="wd-card">
                        <div className="wd-card-title"><i className="ph ph-chart-bar" /> Statistics</div>
                        <div className="wd-kv">
                            <span>Days survived</span><b>{w.days}</b>
                            <span>Deaths</span><b>{w.deaths}</b>
                            <span>Mob kills</span><b>{w.mobKills}</b>
                            <span>Play time</span><b>{fmtPlayTime(w.playTimeSec * 20)}</b>
                        </div>
                    </div>
                </div>

                <div className="wd-card wd-inv">
                    <div className="wd-card-title"><i className="ph ph-backpack" /> Inventory</div>
                    {(w.inventory || []).length === 0 ? <div className="ctab-empty-text">No inventory data</div> : (() => {
      const slots = invSlots(w);
      return <div className="wd-inv-layout">
                                    <div className="wd-inv-side">
                                        {[[103, "Helmet"], [102, "Chestplate"], [101, "Leggings"], [100, "Boots"]].map(([s2, label]) => <div key={s2} className="wd-inv-armor-slot">
                                                {invSlot(slots, s2)}
                                                <span className="wd-inv-armor-label">{label}</span>
                                            </div>)}
                                        <div className="wd-inv-offhand">
                                            {invSlot(slots, 150)}
                                            <span className="wd-inv-armor-label">Offhand</span>
                                        </div>
                                    </div>
                                    <div className="wd-inv-body">
                                        {invRow(slots, 27)}
                                        {invRow(slots, 18)}
                                        {invRow(slots, 9)}
                                        {invRow(slots, 0, "wd-inv-hotbar")}
                                    </div>
                                </div>;
    })()}
                </div>

                {msg && <div className="isw-msg">{msg}</div>}
            </div>;
  }
  return <div className="ctab">
            <div className="ctab-toolbar">
                <div className="ctab-toolbar-left">
                    <span className="ctab-label">Worlds</span>
                    {worlds.length > 0 && <span className="ctab-count">{worlds.length}</span>}
                </div>
                <button className="ctab-browse-btn" onClick={doImport} disabled={busy}>
                    <i className="ph ph-upload-simple" />
                    Import world
                </button>
            </div>

            {worlds.length === 0 ? <div className="ctab-empty">
                    <i className="ph ph-globe" />
                    <span className="ctab-empty-text">No worlds saved</span>
                    <button className="ctab-empty-action" onClick={doImport} disabled={busy}>
                        <i className="ph ph-upload-simple" /> Import world
                    </button>
                  </div> : <div className="ctab-installed">
                    {worlds.map((w) => <div key={w.name} className="ctab-file" onClick={() => openDashboard(w)}>
                            {w.icon ? <img className="ctab-file-icon-img" src={`data:image/png;base64,${w.icon}`} alt="" /> : <div className="ctab-file-icon"><i className="ph ph-globe" /></div>}
                            <div className="ctab-file-body">
                                <span className="ctab-file-name">{w.displayName || w.name}</span>
                                <span className="ctab-file-sub">
                                    {w.lastPlayed ? new Date(w.lastPlayed).toLocaleString() : "never played"} · {fmtPlayTime(w.playTime)} · {fmtSize(w.size)}
                                </span>
                            </div>
                            <div className="ctab-file-actions">
                                <button className="ctab-file-action" title="Open world folder" onClick={(e) => {
    e.stopPropagation();
    OpenWorldFolder(instName, w.name);
  }}>
                                    <i className="ph ph-folder-open" />
                                </button>
                                <button className="ctab-file-action" title="Duplicate (backup)" onClick={(e) => {
    e.stopPropagation();
    doDuplicate(w);
  }}>
                                    <i className="ph ph-copy" />
                                </button>
                                <button className="ctab-file-action" title="Rename" onClick={(e) => {
    e.stopPropagation();
    setRenameTarget(w);
    setRenameName(w.name);
  }}>
                                    <i className="ph ph-pencil-simple" />
                                </button>
                                <button className="ctab-file-action" title="Export as ZIP" onClick={(e) => {
    e.stopPropagation();
    doExport(w);
  }}>
                                    <i className="ph ph-export" />
                                </button>
                                <button className="ctab-file-action danger" title="Delete" onClick={(e) => {
    e.stopPropagation();
    setDeleteTarget(w);
  }}>
                                    <i className="ph ph-trash" />
                                </button>
                            </div>
                        </div>)}
                  </div>}

            {loadingInfo && <div className="isw-msg">Loading world info…</div>}
            {msg && <div className="isw-msg">{msg}</div>}

            {renameTarget && <div className="modal-overlay" onClick={() => setRenameTarget(null)}>
                    <div className="modal" onClick={(e) => e.stopPropagation()}>
                        <h3 className="modal-title">Rename world</h3>
                        <div className="acc-form">
                            <div className="acc-form-label">New name</div>
                            <input
    className="modal-input acc-name-input"
    value={renameName}
    onChange={(e) => setRenameName(e.target.value)}
    onKeyDown={(e) => {
      if (e.key === "Enter") doRename();
    }}
    autoFocus
  />
                            <div className="acc-form-row">
                                <button className="modal-btn" onClick={doRename} disabled={!renameName.trim()}>
                                    <i className="ph ph-check" /> Rename
                                </button>
                            </div>
                        </div>
                    </div>
                </div>}

            {deleteTarget && <div className="modal-overlay" onClick={() => setDeleteTarget(null)}>
                    <div className="modal" onClick={(e) => e.stopPropagation()}>
                        <h3 className="modal-title">Delete world?</h3>
                        <div className="settings-row-desc">
                            "{deleteTarget.name}" will be permanently removed from disk. This cannot be undone.
                        </div>
                        <div className="acc-form-row">
                            <button className="modal-btn danger" onClick={doDelete}>
                                <i className="ph ph-trash" /> Delete
                            </button>
                        </div>
                    </div>
                </div>}
        </div>;
};
const ScreenshotCard = ({ instName, s: s2, onOpen, onCopy, onDelete }) => {
  const [data, setData] = useState("");
  useEffect(() => {
    let cancelled = false;
    GetScreenshot(instName, s2.name).then((b64) => {
      if (!cancelled) setData(b64);
    }).catch(() => {
    });
    return () => {
      cancelled = true;
    };
  }, [instName, s2.name]);
  return <div className="ss-card" onClick={() => onOpen(s2)}>
            <div className="ss-thumb">
                {data ? <img className="ss-thumb-img" src={`data:image/png;base64,${data}`} alt={s2.name} /> : <i className="ph ph-image" />}
            </div>
            <div className="ss-card-body">
                <span className="ss-name" title={s2.name}>{s2.name}</span>
                <span className="ss-sub">{fmtSize(s2.size)} · {new Date(s2.modified).toLocaleString()}</span>
            </div>
            <div className="ctab-file-actions">
                <button className="ctab-file-action" title="Copy to clipboard" onClick={(e) => {
    e.stopPropagation();
    onCopy(s2);
  }}>
                    <i className="ph ph-clipboard" />
                </button>
                <button className="ctab-file-action danger" title="Delete" onClick={(e) => {
    e.stopPropagation();
    onDelete(s2);
  }}>
                    <i className="ph ph-trash" />
                </button>
            </div>
        </div>;
};
const ScreenshotsTab = ({ instName }) => {
  const [shots, setShots] = useState([]);
  const [msg, setMsg] = useState("");
  const [previewIdx, setPreviewIdx] = useState(null);
  const [previewData, setPreviewData] = useState("");
  const [deleteTarget, setDeleteTarget] = useState(null);
  const refresh = () => ListScreenshots(instName).then(setShots).catch(() => setShots([]));
  useEffect(() => {
    refresh();
  }, [instName]);
  const openPreview = async (s2) => {
    const idx = shots.findIndex((x) => x.name === s2.name);
    setPreviewIdx(idx);
    try {
      const data = await GetScreenshot(instName, s2.name);
      setPreviewData(data);
    } catch (err) {
      setMsg(String(err));
    }
  };
  const navigate = async (dir) => {
    if (previewIdx === null || shots.length === 0) return;
    const next = (previewIdx + dir + shots.length) % shots.length;
    setPreviewIdx(next);
    try {
      const data = await GetScreenshot(instName, shots[next].name);
      setPreviewData(data);
    } catch (err) {
      setMsg(String(err));
    }
  };
  useEffect(() => {
    if (previewIdx === null) return;
    const onKey = (e) => {
      if (e.key === "ArrowLeft") navigate(-1);
      else if (e.key === "ArrowRight") navigate(1);
      else if (e.key === "Escape") setPreviewIdx(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [previewIdx, shots]);
  const doCopy = async (s2) => {
    try {
      await CopyScreenshotToClipboard(instName, s2.name);
      setMsg(`Copied "${s2.name}" to clipboard`);
    } catch (err) {
      setMsg(String(err));
    }
  };
  const doDelete = async () => {
    if (!deleteTarget) return;
    try {
      await DeleteScreenshot(instName, deleteTarget.name);
      setMsg(`Deleted "${deleteTarget.name}"`);
      setDeleteTarget(null);
      setPreviewIdx(null);
      refresh();
    } catch (err) {
      setMsg(String(err));
    }
  };
  const preview = previewIdx !== null ? shots[previewIdx] : null;
  return <div className="ctab">
            <div className="ctab-toolbar">
                <div className="ctab-toolbar-left">
                    <span className="ctab-label">Screenshots</span>
                    {shots.length > 0 && <span className="ctab-count">{shots.length}</span>}
                </div>
                <button className="ctab-browse-btn" onClick={() => OpenScreenshotsFolder(instName)}>
                    <i className="ph ph-folder-open" />
                    Open folder
                </button>
            </div>

            {shots.length === 0 ? <div className="ctab-empty">
                    <i className="ph ph-camera" />
                    <span className="ctab-empty-text">No screenshots yet</span>
                    <span className="ctab-empty-sub">Take a screenshot in game (F2) and it will appear here.</span>
                  </div> : <div className="ss-grid">
                    {shots.map((s2) => <ScreenshotCard
    key={s2.name}
    instName={instName}
    s={s2}
    onOpen={openPreview}
    onCopy={doCopy}
    onDelete={setDeleteTarget}
  />)}
                  </div>}

            {msg && <div className="isw-msg">{msg}</div>}

            {preview && <div className="modal-overlay" onClick={() => setPreviewIdx(null)}>
                    <div className="ss-preview" onClick={(e) => e.stopPropagation()}>
                        <div className="ss-preview-head">
                            <span className="ss-preview-name">{preview.name}</span>
                            <div className="ctab-file-actions">
                                <button className="ctab-file-action" title="Copy to clipboard" onClick={() => doCopy(preview)}>
                                    <i className="ph ph-clipboard" />
                                </button>
                                <button className="ctab-file-action danger" title="Delete" onClick={() => setDeleteTarget(preview)}>
                                    <i className="ph ph-trash" />
                                </button>
                                <button className="ctab-file-action" title="Close (Esc)" onClick={() => setPreviewIdx(null)}>
                                    <i className="ph ph-x" />
                                </button>
                            </div>
                        </div>
                        <div className="ss-preview-body">
                            <button className="ss-nav" title="Previous (←)" onClick={() => navigate(-1)} disabled={shots.length < 2}>
                                <i className="ph ph-caret-left" />
                            </button>
                            <img className="ss-preview-img" src={`data:image/png;base64,${previewData}`} alt={preview.name} />
                            <button className="ss-nav" title="Next (→)" onClick={() => navigate(1)} disabled={shots.length < 2}>
                                <i className="ph ph-caret-right" />
                            </button>
                        </div>
                        <div className="ss-preview-foot">{previewIdx + 1} / {shots.length}</div>
                    </div>
                </div>}

            {deleteTarget && <div className="modal-overlay" onClick={() => setDeleteTarget(null)}>
                    <div className="modal" onClick={(e) => e.stopPropagation()}>
                        <h3 className="modal-title">Delete screenshot?</h3>
                        <div className="settings-row-desc">
                            "{deleteTarget.name}" will be permanently removed from disk. This cannot be undone.
                        </div>
                        <div className="acc-form-row">
                            <button className="modal-btn danger" onClick={doDelete}>
                                <i className="ph ph-trash" /> Delete
                            </button>
                        </div>
                    </div>
                </div>}
        </div>;
};
// ScrollingName renders a truncated instance name that, when the text does not
// fit, scrolls horizontally on hover so the full name can be read. Names that
// fit never animate. The shift distance is measured from the DOM and exposed
// to CSS as --inst-name-shift.
const ScrollingName = ({ text, className = "instance-name" }) => {
  const outerRef = useRef(null);
  const innerRef = useRef(null);
  const [over, setOver] = useState(false);
  useEffect(() => {
    const outer = outerRef.current;
    const inner = innerRef.current;
    if (!outer || !inner) return;
    const check = () => {
      const overflow = inner.scrollWidth > outer.clientWidth + 1;
      setOver(overflow);
      if (overflow) {
        outer.style.setProperty("--inst-name-shift", `${-(inner.scrollWidth - outer.clientWidth + 12)}px`);
      } else {
        outer.style.removeProperty("--inst-name-shift");
      }
    };
    check();
    if (typeof ResizeObserver !== "undefined") {
      const ro = new ResizeObserver(check);
      ro.observe(outer);
      return () => ro.disconnect();
    }
    window.addEventListener("resize", check);
    return () => window.removeEventListener("resize", check);
  }, [text]);
  return <span ref={outerRef} className={className}>
            <span ref={innerRef} className={`inst-name-clip${over ? " inst-name-over" : ""}`}>{text}</span>
        </span>;
};
const InstSettingsTab = ({ inst, settings, onSave }) => {
  const ov = inst.meta.overrides || {};
  const [acc, setAcc] = useState(ov.account || { on: false, accountId: "" });
  const [mem, setMem] = useState(ov.memory || { on: false, memory: 2, minMemory: 0, jvmArgs: "" });
  const [win, setWin] = useState(ov.window || { on: false, winWidth: 1280, winHeight: 720, fullscreen: false });
  const [accounts, setAccounts] = useState([]);
  const [sysRam, setSysRam] = useState(16);
  const screenW = window.screen.width || 3840;
  const screenH = window.screen.height || 2160;
  const resPresets = [
    [854, 480],
    [1280, 720],
    [1600, 900],
    [1920, 1080],
    [2560, 1440],
    [3840, 2160]
  ].filter(([w, h]) => w <= screenW && h <= screenH);
  useEffect(() => {
    GetAccounts().then(setAccounts).catch(() => setAccounts([]));
    GetSystemMemory().then((gb) => { if (gb && gb > 1) setSysRam(gb); }).catch(() => {});
  }, []);
  const memMax = Math.max(8, sysRam);
  const memFloor = Math.max(16, Math.floor(navigator.deviceMemory || 16));
  // Effective values shown in controls: instance overrides when the switch is
  // on, otherwise the global launcher settings (which is what will be used).
  const effMem = mem.on ? mem : { memory: settings?.memory || 2, minMemory: settings?.minMemory || 0, jvmArgs: settings?.jvmArgs || "" };
  const effWin = win.on ? win : { winWidth: settings?.winWidth || 1280, winHeight: settings?.winHeight || 720, fullscreen: !!settings?.fullscreen };
  return <div className="settings-stack">
            <div className="settings-section">
                <div className="settings-section-title">
                    <span>Account</span>
                    <button className={`ctab-switch${acc.on ? " on" : " off"}`} role="switch" aria-checked={!!acc.on} title={acc.on ? "On" : "Off"} onClick={() => setAcc({ ...acc, on: !acc.on }) && onSave("account", { ...acc, on: !acc.on })}>
                        <span className="ctab-switch-knob" />
                    </button>
                </div>
                <fieldset disabled={!acc.on} className={`inst-sec-body${acc.on ? "" : " locked"}`}>
                <div className="settings-row">
                    <div className="settings-row-info">
                        <div className="settings-row-name">Account</div>
                        <div className="settings-row-desc">Launch this instance with a chosen account instead of the active one.</div>
                    </div>
                    <select className="modal-select" value={acc.accountId || ""} onChange={(e) => {
    const v = { on: true, accountId: e.target.value };
    setAcc(v);
    onSave("account", v);
  }}>
                        <option value="">Default (active account)</option>
                        {accounts.map((a) => <option key={a.id} value={a.id}>{a.name} {"\u2014"} {a.type === "microsoft" ? "Microsoft" : "Offline"}</option>)}
                    </select>
                </div>
                </fieldset>
            </div>
            <div className="settings-section">
                <div className="settings-section-title">
                    <span>Memory &amp; Java</span>
                    <button className={`ctab-switch${mem.on ? " on" : " off"}`} role="switch" aria-checked={!!mem.on} title={mem.on ? "On" : "Off"} onClick={() => setMem({ ...mem, on: !mem.on }) && onSave("memory", { ...mem, on: !mem.on })}>
                        <span className="ctab-switch-knob" />
                    </button>
                </div>
                <fieldset disabled={!mem.on} className={`inst-sec-body${mem.on ? "" : " locked"}`}>
                <div className="settings-row">
                    <div className="settings-row-info">
                        <div className="settings-row-name">RAM allocation</div>
                        <div className="settings-row-desc">Overrides the global memory setting for this instance.</div>
                    </div>
                    <div className="mem-control inst-mem-control">
                        <div className="mem-top">
                            <div className={`mem-badge${(effMem.memory || 2) > Math.floor(memFloor / 2) ? " warn" : ""}`}>
                                <span className="mem-badge-val">{effMem.memory || 2}</span>
                                <span className="mem-badge-unit">GB</span>
                            </div>
                            <input
    type="range"
    className="mem-slider"
    min="1"
    max={memMax}
    step="1"
    style={{ "--fill": `${((effMem.memory || 2) - 1) / Math.max(1, memMax - 1) * 100}%` }}
    value={effMem.memory || 2}
    onInput={(e) => {
      const v = { on: true, memory: Number(e.target.value), minMemory: Math.min(mem.minMemory || 0, Number(e.target.value)), jvmArgs: mem.jvmArgs || "" };
      setMem(v);
      onSave("memory", v);
    }}
    onChange={(e) => {
      const v = { on: true, memory: Number(e.target.value), minMemory: Math.min(mem.minMemory || 0, Number(e.target.value)), jvmArgs: mem.jvmArgs || "" };
      setMem(v);
      onSave("memory", v);
    }}
  />
                        </div>
                        <div className="mem-top">
                            <div className="mem-badge">
                                <span className="mem-badge-val">{effMem.minMemory || 0}</span>
                                <span className="mem-badge-unit">GB</span>
                            </div>
                            <input
    type="range"
    className="mem-slider"
    min="0"
    max={memMax}
    step="1"
    style={{ "--fill": `${(effMem.minMemory || 0) / Math.max(1, memMax) * 100}%` }}
    value={effMem.minMemory || 0}
    onInput={(e) => {
      const v = { on: true, memory: mem.memory || 2, minMemory: Number(e.target.value), jvmArgs: mem.jvmArgs || "" };
      setMem(v);
      onSave("memory", v);
    }}
    onChange={(e) => {
      const v = { on: true, memory: mem.memory || 2, minMemory: Number(e.target.value), jvmArgs: mem.jvmArgs || "" };
      setMem(v);
      onSave("memory", v);
    }}
  />
                        </div>
                    </div>
                </div>
                <div className="settings-row">
                    <div className="settings-row-info">
                        <div className="settings-row-name">JVM arguments</div>
                        <div className="settings-row-desc">Extra JVM arguments appended at launch.</div>
                    </div>
                    <textarea className="modal-input inst-jvm" rows={2} placeholder="-XX:+UseZGC" value={effMem.jvmArgs || ""} onChange={(e) => {
    const v = { on: true, memory: mem.memory || 2, jvmArgs: e.target.value };
    setMem(v);
    onSave("memory", v);
  }} />
                </div>
                </fieldset>
            </div>
            <div className="settings-section">
                <div className="settings-section-title">
                    <span>Window</span>
                    <button className={`ctab-switch${win.on ? " on" : " off"}`} role="switch" aria-checked={!!win.on} title={win.on ? "On" : "Off"} onClick={() => setWin({ ...win, on: !win.on }) && onSave("window", { ...win, on: !win.on })}>
                        <span className="ctab-switch-knob" />
                    </button>
                </div>
                <fieldset disabled={!win.on} className={`inst-sec-body${win.on ? "" : " locked"}`}>
                <div className="settings-row">
                    <div className="settings-row-info">
                        <div className="settings-row-name">Resolution</div>
                        <div className="settings-row-desc">Overrides the global window size for this instance.</div>
                    </div>
                    <div className="inst-res-row">
                        <input className="modal-input inst-res-input" type="number" min={640} max={screenW} value={effWin.winWidth || Math.min(1280, screenW)} onChange={(e) => {
    const v = { on: true, winWidth: Number(e.target.value), winHeight: win.winHeight || 720, fullscreen: !!win.fullscreen };
    setWin(v);
    onSave("window", v);
  }} />
                        <span className="settings-res-x">{"\u00D7"}</span>
                        <input className="modal-input inst-res-input" type="number" min={480} max={screenH} value={effWin.winHeight || Math.min(720, screenH)} onChange={(e) => {
    const v = { on: true, winWidth: win.winWidth || 1280, winHeight: Number(e.target.value), fullscreen: !!win.fullscreen };
    setWin(v);
    onSave("window", v);
  }} />
                        <select className="modal-select" value="" onChange={(e) => {
    if (!e.target.value) return;
    const [w, h] = e.target.value.split("x").map(Number);
    const v = { on: true, winWidth: w, winHeight: h, fullscreen: !!win.fullscreen };
    setWin(v);
    onSave("window", v);
  }}>
                            <option value="">Preset...</option>
                            {resPresets.map(([w, h]) => <option key={`${w}x${h}`} value={`${w}x${h}`}>{w} {"\u00D7"} {h}</option>)}
                        </select>
                    </div>
                </div>
                <div className="settings-row">
                    <div className="settings-row-info">
                        <div className="settings-row-name">Fullscreen</div>
                        <div className="settings-row-desc">Start the game in fullscreen mode.</div>
                    </div>
                    <button className={`ctab-switch${effWin.fullscreen ? " on" : " off"}`} role="switch" aria-checked={!!effWin.fullscreen} title={effWin.fullscreen ? "On" : "Off"} onClick={() => {
    const v = { on: true, winWidth: win.winWidth || 1280, winHeight: win.winHeight || 720, fullscreen: !win.fullscreen };
    setWin(v);
    onSave("window", v);
  }}>
                        <span className="ctab-switch-knob" />
                    </button>
                </div>
                </fieldset>
            </div>
        </div>;
};
export function App() {
  const [active, setActive] = useState("home");
  const [stateLoaded, setStateLoaded] = useState(false);
  const booted = useRef(false);
  const [groups, setGroups] = useState(() => [{ id: uid(), name: "Default", instances: [], default: true, expanded: true }]);
  useEffect(() => {
    GetPersistedState().then((stateJson) => {
      const s2 = JSON.parse(stateJson || "{}");
      if (s2 && typeof s2 === "object") {
        const settings2 = s2.settings;
        if (settings2 && typeof settings2 === "object" && Object.keys(settings2).length) {
          const dlMode = settings2.dlMode || (settings2.concurrencyAuto === true ? "auto" : settings2.concurrencyAuto === false ? "manual" : "off");
          setSettings({ ...{ panoramaMode: "animate", pickerLayout: "list", concurrency: 8, memory: 2, minMemory: 0, jvmArgs: "", javaPath: "", winWidth: 1280, winHeight: 720, fullscreen: false, server: "", env: "", preLaunch: "", postExit: "", autoLogs: true, minimizeOnLaunch: false, quitOnLaunch: false }, ...settings2, dlMode });
        }
        const g = s2.groups;
        if (Array.isArray(g) && g.length) {
          const g2 = g.map((gr, i) => ({
            ...gr.name === "Default" && !gr.default ? { ...gr, default: true } : gr,
            expanded: gr.expanded ?? i === 0,
            instances: (gr.instances || []).map((inst) => inst?.meta?.source === "client" && inst.meta.client === "ogulniega" && !inst.meta.loader ? { ...inst, meta: { ...inst.meta, loader: "Fabric" } } : inst)
          }));
          setGroups(g2);
          const san = (n) => String(n || "").replace(/[\\/:*?"<>|]/g, "_").trim().toLowerCase() || "instance";
          const seen = new Set();
          const dupes = [];
          for (const gr of g2) for (const inst of (gr.instances || [])) {
            const k = san(inst.name);
            if (seen.has(k)) dupes.push([inst.id, inst.name]);
            else seen.add(k);
          }
          if (dupes.length) {
            (async () => {
              for (const [id, nm] of dupes) {
                try {
                  const finalName = await CreateInstance(nm);
                  setGroups((prev) => prev.map((gr) => ({ ...gr, instances: (gr.instances || []).map((i) => i.id === id ? { ...i, name: finalName } : i) })));
                } catch (e) {
                  console.error("[repair] duplicate instance", e);
                }
              }
            })();
          }
          const lp = s2.settings?.lastPlayed;
          if (lp && g2.some((gr) => gr.instances.some((i) => i.id === lp))) {
            setHomeInstId(lp);
          }
        }
      }
      setStateLoaded(true);
    }).catch(() => {
      setStateLoaded(true);
    });
  }, []);
  useEffect(() => {
    if (!stateLoaded) return;
    if (groups.some((g) => g.default)) return;
    setGroups([{ id: uid(), name: "Default", instances: [], default: true, expanded: true }, ...groups]);
  }, [stateLoaded, groups]);
  useEffect(() => {
    if (!stateLoaded) return;
    syncPlugins();
  }, [stateLoaded]);
  const [dropPending, setDropPending] = useState(null);
  const [dropBusy, setDropBusy] = useState(false);
  const [, setRegistryTick] = useState(0);
  useEffect(() => subscribeRegistry(() => setRegistryTick((t) => t + 1)), []);
  useEffect(() => {
    const offChanged = EventsOn("plugins:changed", () => syncPlugins());
    const offDrop = EventsOn("plugins:dropRequest", (payload) => {
      if (payload && payload.manifest) setDropPending(payload);
    });
    return () => {
      offChanged();
      offDrop();
    };
  }, []);
  const confirmDropInstall = async () => {
    setDropBusy(true);
    try {
      await InstallPluginFromZip(dropPending.path);
      syncPlugins();
      setDropPending(null);
    } catch (e) {
      console.error("Plugin install failed:", e);
      setDropPending(null);
    } finally {
      setDropBusy(false);
    }
  };
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [showInstModal, setShowInstModal] = useState(false);
  const [groupName, setGroupName] = useState("");
  const [groupError, setGroupError] = useState("");
  const [dragId, setDragId] = useState(null);
  const [dragOverGroup, setDragOverGroup] = useState(null);
  const [deleteSel, setDeleteSel] = useState([]);
  const [deleteGroups, setDeleteGroups] = useState([]);
  const [deleteMode, setDeleteMode] = useState(false);
  const [pendingDelete, setPendingDelete] = useState([]);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [deleteLunarFolder, setDeleteLunarFolder] = useState(false);
  const [deleteFeatherFolder, setDeleteFeatherFolder] = useState(false);
  const [deleteDawnFolder, setDeleteDawnFolder] = useState(false);
  const [deleteVersionFiles, setDeleteVersionFiles] = useState(false);
  const [renameId, setRenameId] = useState(null);
  const [renameName, setRenameName] = useState("");
  const [homeInstId, setHomeInstId] = useState("");
  const [showLaunchPicker, setShowLaunchPicker] = useState(false);
  const [accounts, setAccounts] = useState([]);
  const [activeAccountId, setActiveAccountId] = useState("");
  const [accLoginPrompt, setAccLoginPrompt] = useState("");
  const [accTab, setAccTab] = useState("offline");
  const [showAccModal, setShowAccModal] = useState(false);
  const [accPicked, setAccPicked] = useState(null);
  const [skinAccountId, setSkinAccountId] = useState("");
  const [skinFile, setSkinFile] = useState(null);
  const [skinVariant, setSkinVariant] = useState("CLASSIC");
  const [skinUploading, setSkinUploading] = useState(false);
  const [skinMsg, setSkinMsg] = useState("");
  const [skinMsgOk, setSkinMsgOk] = useState(false);
  const skinMsgTimer = useRef(null);
  const showSkinMsg = (msg, ok) => {
    setSkinMsg(msg);
    setSkinMsgOk(ok);
    clearTimeout(skinMsgTimer.current);
    if (ok) skinMsgTimer.current = setTimeout(() => setSkinMsg(""), 5e3);
  };
  const clearSkinMsg = () => {
    clearTimeout(skinMsgTimer.current);
    setSkinMsg("");
  };
  const [localSkins, setLocalSkins] = useState([]);
  const refreshLocalSkins = () => ListLocalSkins().then(setLocalSkins).catch(() => setLocalSkins([]));
  useEffect(() => {
    if (active === "skins") refreshLocalSkins();
  }, [active]);
  const [offlineName, setOfflineName] = useState("");
  const [msLogin, setMsLogin] = useState(null);
  const [msError, setMsError] = useState("");
  const [accError, setAccError] = useState("");
  const refreshAccounts = () => {
    GetAccounts().then((list) => setAccounts(list)).catch(() => {
    });
    GetActiveAccount().then((id) => setActiveAccountId(id)).catch(() => {
    });
  };
  useEffect(() => {
    if (accounts.length && activeAccountId) setAccLoginPrompt("");
  }, [accounts.length, activeAccountId]);
  useEffect(() => {
    refreshAccounts();
  }, []);
  const addOffline = async () => {
    setAccError("");
    try {
      const acc = await AddOfflineAccount(offlineName);
      setOfflineName("");
      refreshAccounts();
      if (acc.id) setActiveAccountId(acc.id);
      setShowAccModal(false);
      setAccPicked(null);
    } catch (e) {
      setAccError(String(e));
    }
  };
  const startMsLogin = async () => {
    setAccError("");
    setMsError("");
    try {
      const dc = await AddMicrosoftAccount();
      setMsLogin({ userCode: dc.user_code, verificationUri: dc.verification_uri, deviceCode: dc.device_code, interval: dc.interval });
      pollMsLogin(dc.device_code, dc.interval || 5);
    } catch (e) {
      setMsError(String(e));
    }
  };
  const pollMsLogin = async (deviceCode, interval) => {
    try {
      const acc = await PollMicrosoftLogin(deviceCode, interval);
      setMsLogin(null);
      refreshAccounts();
      if (acc.id) setActiveAccountId(acc.id);
      setShowAccModal(false);
      setAccPicked(null);
    } catch (e) {
      setMsError(String(e));
      setMsLogin(null);
    }
  };
  const setActiveAcc = async (id) => {
    try {
      await SetActiveAccount(id);
      setActiveAccountId(id);
    } catch (e) {
      setAccError(String(e));
    }
  };
  const delAccount = async (id) => {
    try {
      await DeleteAccount(id);
      refreshAccounts();
    } catch (e) {
      setAccError(String(e));
    }
  };
  const onSkinFilePicked = (e) => {
    const f = e.target.files?.[0];
    if (!f) return;
    if (!f.type.startsWith("image/png")) {
      showSkinMsg("Only PNG files are supported.", false);
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      const base64 = String(reader.result).split(",")[1];
      setSkinFile({ name: f.name, url: String(reader.result), base64 });
      setSkinMsg("");
    };
    reader.readAsDataURL(f);
    e.target.value = "";
  };
  const uploadSkin = async () => {
    const accId = skinAccountId || activeAccountId;
    if (!skinFile || !skinFile.base64 || !accId) {
      showSkinMsg("Choose a PNG file first.", false);
      return;
    }
    const acc = accounts.find((a) => a.id === accId);
    if (!acc || acc.type !== "microsoft") {
      showSkinMsg("Skin upload requires a Microsoft account. Offline accounts can't change their skin.", false);
      return;
    }
    setSkinUploading(true);
    setSkinMsg("");
    try {
      await UploadSkin(acc.id, skinFile.base64, skinVariant);
      showSkinMsg("Skin uploaded. It will appear in game immediately.", true);
      refreshLocalSkins();
    } catch (err) {
      showSkinMsg(String(err), false);
    } finally {
      setSkinUploading(false);
    }
  };
  const useLocalSkin = async (s2) => {
    const accId = skinAccountId || activeAccountId;
    const acc = accounts.find((a) => a.id === accId);
    if (!acc || acc.type !== "microsoft") {
      showSkinMsg("Using a saved skin requires a Microsoft account.", false);
      return;
    }
    setSkinUploading(true);
    setSkinMsg("");
    try {
      const res = await fetch(s2.url);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const blob = await res.blob();
      const base64 = await new Promise((resolve, reject) => {
        const r = new FileReader();
        r.onload = () => resolve(String(r.result).split(",")[1]);
        r.onerror = reject;
        r.readAsDataURL(blob);
      });
      await UploadSkin(acc.id, base64, skinVariant);
      showSkinMsg(`Skin set for ${acc.name}.`, true);
    } catch (err) {
      showSkinMsg(String(err), false);
    } finally {
      setSkinUploading(false);
    }
  };
  const deleteLocalSkin = async (s2, ev) => {
    ev.stopPropagation();
    if (!confirm(`Delete saved skin "${s2.name}"?`)) return;
    try {
      await DeleteLocalSkin(s2.name);
      if (skinFile && skinFile.url === s2.url) setSkinFile(null);
      refreshLocalSkins();
    } catch (err) {
      showSkinMsg(String(err), false);
    }
  };
  const [gameRunning, setGameRunning] = useState(false);
  const [gameProgress, setGameProgress] = useState(null);
  const [gameLog, setGameLog] = useState([]);
  const [gameError, setGameError] = useState("");
  const [launching, setLaunching] = useState(false);
  const [showLogs, setShowLogs] = useState(false);
  const [logAuto, setLogAuto] = useState(true);
  const refreshLogs = () => GetLogs().then((lines) => {
    if (lines && lines.length) setGameLog((prev) => [...prev.slice(-499), ...lines]);
  }).catch(() => {
  });
  useEffect(() => {
    if (showLogs) refreshLogs();
  }, [showLogs]);
  useEffect(() => {
    IsGameRunning().then(setGameRunning).catch(() => {
    });
  }, []);
  useEffect(() => {
    if (!launching && !gameRunning) return;
    const id = setInterval(() => {
      GetProgress().then((p) => {
        if (p && p.phase) setGameProgress(p);
      }).catch(() => {
      });
      GetState().then((s2) => {
        if (s2.error) {
          setGameError(s2.error);
          setGameRunning(false);
          setLaunching(false);
          return;
        }
        if (s2.running) {
          setGameRunning(true);
          setLaunching(false);
          setGameProgress(null);
          setGameError("");
          if (!gameRunning) {
            if (settings.autoLogs) setShowLogs(true);
            if (settings.quitOnLaunch) Quit();
            else if (settings.minimizeOnLaunch) WindowMinimise();
          }
        } else if (gameRunning) {
          setGameRunning(false);
          setLaunching(false);
          setGameProgress(null);
          if (settings.minimizeOnLaunch) WindowUnminimise();
        }
      }).catch(() => {
      });
      GetLogs().then((lines) => {
        if (lines && lines.length) setGameLog((prev) => [...prev.slice(-499), ...lines]);
      }).catch(() => {
      });
    }, 200);
    return () => clearInterval(id);
  }, [launching, gameRunning]);
  const [panoVersion, setPanoVersion] = useState("");
  const [settingsTab, setSettingsTab] = useState("background");
  const [storage, setStorage] = useState(null);
  const [cleaning, setCleaning] = useState(false);
  const [stHover, setStHover] = useState(null);
  const loadStorage = () => {
    DiskUsage().then(setStorage).catch(() => {});
  };
  const fmtDisk = (n) => {
    if (!n) return "0 B";
    if (n >= 1073741824) return `${(n / 1073741824).toFixed(2)} GB`;
    return fmtBytes(n);
  };
  const [settings, setSettings] = useState(() => ({ panoramaMode: "animate", pickerLayout: "list", dlMode: "off", concurrency: 8, memory: 2, minMemory: 0, jvmArgs: "", javaPath: "", winWidth: 1280, winHeight: 720, fullscreen: false, server: "", env: "", preLaunch: "", postExit: "", lastPlayed: "", autoLogs: true, minimizeOnLaunch: false, quitOnLaunch: false }));
  useEffect(() => {
    if (!stateLoaded) return;
    if (!booted.current) {
      booted.current = true;
      return;
    }
    console.log("[save] writing", JSON.stringify(settings).length, JSON.stringify(groups).length);
    SaveState(JSON.stringify(settings), JSON.stringify(groups)).then(() => console.log("[save] ok")).catch((e) => console.error("[save] error", e));
  }, [stateLoaded, settings, groups]);
  useEffect(() => {
    if (!stateLoaded) return;
    const inst = groups.flatMap((g) => g.instances).find((i) => i.id === homeInstId);
    if (inst?.meta?.mcVersion) {
      setPanoVersion(inst.meta.mcVersion);
      return;
    }
    GetPanoramaVersions().then((list) => {
      const rel = list.find((v) => v.type === "release");
      setPanoVersion(rel?.id || list[0]?.id || "");
    }).catch(() => setPanoVersion(""));
  }, [homeInstId]);
  const [instTab, setInstTab] = useState("vanilla");
  const [instName, setInstName] = useState("");
  const [instMeta, setInstMeta] = useState({});
  const [instGroup, setInstGroup] = useState("");
  const [mcVersions, setMcVersions] = useState([]);
  const [mcVersion, setMcVersion] = useState("");
  const [loader, setLoader] = useState("Vanilla");
  const [loaderSupport, setLoaderSupport] = useState({});
  const [loaderChecking, setLoaderChecking] = useState(false);
  const [loaderVersions, setLoaderVersions] = useState([]);
  const [loaderVersion, setLoaderVersion] = useState("");
  const [vfilters, setVfilters] = useState({ release: true, snapshot: true, old_beta: false, old_alpha: false });
  const [vsearch, setVsearch] = useState("");
  const [mrQuery, setMrQuery] = useState("");
  const [mrSort, setMrSort] = useState("relevance");
  const [mrResults, setMrResults] = useState([]);
  const [mrPage, setMrPage] = useState(1);
  const [mrTotal, setMrTotal] = useState(0);
  const [mrLoading, setMrLoading] = useState(false);
  const [mrSelected, setMrSelected] = useState(null);
  const [mrVersions, setMrVersions] = useState([]);
  const [mrVersion, setMrVersion] = useState("");
  const [cfQuery, setCfQuery] = useState("");
  const [cfSort, setCfSort] = useState("Popularity");
  const [cfResults, setCfResults] = useState([]);
  const [cfPage, setCfPage] = useState(1);
  const [cfTotal, setCfTotal] = useState(0);
  const [cfLoading, setCfLoading] = useState(false);
  const [cfSelected, setCfSelected] = useState(null);
  const [cfFiles, setCfFiles] = useState([]);
  const [cfFile, setCfFile] = useState("");
  const [clients, setClients] = useState([]);
  const [client, setClient] = useState("");
  const [clientVersions, setClientVersions] = useState([]);
  const [clientVersion, setClientVersion] = useState("");
  const [clientModules, setClientModules] = useState([]);
  const [clientModule, setClientModule] = useState("");
  const [clientLoader, setClientLoader] = useState("");
  const [clientLoading, setClientLoading] = useState(false);
  useEffect(() => {
    if (!showInstModal || instTab !== "clients") return;
    if (clients.length) return;
    Clients().then(setClients).catch(() => setClients([]));
  }, [showInstModal, instTab, clients.length]);
  useEffect(() => {
    if (!showInstModal || instTab !== "clients" || !client) return;
    setClientLoading(true);
    setClientVersions([]);
    setClientVersion("");
    setClientModules([]);
    setClientModule("");
    setClientLoader("");
    ClientVersions(client).then((vs) => {
      setClientVersions(vs || []);
      setClientVersion(vs?.[0]?.id || "");
    }).catch(() => {
    }).finally(() => setClientLoading(false));
  }, [showInstModal, instTab, client]);
  useEffect(() => {
    if (!clientVersions.length || !clientVersion) return;
    const ver = clientVersions.find((v) => v.id === clientVersion);
    const mods = ver?.modules || [];
    setClientLoader(ver?.loader || "");
    const seen = /* @__PURE__ */ new Set();
    const deduped = [];
    for (const m of mods) {
      const label = m === "lunar-noOF" ? "lunar" : m;
      if (seen.has(label)) continue;
      seen.add(label);
      deduped.push(m);
    }
    setClientModules(deduped);
    setClientModule(deduped[0] || "");
  }, [clientVersion, clientVersions]);
  useEffect(() => {
    if (!showInstModal || instTab !== "vanilla") return;
    if (mcVersions.length) return;
    HttpGet("https://launchermeta.mojang.com/mc/game/version_manifest_v2.json").then((body) => {
      const data = JSON.parse(body);
      setMcVersions(data.versions || []);
      const firstRelease = (data.versions || []).find((v) => v.type === "release");
      setMcVersion(firstRelease?.id || data.versions?.[0]?.id || "");
    }).catch(() => setMcVersions([]));
  }, [showInstModal, instTab]);
  useEffect(() => {
    if (!showInstModal || instTab !== "vanilla" || !mcVersion) return;
    setLoaderChecking(true);
    setLoaderSupport({});
    Promise.all(
      loaders.filter((l) => l !== "Vanilla").map(
        (l) => fetchLoaderVersions(l, mcVersion).then((vs) => [l, vs.length > 0])
      )
    ).then((results) => {
      setLoaderSupport(Object.fromEntries(results));
      setLoaderChecking(false);
    });
  }, [showInstModal, instTab, mcVersion]);
  useEffect(() => {
    if (!showInstModal || instTab !== "vanilla" || loader === "Vanilla" || !mcVersion) {
      setLoaderVersions([]);
      setLoaderVersion("");
      return;
    }
    fetchLoaderVersions(loader, mcVersion).then((versions) => {
      setLoaderVersions(versions);
      setLoaderVersion(versions[0] || "");
    });
  }, [showInstModal, instTab, loader, mcVersion]);
  useEffect(() => {
    if (!showInstModal || instTab !== "modrinth") return;
    const t = setTimeout(() => searchModrinth(mrPage), 350);
    return () => clearTimeout(t);
  }, [showInstModal, instTab, mrQuery, mrSort, mrPage]);
  useEffect(() => {
    if (!showInstModal || instTab !== "curseforge") return;
    const t = setTimeout(() => searchCurseForge(cfPage), 350);
    return () => clearTimeout(t);
  }, [showInstModal, instTab, cfQuery, cfSort, cfPage]);
  const searchModrinth = (page) => {
    setMrLoading(true);
    const q = encodeURIComponent(mrQuery.trim());
    const offset = (Math.max(1, page || 1) - 1) * 20;
    HttpGet(`https://api.modrinth.com/v2/search?facets=${encodeURIComponent('[["project_type:modpack"]]')}&query=${q}&limit=20&offset=${offset}&index=${mrSort}`).then((body) => {
      const d = JSON.parse(body);
      setMrResults(d.hits || []);
      setMrTotal(d.total_hits || 0);
    }).catch(() => setMrResults([])).finally(() => setMrLoading(false));
  };
  const selectModrinth = (hit) => {
    setMrSelected(hit);
    setMrVersion("");
    setMrVersions([]);
    HttpGet(`https://api.modrinth.com/v2/project/${hit.project_id}/version`).then((body) => {
      const versions = JSON.parse(body);
      setMrVersions(versions);
      setMrVersion(versions[0]?.id || "");
    }).catch(() => setMrVersions([]));
  };
  const searchCurseForge = (page) => {
    setCfLoading(true);
    const index = (Math.max(1, page || 1) - 1) * 50;
    CurseForgeSearch(cfQuery.trim(), "", cfSortFields[cfSort] || "", index, 0, 0, 0).then((body) => {
      const d = JSON.parse(body);
      setCfResults(d.data || []);
      setCfTotal(d.pagination?.totalCount || 0);
    }).catch(() => setCfResults([])).finally(() => setCfLoading(false));
  };
  const selectCurseForge = (mod) => {
    setCfSelected(mod);
    setCfFile("");
    setCfFiles([]);
    CurseForgeFiles(String(mod.id)).then((body) => {
      const files = JSON.parse(body).data || [];
      setCfFiles(files);
      setCfFile(files[0]?.id ? String(files[0].id) : "");
    }).catch(() => setCfFiles([]));
  };
  const addInstance = (meta) => {
    const targetId = instGroup || groups[0].id;
    const name = instName.trim() || (meta.source === "vanilla" ? `${loader} ${mcVersion}` : meta.source === "modrinth" ? meta.projectTitle : meta.source === "client" ? `${clientName(meta.client)} ${meta.mcVersion}` : meta.modName);
    CreateInstance(name).then((finalName) => {
      const inst = { id: uid(), name: finalName, meta };
      setGroups(groups.map(
        (g) => g.id === targetId ? { ...g, instances: [...g.instances, inst] } : g
      ));
      if (!homeInstId || !groups.some((g) => g.instances.some((i) => i.id === homeInstId))) {
        setHomeInstId(inst.id);
      }
      if (finalName !== name) flashMsg(`Name taken, created as "${finalName}"`);
    }).catch((e) => flashMsg(String(e))).finally(() => {
      setShowInstModal(false);
      setInstName("");
      setInstIcon("");
      setInstMeta({});
    });
  };
  const updateInstanceMeta = (instId, patch) => {
    setGroups(groups.map((g) => ({
      ...g,
      instances: g.instances.map((i) => i.id === instId ? { ...i, meta: { ...i.meta, ...patch } } : i)
    })));
  };
  const iconInputRef = useRef(null);
  const [instIcon, setInstIcon] = useState("");
  const [instSettings, setInstSettings] = useState(null);
  const [instSettingsTab, setInstSettingsTab] = useState("mods");
  const settingsInst = instSettings ? groups.flatMap((g) => g.instances).find((i) => i.id === instSettings.id) : null;
  const openInstSettings = (inst) => {
    setInstSettingsTab("mods");
    setInstSettings(inst);
  };
  const closeInstSettings = () => setInstSettings(null);
  const [iswMsg, setIswMsg] = useState("");
  const iswMsgTimer = useRef(null);
  const flashMsg = (m) => {
    setIswMsg(m);
    clearTimeout(iswMsgTimer.current);
    iswMsgTimer.current = setTimeout(() => setIswMsg(""), 4e3);
  };
  const openInstFolder = () => {
    OpenInstanceFolder(settingsInst.name).catch((e) => flashMsg(String(e)));
  };
  const duplicateInst = () => {
    InstanceSize(settingsInst.name).then((size) => {
      if (!confirm(`Duplicate "${settingsInst.name}"? Copies about ${fmtDisk(size)} on disk.`)) return;
      DuplicateInstance(settingsInst.name).then((newName) => {
        const origGroup = groups.find((g) => g.instances.some((i) => i.id === settingsInst.id));
        const inst = { id: uid(), name: newName, meta: { ...settingsInst.meta } };
        setGroups(groups.map((g) => g.id === origGroup?.id ? { ...g, instances: [...g.instances, inst] } : g));
        flashMsg(`Duplicated as "${newName}"`);
      }).catch((e) => flashMsg(String(e)));
    }).catch((e) => flashMsg(String(e)));
  };
  const exportInst = () => {
    ExportInstance(settingsInst.name).then((path) => flashMsg(`Exported to ${path}`)).catch((e) => flashMsg(String(e)));
  };
  const setOv = (group, patch) => {
    if (!settingsInst) return;
    const ov = { ...settingsInst.meta.overrides || {} };
    const cur = ov[group] || {};
    let next;
    if (patch.on) {
      const defaults = {
        general: { name: settingsInst.name, notes: "", gameDir: "", showConsole: true, autoCloseConsole: true },
        window: { winWidth: settings.winWidth || 1280, winHeight: settings.winHeight || 720, fullscreen: false },
        server: { server: settings.server || "", port: "25565" },
        gameArgs: { args: "" },
        env: { env: settings.env || "" },
        java: { javaPath: settings.javaPath || "", mode: settings.javaPath ? "custom" : "auto" },
        memory: { memory: settings.memory || 2, minMemory: settings.minMemory || 0, jvmArgs: settings.jvmArgs || "" },
        javaArgs: { args: "" },
        compatibility: { ignoreCompat: false },
        commands: { preLaunch: settings.preLaunch || "", postExit: settings.postExit || "" },
        workDir: { path: "" }
      }[group];
      next = { ...cur, ...defaults, ...patch };
    } else {
      next = { ...cur, on: false };
    }
    updateInstanceMeta(settingsInst.id, { overrides: { ...ov, [group]: next } });
  };
  const onIconPicked = (e) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      setInstIcon(reader.result);
      e.target.value = "";
    };
    reader.readAsDataURL(file);
  };
  const setGeneral = (patch) => {
    if (!settingsInst) return;
    const cur = settingsInst.meta.overrides?.general || { name: settingsInst.name, notes: "", gameDir: "", showConsole: true, autoCloseConsole: true };
    updateInstanceMeta(settingsInst.id, { overrides: { ...settingsInst.meta.overrides, general: { ...cur, ...patch } } });
  };
  const onSettingsIconPicked = (e) => {
    const file = e.target.files?.[0];
    if (!file || !settingsInst) return;
    const reader = new FileReader();
    reader.onload = () => {
      updateInstanceMeta(settingsInst.id, { icon: reader.result });
      e.target.value = "";
    };
    reader.readAsDataURL(file);
  };
  const clientName = (id) => clients.find((c) => c.id === id)?.name || (id === "lunar" ? "Lunar Client" : id);
  const defaultInstIcon = (inst) => {
    if (inst.meta.source === "lunar" || inst.meta.source === "client") {
      return clientIcon[inst.meta.client || "lunar"] || clientIcon.lunar;
    }
    return loaderIcon[inst.meta.loader] || null;
  };
  const modalPreviewIcon = () => {
    if (instIcon) return instIcon;
    if (instTab === "modrinth") return mrSelected?.icon_url || "";
    if (instTab === "curseforge") return cfSelected?.logo?.url || "";
    if (instTab === "clients") return clientIcon[client] || clientIcon.lunar;
    return loaderIcon[loader] || "";
  };
  const InstIcon = ({ inst }) => <div className="instance-card-icon">
            {inst.meta.icon ? <img src={inst.meta.icon} alt="" /> : defaultInstIcon(inst) ? defaultInstIcon(inst).includes("/") || defaultInstIcon(inst).includes(".") ? <img src={defaultInstIcon(inst)} alt="" /> : <i className={`ph ph-${defaultInstIcon(inst)}`} /> : <i className="ph ph-cube" />}
        </div>;
  const addGroup = () => {
    const name = groupName.trim();
    if (!name) return;
    if (groups.some((g) => g.name.toLowerCase() === name.toLowerCase())) {
      setGroupError("Cannot create groups with the same name. Pick a different name.");
      return;
    }
    setGroupError("");
    const id = uid();
    setGroups([...groups, { id, name, instances: [], expanded: true }]);
    setShowGroupModal(false);
    setGroupName("");
  };
  const startRename = (group) => {
    setRenameId(group.id);
    setRenameName(group.name);
  };
  const cancelRename = () => {
    setRenameId(null);
  };
  const saveRename = () => {
    if (renameId === null) return;
    const name = renameName.trim();
    const group = groups.find((g) => g.id === renameId);
    if (!group || !name || name === group.name) {
      setRenameId(null);
      return;
    }
    if (groups.some((g) => g.id !== renameId && g.name.toLowerCase() === name.toLowerCase())) {
      setRenameId(null);
      return;
    }
    setGroups(groups.map((g) => g.id === renameId ? { ...g, name } : g));
    setRenameId(null);
  };
  const toggleGroup = (id) => {
    setGroups(groups.map(
      (g) => g.id === id ? { ...g, expanded: !g.expanded } : g
    ));
  };
  const openDeleteModal = () => {
    setDeleteSel([]);
    setDeleteGroups([]);
    setDeleteMode(true);
  };
  const toggleDeleteSel = (id) => {
    setDeleteSel(deleteSel.includes(id) ? deleteSel.filter((x) => x !== id) : [...deleteSel, id]);
  };
  const toggleDeleteGroup = (id) => {
    if (groups.find((g) => g.id === id)?.default) return;
    setDeleteGroups(deleteGroups.includes(id) ? deleteGroups.filter((x) => x !== id) : [...deleteGroups, id]);
  };
  const cancelDelete = () => {
    setDeleteMode(false);
    setDeleteSel([]);
    setDeleteGroups([]);
  };
  const deleteInstances = () => {
    const doomed = groups.flatMap(
      (g) => deleteGroups.includes(g.id) ? g.instances : g.instances.filter((i) => deleteSel.includes(i.id))
    );
    setPendingDelete(doomed);
    setDeleteLunarFolder(false);
    setDeleteFeatherFolder(false);
    setDeleteDawnFolder(false);
    setDeleteVersionFiles(false);
    setShowDeleteConfirm(true);
  };
  const isLunarInst = (i) => i.meta?.source === "lunar" || (i.meta?.source === "client" && i.meta?.client === "lunar");
  const isFeatherInst = (i) => i.meta?.source === "client" && i.meta?.client === "feather";
  const isDawnInst = (i) => i.meta?.source === "client" && i.meta?.client === "dawn";
  const confirmDelete = () => {
    const doomed = pendingDelete;
    setShowDeleteConfirm(false);
    const ids = new Set(doomed.map((i) => i.id));
    Promise.all(doomed.map((i) => DeleteInstance(i.name).catch(() => {
    }))).then(() => {
      const promises = [];
      if (deleteLunarFolder && doomed.some(isLunarInst)) promises.push(DeleteLunarFolder());
      if (deleteFeatherFolder && doomed.some(isFeatherInst)) promises.push(DeleteFeatherFolder());
      if (deleteDawnFolder && doomed.some(isDawnInst)) promises.push(DeleteDawnFolder());
      if (deleteVersionFiles && doomed.length) promises.push(PruneOrphanedVersions(doomed.map((i) => i.name)));
      return Promise.all(promises.map(p => p.catch(() => {})));
    }).finally(() => {
      const remaining = groups.flatMap((g) => g.instances).filter((i) => !ids.has(i.id));
      setGroups(groups.filter((g) => g.instances.some((i) => !ids.has(i.id))).map((g) => ({ ...g, instances: g.instances.filter((i) => !ids.has(i.id)) })));
      if (remaining.length && !remaining.some((i) => i.id === homeInstId)) {
        const next = [...remaining].sort((a, b) => a.id.localeCompare(b.id))[0]?.id || "";
        setHomeInstId(next);
        setSettings((s2) => ({ ...s2, lastPlayed: s2.lastPlayed && ids.has(s2.lastPlayed) ? next : s2.lastPlayed }));
      } else if (!remaining.length) {
        setHomeInstId("");
        setSettings((s2) => ({ ...s2, lastPlayed: ids.has(s2.lastPlayed) ? "" : s2.lastPlayed }));
      } else if (ids.has(settings.lastPlayed)) {
        setSettings((s2) => ({ ...s2, lastPlayed: s2.lastPlayed && ids.has(s2.lastPlayed) ? homeInstId : s2.lastPlayed }));
      }
      setDeleteMode(false);
      setDeleteSel([]);
      setDeleteGroups([]);
      setPendingDelete([]);
    });
  };
  const openInstModal = () => {
    setInstTab("vanilla");
    setInstMeta({});
    setInstName("");
    setInstGroup(groups[0]?.id || "");
    setShowInstModal(true);
  };
  const moveInstance = (instId, targetGroupId) => {
    setGroups(groups.map((g) => {
      if (g.id === targetGroupId && !g.instances.some((i) => i.id === instId)) {
        const src = groups.find((s2) => s2.instances.some((i) => i.id === instId));
        const inst = src?.instances.find((i) => i.id === instId);
        return inst ? { ...g, instances: [...g.instances, inst] } : g;
      }
      if (g.instances.some((i) => i.id === instId) && g.id !== targetGroupId) {
        return { ...g, instances: g.instances.filter((i) => i.id !== instId) };
      }
      return g;
    }));
  };
  const displayName = (inst, group) => {
    const sameName = group.instances.filter((i) => i.name === inst.name);
    if (sameName.length < 2) return inst.name;
    const rank = sameName.indexOf(inst) + 1;
    return rank === 1 ? inst.name : `${inst.name} (${rank})`;
  };
  const loaderBadge = (inst) => {
    const m = inst.meta;
    if (m?.source === "vanilla") return m?.loader && m.loader !== "Vanilla" ? m.loader : "Vanilla";
    if (m?.source === "modrinth") return "Modrinth";
    if (m?.source === "lunar" || m?.source === "client") return clientName(m.client || "lunar");
    return "CurseForge";
  };
  const homeInstName = () => {
    if (!homeInstId) return "";
    for (const g of groups) {
      const inst = g.instances.find((i) => i.id === homeInstId);
      if (inst) return displayName(inst, g);
    }
    return "";
  };
  const instVersion = (inst) => {
    const m = inst.meta;
    if (m.source === "vanilla") return m.loader && m.loader !== "Vanilla" ? `${m.mcVersion} \xB7 ${m.loader}` : m.mcVersion;
    if (m.source === "modrinth") return m.projectTitle || m.subtitle || "";
    if (m.source === "curseforge") return m.modName || m.subtitle || "";
    if (m.source === "lunar" || m.source === "client") return m.subtitle || `${clientName(m.client || "lunar")} \xB7 ${m.module || "default"} \xB7 ${m.mcVersion}`;
    return m.subtitle || m.mcVersion || "";
  };
  const homeInstVersion = () => {
    if (!homeInstId) return "";
    for (const g of groups) {
      const inst = g.instances.find((i) => i.id === homeInstId);
      if (inst) return instVersion(inst);
    }
    return "";
  };
  const homeInstIcon = () => {
    if (!homeInstId) return "";
    for (const g of groups) {
      const inst = g.instances.find((i) => i.id === homeInstId);
      if (inst) return inst.meta?.icon || "";
    }
    return "";
  };
  const homeInst = () => {
    if (!homeInstId) return null;
    for (const g of groups) {
      const inst = g.instances.find((i) => i.id === homeInstId);
      if (inst) return { ...inst, groupName: g.name };
    }
    return null;
  };
  const playGame = async () => {
    const inst = homeInst();
    if (!inst || launching) return;
    if (!accounts.length || !activeAccountId) {
      setAccLoginPrompt("Log in to an account (offline or Microsoft) to launch the game. Add and select an account below.");
      setActive("accounts");
      return;
    }
    setSettings((s2) => ({ ...s2, lastPlayed: inst.id }));
    setGameError("");
    setGameLog([]);
    setGameProgress(null);
    setLaunching(true);
    try {
      const meta = { ...inst.meta };
      if (meta.source === "modrinth" || meta.source === "curseforge") {
        const url = meta.source === "modrinth" ? meta.fileUrl : meta.downloadUrl;
        if (url) {
          const info = await InstallModpack(inst.name, meta.source, url);
          meta.mcVersion = info.mcVersion || meta.mcVersion;
          meta.loader = info.loader || meta.loader || "Vanilla";
          meta.loaderVersion = info.loaderVersion || meta.loaderVersion || "";
          updateInstanceMeta(inst.id, meta);
        }
      }
      const ov = inst.meta.overrides || {};
      const s2 = { ...settings };
      if (ov.java?.on) s2.javaPath = ov.java.mode === "custom" ? ov.java.javaPath || "" : "";
      if (ov.memory?.on) {
        s2.memory = ov.memory.memory;
        s2.minMemory = ov.memory.minMemory || 0;
        s2.jvmArgs = ov.memory.jvmArgs || "";
      }
      if (ov.window?.on) {
        s2.winWidth = ov.window.winWidth;
        s2.winHeight = ov.window.winHeight;
        s2.fullscreen = !!ov.window.fullscreen;
      }
      if (ov.server?.on) s2.server = ov.server.server || "";
      if (ov.env?.on) s2.env = ov.env.env || "";
      if (ov.commands?.on) {
        s2.preLaunch = ov.commands.preLaunch || "";
        s2.postExit = ov.commands.postExit || "";
      }
      const width = s2.winWidth || 1280;
      const height = s2.winHeight || 720;
      const opts = {
        memory: `${s2.memory || 2}G`,
        minMemory: s2.minMemory ? `${s2.minMemory}G` : "",
        jvmArgs: s2.jvmArgs || "",
        javaPath: s2.javaPath || "",
        width,
        height,
        fullscreen: !!s2.fullscreen,
        server: s2.server || "",
        env: (s2.env || "").split("\n").map((v) => v.trim()).filter(Boolean),
        preLaunch: s2.preLaunch || "",
        postExit: s2.postExit || "",
        accountId: ov.account?.on ? ov.account.accountId || "" : ""
      };
      if (meta.source === "lunar" || meta.source === "client") {
        const clientId = meta.client || "lunar";
        await LaunchClientInstance(inst.name, clientId, meta.mcVersion, meta.module || (clientId === "lunar" ? "lunar" : ""), opts);
      } else {
        await LaunchInstance(inst.name, meta.mcVersion, meta.loader || "Vanilla", meta.loaderVersion || "", opts);
      }
    } catch (e) {
      setGameError(String(e));
      setLaunching(false);
    }
  };
  const stopGame = async () => {
    try {
      await StopInstance();
    } catch (e) {
      setGameError(String(e));
    }
  };
  const fmtBytes = (n) => {
    if (!n && n !== 0) return "";
    if (n >= 1048576) return `${(n / 1048576).toFixed(1)} MB`;
    if (n >= 1024) return `${(n / 1024).toFixed(0)} KB`;
    return `${n} B`;
  };
  const fmtSpeed = (n) => {
    if (!n) return "";
    if (n >= 1048576) return `${(n / 1048576).toFixed(1)} MB/s`;
    if (n >= 1024) return `${(n / 1024).toFixed(0)} KB/s`;
    return `${n} B/s`;
  };
  const setInstFromVanilla = () => {
    const name = instName.trim() || `${loader} ${mcVersion}`;
    return {
      source: "vanilla",
      mcVersion,
      loader,
      loaderVersion: loader === "Vanilla" ? "" : loaderVersion,
      subtitle: loader === "Vanilla" ? mcVersion : `${loader} ${loaderVersion}`
    };
  };
  const setInstFromModrinth = () => {
    const v = mrVersions.find((x) => x.id === mrVersion);
    if (!mrSelected || !v) return null;
    const name = instName.trim() || mrSelected.title;
    return {
      source: "modrinth",
      projectId: mrSelected.project_id,
      projectTitle: mrSelected.title,
      versionId: v.id,
      versionNumber: v.version_number,
      mcVersion: v.game_versions?.[0] || "",
      icon: mrSelected.icon_url || "",
      fileUrl: v.files?.find((f) => f.primary)?.url || v.files?.[0]?.url || "",
      subtitle: `${v.version_number} \xB7 ${(v.loaders || []).join(", ")}`
    };
  };
  const setInstFromCurseForge = () => {
    const f = cfFiles.find((x) => String(x.id) === cfFile);
    if (!cfSelected || !f) return null;
    const name = instName.trim() || cfSelected.name;
    return {
      source: "curseforge",
      modId: cfSelected.id,
      modName: cfSelected.name,
      fileId: f.id,
      fileName: f.fileName,
      downloadUrl: f.downloadUrl,
      icon: cfSelected.logo?.url || "",
      mcVersion: (f.gameVersions || []).find((gv) => /^\d+\.\d+/.test(gv)) || "",
      subtitle: `${f.displayName || f.fileName} \xB7 ${(f.gameVersions || []).join(", ")}`
    };
  };
  const setInstFromClient = () => {
    if (!client || !clientVersion) return null;
    const name = instName.trim() || `${clientName(client)} ${clientVersion}`;
    const loader2 = clientLoader || (clientModule === "fabric" ? "Fabric" : clientModule === "forge" ? "Forge" : void 0);
    return {
      source: "client",
      client,
      mcVersion: clientVersion,
      module: clientModule,
      loader: loader2,
      subtitle: `${clientName(client)} \xB7 ${clientModule || "default"} \xB7 ${clientVersion}`
    };
  };
  const createBtn = async () => {
    const meta = instTab === "vanilla" ? setInstFromVanilla() : instTab === "modrinth" ? setInstFromModrinth() : instTab === "curseforge" ? setInstFromCurseForge() : setInstFromClient();
    if (!meta) return;
    if (instIcon) meta.icon = instIcon;
    addInstance(meta);
  };
  const coreViewIds = icons.map((i) => i.id);
  const hiddenCoreViews = coreViewIds.filter((id) => getOverride("view:" + id)?.mode === "hide");
  const pluginTabs = pluginSidebarViews(coreViewIds);
  const activePluginView = getView(active) && (!coreViewIds.includes(active) || getOverride("view:" + active)?.mode === "replace") ? getView(active) : null;
  return <div className="container">
            {settingsInst ? <div key="inst-settings" className="isw-view view-enter">
                    <div className="isw-topbar">
                        <button className="isw-back" onClick={closeInstSettings} title="Back to instances">
                            <i className="ph ph-arrow-left" />
                        </button>
                        <div className="isw-id">
                            <label className="isw-avatar" title="Change icon">
                                <input type="file" accept="image/*" hidden onChange={onSettingsIconPicked} />
                                <InstIcon inst={settingsInst} />
                                <span className="isw-avatar-edit"><i className="ph ph-pencil" /></span>
                            </label>
                            <div className="isw-id-text">
                                <input
    className="isw-name-input"
    value={settingsInst.meta.overrides?.general?.name ?? settingsInst.name}
    onChange={(e) => setGeneral({ name: e.target.value })}
  />
                                <span className="isw-sub">{instVersion(settingsInst)}</span>
                            </div>
                        </div>
                        <div className="isw-topbar-actions">
                            <button className="isw-action" title="Open instance folder" onClick={openInstFolder}><i className="ph ph-folder-open" /></button>
                            <button className="isw-action" title="Duplicate instance" onClick={duplicateInst}><i className="ph ph-copy" /></button>
                            <button className="isw-action" title="Export as pack" onClick={exportInst}><i className="ph ph-export" /></button>
                        </div>
                    </div>
                    {iswMsg && <div className="isw-msg">{iswMsg}</div>}
                    <div className="isw-tabs">
                        {(settingsInst.meta?.loader === "Vanilla" || !settingsInst.meta?.loader ? [
    ["resourcepacks", "Resource Packs", "ph-image"],
    ["worlds", "Worlds", "ph-globe"],
    ["screenshots", "Screenshots", "ph-camera"],
    ["settings", "Settings", "ph-sliders"]
  ] : [
    ["mods", "Mods", "ph-puzzle-piece"],
    ["resourcepacks", "Resource Packs", "ph-image"],
    ["shaderpacks", "Shaders", "ph-sparkle"],
    ["worlds", "Worlds", "ph-globe"],
    ["screenshots", "Screenshots", "ph-camera"],
    ["settings", "Settings", "ph-sliders"]
  ]).map(([id, label, icon]) => <button
    key={id}
    className={`isw-tab${instSettingsTab === id ? " active" : ""}`}
    onClick={() => setInstSettingsTab(id)}
  >
                                <i className={`ph ${icon}`} />
                                <span>{label}</span>
                            </button>)}
                    </div>
                    <div className="isw-content">
                        {instSettingsTab === "mods" && <ContentTab instName={settingsInst.name} subfolder="mods" facet="mod" searchable mcVersion={settingsInst.meta?.mcVersion} instLoader={settingsInst.meta?.loader} />}
                        {instSettingsTab === "resourcepacks" && <ContentTab instName={settingsInst.name} subfolder="resourcepacks" facet="resourcepack" searchable mcVersion={settingsInst.meta?.mcVersion} instLoader={settingsInst.meta?.loader} />}
                        {instSettingsTab === "shaderpacks" && <ContentTab instName={settingsInst.name} subfolder="shaderpacks" facet="shaderpack" searchable mcVersion={settingsInst.meta?.mcVersion} instLoader={settingsInst.meta?.loader} />}
                        {instSettingsTab === "worlds" && <WorldsTab instName={settingsInst.name} mcVersion={settingsInst.meta?.mcVersion} />}
                        {instSettingsTab === "screenshots" && <ScreenshotsTab instName={settingsInst.name} />}
                        {instSettingsTab === "settings" && <InstSettingsTab inst={settingsInst} settings={settings} onSave={(group, value) => updateInstanceMeta(settingsInst.id, { overrides: { ...(settingsInst.meta.overrides || {}), [group]: value } })} />}
                    </div>
                    </div> : activePluginView ? <PluginViewHost key={active} view={activePluginView} /> : active === "instances" ? <div key="instances" className="instances-view view-enter">
                    <div className="topbar">
                        <Slot name="topbar" />
                        {deleteMode ? <>
                                <span className="del-hint">Select groups or instances to delete</span>
                                <button className="topbar-btn danger" onClick={deleteInstances} disabled={!deleteSel.length && !deleteGroups.length}>
                                    <i className="ph ph-trash" />
                                    <span>Delete ({deleteSel.length + deleteGroups.length})</span>
                                </button>
                                <button className="topbar-btn" onClick={cancelDelete}>
                                    <span>Cancel</span>
                                </button>
                            </> : <>
                                <button className="topbar-btn" onClick={openInstModal}>
                                    <i className="ph ph-plus" />
                                    <span>New instance</span>
                                </button>
                                <button className="topbar-btn" onClick={() => setShowGroupModal(true)}>
                                    <i className="ph ph-folder-plus" />
                                    <span>New group</span>
                                </button>
                                <button className="topbar-btn danger" onClick={openDeleteModal}>
                                    <i className="ph ph-trash" />
                                    <span>Delete</span>
                                </button>
                            </>}
                    </div>
                    <div className="instances-content">
                        {groups.map((group) => {
    const groupLocked = group.default;
    return <div
      key={group.id}
      className={`group${group.expanded ? " active" : ""}${dragOverGroup === group.id ? " drop-target" : ""}${deleteGroups.includes(group.id) ? " marked-delete" : ""}`}
      onDragEnter={(e) => {
        e.preventDefault();
        setDragOverGroup(group.id);
      }}
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault();
        e.stopPropagation();
        const id = dragId;
        setDragId(null);
        setDragOverGroup(null);
        if (id) moveInstance(id, group.id);
      }}
    >
                                <div
      className={`group-header${deleteMode && !groupLocked ? " selectable" : ""}${deleteGroups.includes(group.id) ? " selected" : ""}${groupLocked && deleteMode ? " locked" : ""}`}
      onClick={deleteMode && !groupLocked ? () => toggleDeleteGroup(group.id) : () => toggleGroup(group.id)}
    >
                                    {deleteMode ? groupLocked ? <i className="ph ph-lock-simple group-lock" /> : <span className={`group-check${deleteGroups.includes(group.id) ? " checked" : ""}`}>
                                                {deleteGroups.includes(group.id) && <i className="ph ph-check" />}
                                            </span> : <i className={`ph ph-caret-right group-chevron${group.expanded ? " open" : ""}`} />}
                                    {renameId === group.id ? <input
      className="group-rename-input"
      value={renameName}
      autoFocus
      onClick={(e) => e.stopPropagation()}
      onInput={(e) => setRenameName(e.target.value)}
      onBlur={cancelRename}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          saveRename();
        }
        if (e.key === "Escape") {
          e.preventDefault();
          cancelRename();
        }
      }}
    /> : <>
                                            <span
      className="group-name"
      title="Double-click to rename"
      onDoubleClick={(e) => {
        if (!deleteMode) {
          e.stopPropagation();
          startRename(group);
        }
      }}
    >
                                                {group.name}
                                            </span>
                                            {!deleteMode && <button
      className="group-rename-btn"
      title="Rename"
      onClick={(e) => {
        e.stopPropagation();
        startRename(group);
      }}
    >
                                                    <i className="ph ph-pencil-simple" />
                                                </button>}
                                        </>}
                                    <span className="group-count">{group.instances.length}</span>
                                </div>
                                {group.expanded && <div className="instance-grid">
                                        {group.instances.map((inst) => {
      const groupMarked = deleteGroups.includes(group.id);
      const instMarked = deleteSel.includes(inst.id) || groupMarked;
      return <div
        key={inst.id}
        className={`instance-card${dragId === inst.id ? " dragging" : ""}${deleteMode ? " selectable" : ""}${instMarked ? " selected" : ""}`}
        draggable={!deleteMode}
        title={deleteMode ? void 0 : "Instance settings"}
        onClick={deleteMode && !groupMarked ? () => toggleDeleteSel(inst.id) : !deleteMode ? () => openInstSettings(inst) : void 0}
        onDragStart={() => setDragId(inst.id)}
        onDragEnd={() => {
          setDragId(null);
          setDragOverGroup(null);
        }}
      >
                                                {deleteMode && <span className={`instance-check${instMarked ? " checked" : ""}`}>
                                                        {instMarked && <i className="ph ph-check" />}
                                                    </span>}
                                                <div className="instance-card-head">
                                                    <InstIcon inst={inst} />
                                                    <div className="instance-card-info">
                                                        <ScrollingName text={displayName(inst, group)} />
                                                        <span className="instance-version">{instVersion(inst)}</span>
                                                    </div>
                                                </div>
                                                <div className="instance-card-footer">
                                                    <span className="instance-badge">{loaderBadge(inst)}</span>
                                                    {!deleteMode && <>
                                                            {getMenuItems("instance").map((mi) => <button
        key={mi.id}
        className="inst-edit-btn"
        title={mi.label}
        onClick={(e) => {
          e.stopPropagation();
          try {
            mi.onClick({ instance: inst });
          } catch (err) {
            console.error(err);
          }
        }}
      >
                                                                    <i className={`ph ph-${mi.icon || "puzzle-piece"}`} />
                                                                </button>)}
                                                            <button
        className="inst-edit-btn"
        title="Instance settings"
        onClick={(e) => {
          e.stopPropagation();
          openInstSettings(inst);
        }}
      >
                                                                <i className="ph ph-gear" />
                                                            </button>
                                                        </>}
                                                </div>
                                                <Slot name="instance-card" ctx={{ instance: inst }} />
                                            </div>;
    })}
                                        {!group.instances.length && <div className="group-empty">No instances yet</div>}
                                    </div>}
                            </div>;
  })}
                    </div>
                </div> : active === "accounts" ? <div key="accounts" className="settings-view view-enter">
                    <div className="topbar">
                        <span className="topbar-title">Accounts</span>
                    </div>
                    <div className="settings-content">
                        <div className="settings-section acc-wide">
                            {accLoginPrompt && <div className="settings-row-desc acc-login-prompt">{accLoginPrompt}</div>}
                            <div className="settings-section-title acc-section-head">
                                <span>Saved accounts</span>
                                <div className="acc-head-actions">
                                    <button className="acc-add-btn" onClick={() => {
    setAccError("");
    setMsError("");
    setAccPicked(null);
    setShowAccModal(true);
  }} title="Add account">
                                        <i className="ph ph-plus" />
                                    </button>
                                </div>
                            </div>
                            {accounts.length === 0 && <div className="settings-row-desc">No accounts yet. Add an offline or Microsoft account above.</div>}
                            {accounts.map((acc) => <div key={acc.id} className={`account-card${acc.id === activeAccountId ? " active" : ""}`}>
                                    <AccountAvatar acc={acc} />
                                    <div className="acc-info">
                                        <div className="acc-name">
                                            {acc.name}
                                            {acc.id === activeAccountId && <span className="acc-badge">Active</span>}
                                        </div>
                                        <div className="acc-meta">
                                            {acc.type === "microsoft" ? "Microsoft account" : "Offline account"}
                                        </div>
                                        <div className="acc-meta">
                                            {acc.id === activeAccountId ? "This account is used when launching the game" : "Click Use to launch the game with this account"}
                                        </div>
                                    </div>
                                    <div className="acc-actions">
                                        {acc.id !== activeAccountId && <button className="acc-action-btn" onClick={() => setActiveAcc(acc.id)}>
                                                <i className="ph ph-check" /> <span>Use</span>
                                            </button>}
                                        <button className="acc-action-btn danger" onClick={() => delAccount(acc.id)}>
                                            <i className="ph ph-trash" /> <span>Delete</span>
                                        </button>
                                    </div>
                                </div>)}
                        </div>
                    </div>
                </div> : active === "skins" ? <div key="skins" className="settings-view view-enter">
                    <div className="topbar">
                        <span className="topbar-title">Skin</span>
                    </div>
                    <div className="settings-content">
                        <div className="settings-section">
                            <div className="settings-section-title">Editor</div>
                            {accounts.length === 0 ? <div className="settings-row-desc">
                                    No accounts yet. Add an account in the Accounts tab to preview and edit its skin.
                                </div> : <>
                                    <div className="skin-pick-row">
                                        <select
    className="modal-select skin-select"
    value={skinAccountId || activeAccountId}
    onChange={(e) => setSkinAccountId(e.target.value)}
  >
                                            {accounts.map((acc) => <option key={acc.id} value={acc.id}>
                                                    {acc.name} ({acc.type === "microsoft" ? "Microsoft" : "Offline"})
                                                </option>)}
                                        </select>
                                        {accounts.length === 1 && <span className="settings-row-desc">Only one account. Add more in the Accounts tab.</span>}
                                    </div>
                                    {(() => {
    const acc = accounts.find((a) => a.id === (skinAccountId || activeAccountId)) || accounts[0];
    if (!acc) return null;
    const isMicrosoft = acc.type === "microsoft";
    const previewUrl = isMicrosoft && skinFile ? skinFile.url : `https://minotar.net/skin/${isMicrosoft ? acc.id : STEVE_UUID}`;
    return <div className="skin-preview">
                                                <SkinViewer3D skinUrl={previewUrl} model={skinFile ? (skinVariant === "SLIM" ? "slim" : "default") : "auto-detect"} />
                                                <div className="skin-info">
                                                    <div className="acc-name">{acc.name}</div>
                                                    <div className="acc-meta">
                                                        {isMicrosoft ? "Microsoft account" : "Offline account \u2014 always shows the Steve skin"}
                                                    </div>
                                                    {isMicrosoft ? <div className="skin-upload">
                                                            <input
      id="skin-file-input"
      type="file"
      accept="image/png"
      hidden
      onChange={onSkinFilePicked}
    />
                                                            <label className="modal-btn skin-open-btn" htmlFor="skin-file-input">
                                                                <i className="ph ph-upload-simple" /> Choose PNG
                                                            </label>
                                                            {skinFile && <select
      className="modal-select"
      value={skinVariant}
      onChange={(e) => setSkinVariant(e.target.value)}
      title="Skin model: Classic (4px arms) or Slim (3px arms)"
    >
                                                                <option value="CLASSIC">Classic</option>
                                                                <option value="SLIM">Slim</option>
                                                            </select>}
                                                            {skinFile && skinFile.base64 && <div className="skin-upload-actions">
                                                                    <span className="acc-meta">{skinFile.name}</span>
                                                                    <button
      className="acc-action-btn"
      onClick={uploadSkin}
      disabled={skinUploading}
    >
                                                                        <i className="ph ph-cloud-arrow-up" /> {skinUploading ? "Uploading\u2026" : "Upload skin"}
                                                                    </button>
                                                                </div>}
                                                            {skinFile && !skinFile.base64 && <div className="skin-upload-actions">
                                                                    <span className="acc-meta">{skinFile.name}</span>
                                                                    <button
      className="acc-action-btn"
      onClick={() => useLocalSkin(skinFile)}
      disabled={skinUploading}
    >
                                                                        <i className="ph ph-check" /> {skinUploading ? "Setting\u2026" : "Use saved skin"}
                                                                    </button>
                                                                </div>}
                                                        </div> : <div className="acc-meta skin-offline-note">
                                                            Offline accounts can't change their skin — it always shows Steve. Use a Microsoft account to customize.
                                                        </div>}
                                                    {skinMsg && <div className={`skin-msg${skinMsgOk ? " ok" : ""}`}>{skinMsg}</div>}
                                                </div>
                                            </div>;
  })()}
                                    <div className="settings-section-title" style={{ marginTop: 24 }}>
                                        Saved skins ({localSkins.length})
                                    </div>
                                    <div className="settings-row-desc">
                                        Skins uploaded from this launcher are stored locally in
                                        {" "}%APPDATA%\.multilauncher\assets\skins. Click one to preview it.
                                    </div>
                                    {localSkins.length === 0 ? <div className="acc-meta">No saved skins yet. Upload a PNG above to save it here.</div> : <div className="local-skin-grid">
                                            {localSkins.map((s2) => <div
    key={s2.name}
    role="button"
    tabIndex={0}
    className={`local-skin-card${skinFile && skinFile.url === s2.url ? " active" : ""}`}
    onClick={() => setSkinFile({ name: s2.name, url: s2.url })}
    title={s2.name}
  >
                                                    <SkinViewer3D skinUrl={s2.url} mini />
                                                    <button
    className="local-skin-del"
    title="Delete saved skin"
    onClick={(e) => deleteLocalSkin(s2, e)}
  >
                                                        <i className="ph ph-trash" />
                                                    </button>
                                                    <span className="local-skin-name">{s2.name}</span>
                                                </div>)}
                                        </div>}
                                </>}
                        </div>
                    </div>
                </div> : active === "plugins" ? <PluginsPanel /> : active === "settings" ? <div key="settings" className="settings-view view-enter">
                    <div className="settings-nav">
                        {["background", "launcher", "minecraft", "java", "tweaks", "storage"].map((s2) => <button
    key={s2}
    className={`settings-nav-btn${settingsTab === s2 ? " active" : ""}`}
    onClick={() => {
      setSettingsTab(s2);
      if (s2 === "storage") loadStorage();
    }}
  >
                                {s2.charAt(0).toUpperCase() + s2.slice(1)}
                            </button>)}
                    </div>
                    <div className="settings-content">
                        {settingsTab === "background" && <div className="settings-section">
                            <div className="settings-section-title">Panorama background</div>
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Background</div>
                                    <div className="settings-row-desc">Panorama: animated version background. Instance icon: selected instance icon.</div>
                                </div>
                                <select
    className="modal-select"
    value={settings.bg || "panorama"}
    onChange={(e) => setSettings({ ...settings, bg: e.target.value })}
  >
                                    <option value="panorama">Panorama</option>
                                    <option value="icon">Instance icon</option>
                                </select>
                            </div>
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Animation</div>
                                    <div className="settings-row-desc">Animated: rotating panorama. Paused: frozen view. Off: plain background.</div>
                                </div>
                                <select
    className="modal-select"
    value={settings.panoramaMode}
    onChange={(e) => setSettings({ ...settings, panoramaMode: e.target.value })}
  >
                                    <option value="animate">Animated</option>
                                    <option value="paused">Paused</option>
                                    <option value="off">Off</option>
                                </select>
                            </div>
                        </div>}
                        {settingsTab === "launcher" && <div className="settings-section">
                            <div className="settings-section-title">Launcher</div>
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Instance picker</div>
                                    <div className="settings-row-desc">How instances are shown next to the Play button: list or grid of cards.</div>
                                </div>
                                <select
    className="modal-select"
    value={settings.pickerLayout || "list"}
    onChange={(e) => setSettings({ ...settings, pickerLayout: e.target.value })}
  >
                                    <option value="list">List</option>
                                    <option value="grid">Grid</option>
                                </select>
                            </div>
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Download threads</div>
                                    <div className="settings-row-desc">Auto adjusts to your connection. Manual uses a fixed count. OFF disables optimized downloading (1 thread).</div>
                                </div>
                                <div style={{ display: "flex", alignItems: "center", gap: "12px" }}>
                                    <div className="dl-mode-toggle">
                                        <button
    type="button"
    className={`dl-mode-btn${settings.dlMode === "auto" ? " active" : ""}`}
    onClick={() => {
      setSettings((s2) => ({ ...s2, dlMode: "auto" }));
      SetDownloadConcurrency(0);
    }}
  >
                                            Auto
                                        </button>
                                        <button
    type="button"
    className={`dl-mode-btn${settings.dlMode === "manual" ? " active" : ""}`}
    onClick={() => {
      setSettings((s2) => ({ ...s2, dlMode: "manual" }));
      SetDownloadConcurrency(s.concurrency || 8);
    }}
  >
                                            Manual
                                        </button>
                                        <button
    type="button"
    className={`dl-mode-btn${settings.dlMode === "off" ? " active" : ""}`}
    onClick={() => {
      setSettings((s2) => ({ ...s2, dlMode: "off" }));
      SetDownloadConcurrency(1);
    }}
  >
                                            OFF
                                        </button>
                                    </div>
                                    <input
    type="range"
    min="1"
    max="32"
    disabled={settings.dlMode !== "manual"}
    value={settings.dlMode === "off" ? 1 : settings.concurrency || 8}
    onInput={(e) => {
      const v = parseInt(e.target.value);
      setSettings((s2) => ({ ...s2, concurrency: v }));
    }}
    onChange={(e) => {
      const v = parseInt(e.target.value);
      setSettings((s2) => ({ ...s2, concurrency: v }));
      SetDownloadConcurrency(v);
    }}
  />
                                    <span style={{ minWidth: "24px", textAlign: "right", fontSize: "13px" }}>
                                        {settings.dlMode === "auto" ? "\u221E" : settings.dlMode === "off" ? "1" : settings.concurrency || 8}
                                    </span>
                                </div>
                            </div>
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Open logs on launch</div>
                                    <div className="settings-row-desc">Automatically opens the log panel when the game process starts.</div>
                                </div>
                                <label className="settings-check">
                                    <input
    type="checkbox"
    checked={!!settings.autoLogs}
    onChange={(e) => setSettings((s2) => ({ ...s2, autoLogs: e.target.checked }))}
  />
                                </label>
                            </div>
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Minimize launcher on game start</div>
                                    <div className="settings-row-desc">When the game fully launches, the launcher minimizes to the taskbar and runs in the background. When you quit the game, the window comes back on its own.</div>
                                </div>
                                <label className="settings-check">
                                    <input
    type="checkbox"
    checked={!!settings.minimizeOnLaunch}
    onChange={(e) => setSettings((s2) => ({ ...s2, minimizeOnLaunch: e.target.checked }))}
  />
                                </label>
                            </div>
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Quit launcher on game start</div>
                                    <div className="settings-row-desc">When the game fully launches, the launcher closes completely. Start it again manually after quitting the game.</div>
                                </div>
                                <label className="settings-check">
                                    <input
    type="checkbox"
    checked={!!settings.quitOnLaunch}
    onChange={(e) => setSettings((s2) => ({ ...s2, quitOnLaunch: e.target.checked }))}
  />
                                </label>
                            </div>
                        </div>}
                        {settingsTab === "minecraft" && <div className="settings-section mc-section">
                            <div className="settings-section-title">Minecraft</div>
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Window size</div>
                                    <div className="settings-row-desc">Resolution used for --width/--height. Fullscreen starts the game in fullscreen mode.</div>
                                </div>
                                <div className="win-control">
                                    <input
    type="number"
    className="jvm-input win-num"
    min="320"
    max="7680"
    value={settings.winWidth || 1280}
    disabled={!!settings.fullscreen}
    onChange={(e) => setSettings((s2) => ({ ...s2, winWidth: parseInt(e.target.value) || 0 }))}
  />
                                    <span className="win-x">×</span>
                                    <input
    type="number"
    className="jvm-input win-num"
    min="240"
    max="4320"
    value={settings.winHeight || 720}
    disabled={!!settings.fullscreen}
    onChange={(e) => setSettings((s2) => ({ ...s2, winHeight: parseInt(e.target.value) || 0 }))}
  />
                                    <label className="settings-check">
                                        <input
    type="checkbox"
    checked={!!settings.fullscreen}
    onChange={(e) => setSettings((s2) => ({ ...s2, fullscreen: e.target.checked }))}
  />
                                        Fullscreen
                                    </label>
                                </div>
                            </div>
                            <div className="settings-row jvm-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Join server on launch</div>
                                    <div className="settings-row-desc">Server address (host:port). Default port 25565, e.g. mc.hypixel.net:25565.</div>
                                </div>
                                <input
    type="text"
    className="jvm-input"
    placeholder="mc.hypixel.net:25565"
    value={settings.server || ""}
    onChange={(e) => setSettings((s2) => ({ ...s2, server: e.target.value }))}
  />
                            </div>
                            <div className="settings-row jvm-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Environment variables</div>
                                    <div className="settings-row-desc">KEY=VALUE, one per line. Added to the game process environment.</div>
                                </div>
                                <textarea
    className="jvm-input env-input"
    rows="3"
    placeholder={"MALLOC_ARENA_MAX=2\n__GL_THREADED_OPTIMISATIONS=1"}
    value={settings.env || ""}
    onChange={(e) => setSettings((s2) => ({ ...s2, env: e.target.value }))}
  />
                            </div>
                            <div className="settings-row jvm-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Pre-launch command</div>
                                    <div className="settings-row-desc">Run by the shell before the game starts. A failure aborts launching. E.g. clock sync before joining a server.</div>
                                </div>
                                <input
    type="text"
    className="jvm-input"
    placeholder={'w32tm /resync'}
    value={settings.preLaunch || ""}
    onChange={(e) => setSettings((s2) => ({ ...s2, preLaunch: e.target.value }))}
  />
                            </div>
                            <div className="settings-row jvm-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Post-exit command</div>
                                    <div className="settings-row-desc">Run after the game closes, inside the instance folder. E.g. opening your screenshots folder after a session.</div>
                                </div>
                                <input
    type="text"
    className="jvm-input"
    placeholder={'explorer screenshots'}
    value={settings.postExit || ""}
    onChange={(e) => setSettings((s2) => ({ ...s2, postExit: e.target.value }))}
  />
                            </div>
                        </div>}
                        {settingsTab === "java" && <div className="settings-section">
                            <div className="settings-section-title">Java</div>
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Java runtime</div>
                                    <div className="settings-row-desc">Auto: downloaded JREs matched to the game version. Custom: your own java.exe.</div>
                                </div>
                                <div className="java-path-control">
                                    <select
    className="modal-select"
    value={settings.javaPath ? "custom" : "auto"}
    onChange={async (e) => {
      if (e.target.value === "custom") {
        const p = await PickJavaFile().catch(() => "");
        if (p) setSettings((s2) => ({ ...s2, javaPath: p }));
      } else {
        setSettings((s2) => ({ ...s2, javaPath: "" }));
      }
    }}
  >
                                        <option value="auto">Auto (bundled)</option>
                                        <option value="custom">Custom path</option>
                                    </select>
                                    {settings.javaPath && <div className="java-path-row">
                                            <input
    type="text"
    className="jvm-input"
    value={settings.javaPath}
    onChange={(e) => setSettings((s2) => ({ ...s2, javaPath: e.target.value }))}
  />
                                            <button type="button" className="topbar-btn" onClick={async () => {
    const p = await PickJavaFile().catch(() => "");
    if (p) setSettings((s2) => ({ ...s2, javaPath: p }));
  }}>
                                                Browse
                                            </button>
                                            <button type="button" className="topbar-btn" onClick={async () => {
    if (!settings.javaPath) return;
    const [ver, err] = await TestJavaPath(settings.javaPath).then((v) => [v, ""]).catch((e) => ["", String(e)]);
    alert(err || ver.trim());
  }}>
                                                Test
                                            </button>
                                        </div>}
                                </div>
                            </div>
                            <div className="settings-row jvm-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Memory</div>
                                    <div className="settings-row-desc">RAM dla gry: max (-Xmx) i start (-Xms, 0 = nie ustawione).</div>
                                </div>
                                <div className="mem-control">
                                    <div className="mem-top">
                                        <div className={`mem-badge${navigator.deviceMemory && (settings.memory || 2) > Math.floor(navigator.deviceMemory / 2) ? " warn" : ""}`}>
                                            <span className="mem-badge-val">{settings.memory || 2}</span>
                                            <span className="mem-badge-unit">GB</span>
                                        </div>
                                        <input
    type="range"
    className="mem-slider"
    min="1"
    max="16"
    step="1"
    style={{ "--fill": `${((settings.memory || 2) - 1) / 15 * 100}%` }}
    value={settings.memory || 2}
    onInput={(e) => setSettings((s2) => ({ ...s2, memory: parseInt(e.target.value) }))}
    onChange={(e) => setSettings((s2) => ({ ...s2, memory: parseInt(e.target.value) }))}
  />
                                    </div>
                                    <div className="mem-top">
                                        <div className="mem-badge">
                                            <span className="mem-badge-val">{settings.minMemory || 0}</span>
                                            <span className="mem-badge-unit">GB</span>
                                        </div>
                                        <input
    type="range"
    className="mem-slider"
    min="0"
    max="8"
    step="1"
    style={{ "--fill": `${(settings.minMemory || 0) / 8 * 100}%` }}
    value={settings.minMemory || 0}
    onInput={(e) => setSettings((s2) => ({ ...s2, minMemory: parseInt(e.target.value) }))}
    onChange={(e) => setSettings((s2) => ({ ...s2, minMemory: parseInt(e.target.value) }))}
  />
                                    </div>
                                </div>
                            </div>
                            <div className="settings-row jvm-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">GC presets</div>
                                    <div className="settings-row-desc">Fills JVM arguments with a ready-made flag set. Overwrites current values.</div>
                                </div>
                                <div style={{ display: "flex", gap: "8px" }}>
                                    <button type="button" className="topbar-btn" onClick={() => setSettings((s2) => ({ ...s2, jvmArgs: "-XX:+UseG1GC -XX:G1NewSizePercent=20 -XX:G1ReservePercent=20 -XX:MaxGCPauseMillis=50 -XX:G1HeapRegionSize=32M" }))}>G1GC</button>
                                    <button type="button" className="topbar-btn" onClick={() => setSettings((s2) => ({ ...s2, jvmArgs: "-XX:+UseZGC -XX:+ZGenerational" }))}>ZGC</button>
                                    <button type="button" className="topbar-btn" onClick={() => setSettings((s2) => ({ ...s2, jvmArgs: "-XX:+UseParallelGC" }))}>Parallel</button>
                                </div>
                            </div>
                            <div className="settings-row jvm-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">JVM arguments</div>
                                    <div className="settings-row-desc">Extra Java arguments. Spaces separate arguments. Reset clears the field.</div>
                                </div>
                                <div style={{ display: "flex", gap: "8px", alignItems: "center" }}>
                                    <input
    type="text"
    className="jvm-input"
    placeholder="-XX:+UseG1GC -XX:MaxGCPauseMillis=50 -Dsun.rmi.dgc.server.gcInterval=2147483646"
    value={settings.jvmArgs || ""}
    onChange={(e) => setSettings((s2) => ({ ...s2, jvmArgs: e.target.value }))}
  />
                                    {settings.jvmArgs ? <button
    type="button"
    className="topbar-btn"
    onClick={() => setSettings((s2) => ({ ...s2, jvmArgs: "" }))}
  >
                                        Reset
                                    </button> : null}
                                </div>
                            </div>
                        </div>}
                        {settingsTab === "tweaks" && <div className="settings-section">
                            <div className="settings-section-title">Tweaks</div>
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Coming soon</div>
                                    <div className="settings-row-desc">No settings available yet.</div>
                                </div>
                            </div>
                        </div>}
                        {settingsTab === "storage" && <div className="settings-section">
                            <div className="settings-section-title">Storage
                                <button className="modal-btn" onClick={loadStorage}>Refresh</button>
                            </div>
                            {!storage && <div className="st-loading"><i className="ph ph-circle-notch spinning" /> Measuring…</div>}
                            {storage && (() => {
                              const total = storage.reduce((a, c) => a + (c.size || 0), 0) || 1;
                              const colors = ["#7dd3fc", "#a5b4fc", "#c4b5fd", "#f0abfc", "#fda4af", "#fdba74", "#fde68a", "#86efac", "#5eead4", "#fca5a5"];
                              return <>
                                <div className="st-total">
                                  <div className="st-total-num">{fmtDisk(storage.reduce((a, c) => a + (c.size || 0), 0))}</div>
                                  <div className="st-total-label">used by game files · shared downloads are reused by all instances</div>
                                </div>
                                <div className="st-bar" onMouseLeave={() => setStHover(null)}>
                                  {storage.map((c, i) => (c.size > 0) && <div
    key={c.name}
    className="st-seg"
    title={`${c.name}: ${fmtDisk(c.size)}`}
    onMouseEnter={() => setStHover(c.name)}
    style={{ width: `${Math.max(c.size / total * 100, 1)}%`, background: colors[i % colors.length], opacity: !stHover || stHover === c.name ? 1 : 0.35 }}
  />)}
                                </div>
                                {stHover && <div className="st-hover-label">{(() => {
                                  const c = storage.find((x) => x.name === stHover);
                                  return c ? `${c.name}: ${fmtDisk(c.size)} (${(c.size / total * 100).toFixed(1)}%)` : "";
                                })()}</div>}
                                <div className="st-list">
                                  {storage.map((c, i) => <div className="st-row" key={c.name}>
                                    <div className="st-info">
                                      <div className="st-name">{c.name}<span className="st-pct">{c.size > 0 ? `${(c.size / total * 100).toFixed(1)}%` : ""}</span></div>
                                      <div className="st-path" title={c.path}>{c.path}</div>
                                    </div>
                                    <div className="st-size">{fmtDisk(c.size)}</div>
                                  </div>)}
                                </div>
                              </>;
                            })()}
                            <div className="settings-row">
                                <div className="settings-row-info">
                                    <div className="settings-row-name">Clean cache</div>
                                    <div className="settings-row-desc">Removes interrupted downloads (.part) and used loader installers.</div>
                                </div>
                                <button
    className="modal-btn"
    disabled={cleaning}
    onClick={() => {
      setCleaning(true);
      CleanCache().then((r) => {
        flashMsg(`Cleaned ${r.files} files, freed ${fmtDisk(r.freed)}`);
        loadStorage();
      }).catch((e) => flashMsg(String(e))).finally(() => setCleaning(false));
    }}
  >
                                    {cleaning ? "Cleaning…" : "Clean"}
                                </button>
                            </div>
                        </div>}
                    </div>
                </div> : <div key="home" className="home-view view-enter">
                    <div className="panel-left">
                        {settings.bg === "icon" && homeInstIcon() ? <div className="inst-bg" style={{ backgroundImage: `url(${homeInstIcon()})` }}>
                                <div className="panorama-scrim" />
                            </div> : <PanoramaBackground version={panoVersion} mode={settings.panoramaMode} />}
                        {homeInstVersion() && <div className="home-version">{homeInstVersion()}</div>}
                        <button className="home-logs-btn" title="Instance logs" onClick={() => setShowLogs(true)}>
                            <i className="ph ph-terminal-window" />
                        </button>
                        {active === "home" && <div className="home-launch">
                                <div className="home-launch-btns">
                                    {gameRunning ? <button className="play-btn stop" onClick={stopGame}>Stop</button> : <button className={`play-btn${!homeInst() || !accounts.length || !activeAccountId ? " disabled" : ""}`} onClick={playGame} disabled={!homeInst()} title={!homeInst() ? "Create an instance first" : !accounts.length || !activeAccountId ? "Add and select an account to play" : "Play"}>
                                            {launching ? "Starting\u2026" : "Play"}
                                        </button>}
                                    <Slot name="launch-button" />
                                    <button
    className="play-drop-btn"
    title="Choose instance"
    onClick={() => setShowLaunchPicker(!showLaunchPicker)}
  >
                                        <i className={`ph ph-caret-${showLaunchPicker ? "up" : "down"}`} />
                                    </button>
                                    {showLaunchPicker && settings.pickerLayout === "list" && <div className="launch-picker">
                                            {groups.map((g) => <div key={g.id} className="launch-group">
                                                    <div className="launch-group-title">{g.name}</div>
                                                    {g.instances.map((inst) => <div
    key={inst.id}
    className={`launch-item${homeInstId === inst.id ? " selected" : ""}`}
    onClick={() => {
      setHomeInstId(inst.id);
      setShowLaunchPicker(false);
    }}
  >
                                                            <span className="launch-item-name">{displayName(inst, g)}</span>
                                                            <span className="launch-item-ver">{instVersion(inst)}</span>
                                                        </div>)}
                                                    {!g.instances.length && <div className="launch-empty">No instances</div>}
                                                </div>)}
                                        </div>}
                                </div>
                                {launching && !gameRunning && gameProgress?.phase && <div className="modal-overlay">
                                        <div className="dl-modal">
                                            <div className="dl-title">
                                                <span>{gameProgress.phase}</span>
                                            </div>
                                            <div className="game-progress-bar">
                                                <div className="game-progress-fill" style={{ width: `${gameProgress.totalBytes > 0 ? Math.min(100, gameProgress.bytes / gameProgress.totalBytes * 100) : gameProgress.total > 0 ? Math.min(100, gameProgress.current / gameProgress.total * 100) : 0}%` }} />
                                            </div>
                                            <div className="dl-modal-stats">
                                                <span>{gameProgress.total > 0 ? `${gameProgress.current} / ${gameProgress.total} files` : ""}</span>
                                                <span>{gameProgress.speed > 0 ? `${fmtBytes(gameProgress.speed)}/s` : ""}</span>
                                            </div>
                                            <div className="dl-modal-file">
                                                {gameProgress.file || "Preparing\u2026"}
                                            </div>
                                            {gameProgress.files?.length > 0 && <div className="dl-modal-files">
                                                    {gameProgress.files.map((f, i) => <div key={i} className="dl-modal-file-row">
                                                            <span className="dl-modal-file-name">{f.name}</span>
                                                            <span className="dl-modal-file-size">
                                                                {f.speed > 0 && <span className="dl-modal-file-speed">{fmtBytes(f.speed)}/s</span>}
                                                                <span>{fmtBytes(f.bytes)} / {fmtBytes(f.size)}</span>
                                                            </span>
                                                        </div>)}
                                                </div>}
                                            <div className="dl-modal-actions">
                                                <button className="dl-cancel-btn" onClick={() => CancelInstall().catch(() => {
                                                })} title="Cancel installation">
                                                    <i className="ph ph-x-circle" />
                                                    <span>Cancel installation</span>
                                                </button>
                                            </div>
                                        </div>
                                    </div>}
                            </div>}
                    </div>
                </div>}
            {showLogs && <div className="modal-overlay" onClick={() => setShowLogs(false)}>
                    <div className="logs-modal" onClick={(e) => e.stopPropagation()}>
                        <div className="dl-title">
                            <span>Instance logs</span>
                            <div className="dl-title-actions">
                                <button
    className={`log-auto-btn${logAuto ? " on" : ""}`}
    onClick={() => setLogAuto(!logAuto)}
    title="Auto-scroll"
  >
                                    Auto scroll
                                </button>
                                <button className="topbar-btn" onClick={() => setShowLogs(false)} title="Close"><i className="ph ph-x" /></button>
                            </div>
                        </div>
                        <LogView lines={gameLog} showBar={false} auto={logAuto} onAuto={setLogAuto} />
                    </div>
                </div>}
            <div className="sidebar">
                {icons.filter((i) => !hiddenCoreViews.includes(i.id)).map((item) => <button
    key={item.id}
    className={`sidebar-btn${active === item.id ? " active" : ""}`}
    onClick={() => {
      setActive(item.id);
      setInstSettings(null);
    }}
  >
                        {item.icon}
                        <span className="sidebar-label">{item.label}</span>
                    </button>)}
                {pluginTabs.map((item) => <button
    key={item.id}
    className={`sidebar-btn${active === item.id ? " active" : ""}`}
    onClick={() => {
      setActive(item.id);
      setInstSettings(null);
    }}
  >
                        <i className={`ph ${item.icon}`} />
                        <span className="sidebar-label">{item.label}</span>
                    </button>)}
            </div>

            {showLaunchPicker && settings.pickerLayout === "grid" && <div className="modal-overlay" onClick={() => setShowLaunchPicker(false)}>
                    <div className="picker-dialog" onClick={(e) => e.stopPropagation()}>
                        <div className="picker-header">
                            <span className="picker-title">Select instance</span>
                            <button className="topbar-btn" onClick={() => setShowLaunchPicker(false)}>
                                <i className="ph ph-x" />
                            </button>
                        </div>
                        <div className="picker-body">
                            {groups.map((g) => <div key={g.id} className="picker-group">
                                    <div className="launch-group-title">{g.name}</div>
                                    {!g.instances.length && <div className="launch-empty">No instances</div>}
                                    {settings.pickerLayout === "grid" ? <div className="instance-grid">
                                            {g.instances.map((inst) => <div
    key={inst.id}
    className={`instance-card selectable${homeInstId === inst.id ? " selected" : ""}`}
    onClick={() => {
      setHomeInstId(inst.id);
      setShowLaunchPicker(false);
    }}
  >
                                                    <div className="instance-card-head">
                                                        <InstIcon inst={inst} />
                                                        <div className="instance-card-info">
                                                            <ScrollingName text={displayName(inst, g)} />
                                                            <span className="instance-version">{instVersion(inst)}</span>
                                                        </div>
                                                    </div>
                                                </div>)}
                                        </div> : <div className="picker-list">
                                            {g.instances.map((inst) => <div
    key={inst.id}
    className={`picker-row${homeInstId === inst.id ? " selected" : ""}`}
    onClick={() => {
      setHomeInstId(inst.id);
      setShowLaunchPicker(false);
    }}
  >
                                                    <span className="launch-item-name">{displayName(inst, g)}</span>
                                                    <span className="launch-item-ver">{instVersion(inst)}</span>
                                                </div>)}
                                        </div>}
                                </div>)}
                        </div>
                    </div>
                </div>}

            {showAccModal && <div className="modal-overlay" onClick={() => {
    setShowAccModal(false);
    setAccPicked(null);
  }}>
                    <div className="acc-modal" onClick={(e) => e.stopPropagation()}>
                        <div className="picker-header">
                            <span className="picker-title">Add account</span>
                            <button className="topbar-btn" onClick={() => {
    setShowAccModal(false);
    setAccPicked(null);
  }}>
                                <i className="ph ph-x" />
                            </button>
                        </div>
                        <div className="picker-body">
                            {!accPicked ? <div className="acc-pick">
                                    <button className="acc-pick-card" onClick={() => {
    setAccTab("offline");
    setAccPicked("offline");
    setAccError("");
    setMsError("");
  }}>
                                        <i className="ph ph-user" />
                                        <span className="acc-pick-name">Offline</span>
                                        <span className="acc-pick-desc">Play solo or on offline servers. No sign-in needed.</span>
                                    </button>
                                    <button className="acc-pick-card" onClick={() => {
    setAccTab("microsoft");
    setAccPicked("microsoft");
    setAccError("");
    setMsError("");
  }}>
                                        <MsLogo />
                                        <span className="acc-pick-name">Microsoft</span>
                                        <span className="acc-pick-desc">Sign in for online multiplayer with your Microsoft account.</span>
                                    </button>
                                </div> : accTab === "offline" ? <div className="acc-form">
                                    <div className="acc-form-label">Player name</div>
                                    <div className="inst-search acc-name-row">
                                        <input
    className="modal-input acc-name-input"
    placeholder="3-16 characters, A-Z a-z 0-9 _"
    value={offlineName}
    onChange={(e) => setOfflineName(e.target.value)}
    onKeyDown={(e) => {
      if (e.key === "Enter") addOffline();
    }}
  />
                                        <button className="modal-btn primary" onClick={addOffline} disabled={!offlineName.trim()}>
                                            <i className="ph ph-plus" /> Add
                                        </button>
                                    </div>
                                    {accError && <div className="game-error">{accError}</div>}
                                </div> : <div className="acc-form">
                                    {!msLogin ? <button className="modal-btn primary ms-start-btn" onClick={startMsLogin} disabled={!!msLogin}>
                                            <MsLogo /> Sign in with Microsoft
                                        </button> : <div className="ms-login-box">
                                            <div className="ms-login-step">
                                                <span className="ms-step-num">1</span>
                                                <span className="ms-step-text">
                                                    Open <a href={msLogin.verificationUri} target="_blank" rel="noreferrer">microsoft.com/link</a>
                                                </span>
                                            </div>
                                            <div className="ms-login-step">
                                                <span className="ms-step-num">2</span>
                                                <span className="ms-step-text">Enter this code:</span>
                                            </div>
                                            <div className="ms-code-row">
                                                <span className="ms-code">{msLogin.userCode}</span>
                                                <button
    className="modal-btn"
    onClick={() => navigator.clipboard?.writeText(msLogin.userCode)}
    title="Copy code"
  >
                                                    <i className="ph ph-copy" />
                                                </button>
                                            </div>
                                            <div className="ms-waiting"><i className="ph ph-circle-notch spinning" /> Waiting for sign-in…</div>
                                        </div>}
                                    {msError && <div className="game-error">{msError}</div>}
                                </div>}
                        </div>
                    </div>
                </div>}

            <input ref={iconInputRef} type="file" accept="image/*" hidden onChange={onIconPicked} />

{showDeleteConfirm && <div className="modal-overlay" onClick={() => setShowDeleteConfirm(false)}>
                    <div className="modal" onClick={(e) => e.stopPropagation()}>
                        <h3 className="modal-title">Delete {pendingDelete.length} instance{pendingDelete.length === 1 ? "" : "s"}?</h3>
                        <div className="modal-text">Instance files on disk will be deleted permanently. This cannot be undone.</div>
                        {pendingDelete.some(isLunarInst) && <label className="modal-check">
                                <input
                    type="checkbox"
                    checked={deleteLunarFolder}
                    onChange={(e) => setDeleteLunarFolder(e.target.checked)}
                  />
                                Also delete the Lunar Client folder (shared downloads, JRE, textures)
                            </label>}
                        {pendingDelete.some(isFeatherInst) && <label className="modal-check">
                                <input
                    type="checkbox"
                    checked={deleteFeatherFolder}
                    onChange={(e) => setDeleteFeatherFolder(e.target.checked)}
                  />
                                Also delete the Feather folder (shared downloads, JRE, assets)
                            </label>}
                        {pendingDelete.some(isDawnInst) && <label className="modal-check">
                                <input
                    type="checkbox"
                    checked={deleteDawnFolder}
                    onChange={(e) => setDeleteDawnFolder(e.target.checked)}
                  />
                                Also delete the Dawn folder (shared downloads, JRE, assets)
                            </label>}
                        <label className="modal-check">
                                <input
                    type="checkbox"
                    checked={deleteVersionFiles}
                    onChange={(e) => setDeleteVersionFiles(e.target.checked)}
                  />
                                Also delete downloaded Minecraft files (versions, libraries, assets) not used by other instances
                            </label>
                        <div className="modal-actions">
                            <button className="modal-btn" onClick={() => setShowDeleteConfirm(false)}>Cancel</button>
                            <button className="modal-btn danger" onClick={confirmDelete}>Delete</button>
                        </div>
                    </div>
                </div>}

            <InstallModal pending={dropPending} busy={dropBusy} onConfirm={confirmDropInstall} onCancel={() => setDropPending(null)} />

            {showGroupModal && <div className="modal-overlay" onClick={() => setShowGroupModal(false)}>
                    <div className="modal" onClick={(e) => e.stopPropagation()}>
                        <h3 className="modal-title">New group</h3>
                        <input
    className="modal-input"
    placeholder="Group name"
    value={groupName}
    onChange={(e) => {
      setGroupName(e.target.value);
      setGroupError("");
    }}
    autoFocus
    onKeyDown={(e) => {
      if (e.key === "Enter") addGroup();
    }}
  />
                        {groupError && <div className="modal-error">{groupError}</div>}
                        <div className="modal-actions">
                            <button className="modal-btn" onClick={() => {
    setShowGroupModal(false);
    setGroupError("");
  }}>Cancel</button>
                            <button className="modal-btn primary" onClick={addGroup}>Create</button>
                        </div>
                    </div>
                </div>}

            {showInstModal && <div className="modal-overlay" onClick={() => {
    setShowInstModal(false);
    setInstIcon("");
  }}>
                    <div className="inst-dialog" onClick={(e) => e.stopPropagation()}>
                        <div className="inst-header">
                            <div className="inst-icon">
                                {(() => {
    const icon = modalPreviewIcon();
    if (!icon) return <i className="ph ph-cube" />;
    if (icon.includes("/") || icon.includes(".")) return <img src={icon} alt="" />;
    return <i className={`ph ph-${icon}`} />;
  })()}
                            </div>
                            <div className="inst-header-icon-btns">
                                <button className="modal-btn" onClick={() => iconInputRef.current?.click()}>
                                    <i className="ph ph-camera" /> Upload
                                </button>
                                {instIcon && <button className="modal-btn" onClick={() => setInstIcon("")}>Remove</button>}
                            </div>
                            <div className="inst-header-field">
                                <label className="modal-label">Name</label>
                                <input
    className="modal-input"
    value={instName}
    placeholder="Instance name"
    onChange={(e) => setInstName(e.target.value)}
  />
                            </div>
                            <div className="inst-header-field">
                                <label className="modal-label">Group</label>
                                <select className="modal-select" value={instGroup} onChange={(e) => setInstGroup(e.target.value)}>
                                    {groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
                                </select>
                            </div>
                        </div>

                        <div className="inst-body">
                            <div className="inst-sources">
                                {["vanilla", "modrinth", "curseforge", "clients"].map((t) => <button
    key={t}
    className={`inst-source${instTab === t ? " active" : ""}`}
    onClick={() => setInstTab(t)}
  >
                                        {t === "vanilla" ? "Vanilla" : t === "modrinth" ? "Modrinth" : t === "curseforge" ? "CurseForge" : "Clients"}
                                    </button>)}
                            </div>

                            <div className="inst-main">
                                {instTab === "vanilla" && <div className="inst-vanilla">
                                        <div className="inst-cols">
                                            <div className="inst-col">
                                                <div className="inst-col-title">Minecraft version</div>
                                                <div className="inst-search">
                                                    <input
    className="modal-input"
    placeholder="Search..."
    value={vsearch}
    onChange={(e) => setVsearch(e.target.value)}
  />
                                                </div>
                                                <div className="inst-filters">
                                                    {[["release", "Releases"], ["snapshot", "Snapshots"], ["old_beta", "Old beta"], ["old_alpha", "Old alpha"]].map(([key, label]) => <label key={key} className="inst-filter">
                                                            <input
    type="checkbox"
    checked={vfilters[key]}
    onChange={(e) => setVfilters({ ...vfilters, [key]: e.target.checked })}
  />
                                                            {label}
                                                        </label>)}
                                                </div>
                                                <div className="inst-list inst-list-grow">
                                                    {mcVersions.filter((v) => vfilters[v.type] !== false).filter((v) => !vsearch || v.id.toLowerCase().includes(vsearch.toLowerCase())).map((v) => <div
    key={v.id}
    className={`inst-item inst-item-row${mcVersion === v.id ? " selected" : ""}`}
    onClick={() => setMcVersion(v.id)}
  >
                                                                <span className="inst-item-title">{v.id}</span>
                                                                <span className="inst-item-desc">{new Date(v.releaseTime).toLocaleDateString()}</span>
                                                                <span className="inst-item-desc">{v.type}</span>
                                                            </div>)}
                                                </div>
                                            </div>
                                            <div className="inst-col">
                                                <div className="inst-col-title">Mod loader</div>
                                                {loaderChecking ? <div className="inst-status">Checking loaders...</div> : <div className="inst-loaders">
                                                    {loaders.filter((l) => l === "Vanilla" || loaderSupport[l]).map((l) => <label key={l} className={`inst-loader${loader === l ? " selected" : ""}`}>
                                                            <input
    type="radio"
    name="loader"
    checked={loader === l}
    onChange={() => setLoader(l)}
  />
                                                            {l}
                                                        </label>)}
                                                </div>}
                                                {loader !== "Vanilla" && loaderVersions.length > 0 && <>
                                                        <div className="inst-col-title inst-sub">Loader version</div>
                                                        <div className="inst-list inst-list-grow">
                                                            {loaderVersions.map((v) => <div
    key={v}
    className={`inst-item inst-item-row${loaderVersion === v ? " selected" : ""}`}
    onClick={() => setLoaderVersion(v)}
  >
                                                                    <span className="inst-item-title">{v}</span>
                                                                </div>)}
                                                        </div>
                                                    </>}
                                            </div>
                                        </div>
                                    </div>}

                                {instTab === "modrinth" && <div className="inst-fields">
                                        <form className="inst-search" onSubmit={(e) => {
    e.preventDefault();
    searchModrinth();
  }}>
                                            <input
    className="modal-input"
    placeholder="Search modpacks..."
    value={mrQuery}
    onChange={(e) => {
      setMrQuery(e.target.value);
      setMrPage(1);
    }}
  />
                                            <select className="modal-select inst-sort" value={mrSort} onChange={(e) => {
      setMrSort(e.target.value);
      setMrPage(1);
    }}>
                                                {["Relevance", "Downloads", "Follows", "Newest", "Updated"].map((s2) => <option key={s2} value={s2.toLowerCase()}>{s2}</option>)}
                                            </select>
                                            <button type="submit" className="modal-btn primary">Search</button>
                                        </form>
                                        {mrLoading && <div className="inst-status">Loading...</div>}
                                        <div className="inst-list inst-list-grow">
                                            {mrResults.map((hit) => <div key={hit.project_id} className={`inst-item${mrSelected?.project_id === hit.project_id ? " selected" : ""}`} onClick={() => selectModrinth(hit)}>
                                                    {hit.icon_url && <img className="inst-item-icon" src={hit.icon_url} alt="" />}
                                                    <div className="inst-item-body">
                                                        <span className="inst-item-title">{hit.title}</span>
                                                        <span className="inst-item-desc">{hit.description}</span>
                                                    </div>
                                                </div>)}
                                        </div>
                                        {mrResults.length > 0 && <div className="ctab-pager">
                                                <button className="ctab-pager-btn" disabled={mrPage <= 1} onClick={() => setMrPage(mrPage - 1)}><i className="ph ph-caret-left" /></button>
                                                <span className="ctab-pager-info">Page {mrPage}{mrTotal > 0 ? ` / ${Math.ceil(mrTotal / 20)}` : ""}</span>
                                                <button className="ctab-pager-btn" disabled={mrPage * 20 >= mrTotal} onClick={() => setMrPage(mrPage + 1)}><i className="ph ph-caret-right" /></button>
                                            </div>}
                                        {mrVersions.length > 0 && <>
                                                <label className="modal-label">Version</label>
                                                <select className="modal-select" value={mrVersion} onChange={(e) => setMrVersion(e.target.value)}>
                                                    {mrVersions.map((v) => <option key={v.id} value={v.id}>{v.version_number} ({v.game_versions?.join(", ")})</option>)}
                                                </select>
                                            </>}
                                    </div>}

                                {instTab === "curseforge" && <div className="inst-fields">
                                        <form className="inst-search" onSubmit={(e) => {
    e.preventDefault();
    searchCurseForge();
  }}>
                                            <input
    className="modal-input"
    placeholder="Search modpacks..."
    value={cfQuery}
    onChange={(e) => {
      setCfQuery(e.target.value);
      setCfPage(1);
    }}
  />
                                            <select className="modal-select inst-sort" value={cfSort} onChange={(e) => {
      setCfSort(e.target.value);
      setCfPage(1);
    }}>
                                                {["Featured", "Popularity", "Total Downloads", "Last Updated", "Name"].map((s2) => <option key={s2} value={s2}>{s2}</option>)}
                                            </select>
                                            <button type="submit" className="modal-btn primary">Search</button>
                                        </form>
                                        {cfLoading && <div className="inst-status">Loading...</div>}
                                        <div className="inst-list inst-list-grow">
                                            {cfResults.map((mod) => <div key={mod.id} className={`inst-item${cfSelected?.id === mod.id ? " selected" : ""}`} onClick={() => selectCurseForge(mod)}>
                                                    {mod.logo?.url && <img className="inst-item-icon" src={mod.logo.url} alt="" />}
                                                    <div className="inst-item-body">
                                                        <span className="inst-item-title">{mod.name}</span>
                                                        <span className="inst-item-desc">{mod.summary}</span>
                                                    </div>
                                                </div>)}
                                        </div>
                                        {cfResults.length > 0 && <div className="ctab-pager">
                                                <button className="ctab-pager-btn" disabled={cfPage <= 1} onClick={() => setCfPage(cfPage - 1)}><i className="ph ph-caret-left" /></button>
                                                <span className="ctab-pager-info">Page {cfPage}{cfTotal > 0 ? ` / ${Math.ceil(cfTotal / 50)}` : ""}</span>
                                                <button className="ctab-pager-btn" disabled={cfPage * 50 >= cfTotal} onClick={() => setCfPage(cfPage + 1)}><i className="ph ph-caret-right" /></button>
                                            </div>}
                                        {cfFiles.length > 0 && <>
                                                <label className="modal-label">File</label>
                                                <select className="modal-select" value={cfFile} onChange={(e) => setCfFile(e.target.value)}>
                                                    {cfFiles.map((f) => <option key={f.id} value={String(f.id)}>{f.displayName} ({f.gameVersions?.join(", ")})</option>)}
                                                </select>
                                            </>}
                                    </div>}

                                {instTab === "clients" && <div className="inst-vanilla">
                                        <div className="inst-cols">
                                            <div className="inst-col">
                                                <div className="inst-col-title">Client</div>
                                                <div className="inst-list inst-list-grow">
                                                    {clients.map((c) => <div
    key={c.id}
    className={`inst-item${client === c.id ? " selected" : ""}`}
    onClick={() => setClient(c.id)}
  >
                                                        {clientIcon[c.id] ? <img src={clientIcon[c.id]} alt="" /> : <i className="ph ph-gamecontroller" />}
														<div className="inst-item-body">
															<span className="inst-item-title">{c.name}</span>
														</div>
                                                        </div>)}
                                                </div>
                                            </div>
                                            {client && <div className="inst-col">
                                                    <div className="inst-col-title">Minecraft version</div>
                                                    {clientLoading ? <div className="inst-status">Loading versions...</div> : <div className="inst-list inst-list-grow">
                                                            {clientVersions.map((v) => <div
    key={v.id}
    className={`inst-item inst-item-row${clientVersion === v.id ? " selected" : ""}`}
    onClick={() => setClientVersion(v.id)}
  >
                                                                    <span className="inst-item-title">{v.id}</span>
                                                                    {v.lunarOnly && <span className="inst-badge">Lunar only</span>}
                                                                </div>)}
                                                        </div>}
                                                    {clientModules.length > 0 && <>
                                                            <div className="inst-col-title inst-sub">Module</div>
                                                            <div className="inst-loaders">
                                                                {clientModules.map((m) => <label key={m} className={`inst-loader${clientModule === m ? " selected" : ""}`}>
                                                                        <input
    type="radio"
    name="clientModule"
    checked={clientModule === m}
    onChange={() => setClientModule(m)}
  />
                                                                        {m === "lunar-noOF" ? "lunar" : m}
                                                                    </label>)}
                                                            </div>
                                                        </>}
                                                </div>}
                                        </div>
                                    </div>}
                            </div>
                        </div>

                        <div className="inst-actions">
                            <button className="modal-btn" onClick={() => setShowInstModal(false)}>Cancel</button>
                            <button className="modal-btn primary" onClick={createBtn}>Create</button>
                        </div>
                    </div>
                </div>}
            <UpdateModal />
        </div>;
}
const LogView = ({ lines, showBar = true, auto, onAuto }) => {
  const boxRef = useRef(null);
  const [localAuto, setLocalAuto] = useState(true);
  const useAuto = auto !== void 0 ? auto : localAuto;
  const toggle = () => onAuto ? onAuto(!useAuto) : setLocalAuto(!useAuto);
  useEffect(() => {
    if (useAuto && boxRef.current) boxRef.current.scrollTop = boxRef.current.scrollHeight;
  }, [lines, useAuto]);
  return <div className="log-view">
            {showBar && <div className="log-view-bar">
                    <button
    className={`log-auto-btn${useAuto ? " on" : ""}`}
    onClick={toggle}
    title="Auto-scroll"
  >
                        Auto scroll
                    </button>
                </div>}
            <div className="log-view-body" ref={boxRef}>
                {lines.length ? lines.map((line, i) => <div key={i} className="game-console-line">{line}</div>) : <div className="logs-empty">No log lines yet.</div>}
            </div>
        </div>;
};

