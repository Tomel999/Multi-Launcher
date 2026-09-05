import { useEffect, useRef, useState } from "preact/hooks";
import { EventsOn, BrowserOpenURL, Quit } from "../wailsjs/runtime/runtime";
import { ApplyUpdate, DownloadUpdate } from "../wailsjs/go/main/App";

// The four events emitted by the Go side (see app.go):
//   update:available  *updater.UpdateInfo  — a newer release exists
//   update:progress   UpdateProgress       — download progress
//   update:downloaded { path }             — download verified and staged
//   update:error      UpdateError          — any failure, with a `kind`
const EV_AVAILABLE = "update:available";
const EV_PROGRESS = "update:progress";
const EV_DOWNLOADED = "update:downloaded";
const EV_ERROR = "update:error";

const formatBytes = (n) => {
  const v = Number(n) || 0;
  if (v <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(v) / Math.log(1024)), units.length - 1);
  const scaled = v / 1024 ** i;
  return `${scaled >= 10 || i === 0 ? Math.round(scaled) : scaled.toFixed(1)} ${units[i]}`;
};

const formatDate = (iso) => {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
};

// Human wording for each typed error kind produced by internal/updater.
const errorText = (err) => {
  const stage = err?.stage === "install"
    ? "The update could not be installed."
    : "The update could not be downloaded.";
  switch (err?.kind) {
    case "managed":
      return err?.message || "This installation is managed by a system package manager.";
    case "rate_limit":
      return "GitHub is rate limiting update checks. Try again in a few minutes.";
    case "network":
      return `${stage} Check your internet connection and try again.`;
    case "checksum":
      return "The download failed its integrity check and was discarded. Your installation is unchanged.";
    case "no_asset":
      return "This release has no build for your platform.";
    case "not_found":
      return `${stage} The release files are no longer available.`;
    case "install":
      return "The update was downloaded but could not be installed. If the launcher runs from a protected folder, try moving it elsewhere and updating again.";
    default:
      return err?.message || `${stage} Please try again.`;
  }
};

export function UpdateModal() {
  const [info, setInfo] = useState(null);
  // idle | downloading | installing | error
  const [phase, setPhase] = useState("idle");
  const [progress, setProgress] = useState({ percent: 0, downloaded: 0, total: 0 });
  const [error, setError] = useState(null);
  // Set when a silent auto-update finished downloading while no modal was
  // open: a non-blocking toast, installed on quit.
  const [staged, setStaged] = useState(false);

  // Mirrors `info` for event handlers, which would otherwise see a stale
  // closure and mistake notify-mode downloads for silent ones.
  const infoRef = useRef(null);
  const setInfoTracked = (v) => {
    infoRef.current = v;
    setInfo(v);
  };

  // "Later" dismisses for the whole session: the automatic check only runs
  // once at startup, so no further prompt appears until the app is relaunched.
  const dismissed = useRef(false);

  useEffect(() => {
    const offAvailable = EventsOn(EV_AVAILABLE, (data) => {
      if (dismissed.current || !data) return;
      setInfoTracked(data);
      setError(null);
      setProgress({ percent: 0, downloaded: 0, total: data.size || 0 });
      setPhase("idle");
    });

    const offProgress = EventsOn(EV_PROGRESS, (p) => {
      if (!p) return;
      setPhase("downloading");
      setProgress({
        percent: Number(p.percent) || 0,
        downloaded: Number(p.downloaded) || 0,
        total: Number(p.total) || 0
      });
    });

    // The download is verified (SHA-256) before this fires, so it is safe to
    // install straight away. In silent auto mode no modal is open, so show a
    // toast instead: the update installs on quit.
    const offDownloaded = EventsOn(EV_DOWNLOADED, () => {
      if (!infoRef.current) {
        setStaged(true);
        return;
      }
      setPhase("installing");
      ApplyUpdate().catch((e) => {
        setPhase("error");
        setError({ kind: "install", stage: "install", message: String(e?.message || e) });
      });
    });

    const offError = EventsOn(EV_ERROR, (e) => {
      setPhase("error");
      setError(e || { kind: "", message: "Update failed." });
    });

    return () => {
      offAvailable?.();
      offProgress?.();
      offDownloaded?.();
      offError?.();
    };
  }, []);

  if (!info) {
    // Silent-mode toast only; the modal stays hidden until an update:available
    // event (notify mode or a manager-owned install) opens it.
    if (!staged) return null;
    return <div className="upd-toast" onClick={() => setStaged(false)}>
            <i className="ph ph-download-simple" />
            <span>Update ready — it installs when you quit.</span>
            <button className="upd-toast-btn" onClick={(e) => {
              e.stopPropagation();
              setStaged(false);
              Quit();
            }}>
                Restart now
            </button>
        </div>;
  }

  const managed = info.managed || null;

  const startDownload = () => {
    setError(null);
    setProgress({ percent: 0, downloaded: 0, total: info.size || 0 });
    setPhase("downloading");
    DownloadUpdate().catch((e) => {
      setPhase("error");
      setError({ kind: "", stage: "download", message: String(e?.message || e) });
    });
  };

  const dismiss = () => {
    dismissed.current = true;
    setInfoTracked(null);
  };

  const busy = phase === "downloading" || phase === "installing";
  const pct = Math.max(0, Math.min(100, progress.percent));
  const sizeLine = progress.total > 0
    ? `${formatBytes(progress.downloaded)} / ${formatBytes(progress.total)}`
    : formatBytes(progress.downloaded);

  return <div className="modal-overlay" onClick={() => {
    // Downloading/installing cannot be cancelled mid-flight.
    if (!busy) dismiss();
  }}>
            <div className="modal upd-modal" onClick={(e) => e.stopPropagation()}>
                <div className="upd-title">
                    <i className="ph ph-download-simple" />
                    {managed ? `Update via ${managed.name}` : "Update available"}
                </div>

                <div className="upd-version">
                    <span className="upd-version-new">{info.version}</span>
                    <span className="upd-version-arrow"><i className="ph ph-arrow-right" /></span>
                    <span className="upd-version-old">{info.current || "current"}</span>
                </div>

                <div className="upd-meta">
                    {formatDate(info.publishedAt) && <span>{formatDate(info.publishedAt)}</span>}
                    {info.size > 0 && <span>{formatBytes(info.size)}</span>}
                    {info.assetName && <span className="upd-asset">{info.assetName}</span>}
                </div>

                {managed
                  ? <div className="upd-managed">
                        <i className="ph ph-package" />
                        <span>This installation is managed by {managed.name} and cannot update itself. Run:</span>
                        <code className="upd-code">{managed.command}</code>
                    </div>
                  : info.changelog
                    ? <pre className="upd-changelog">{info.changelog}</pre>
                    : <div className="upd-nochangelog">No changelog provided for this release.</div>}

                {phase === "downloading" && <>
                        <div className="upd-bar">
                            <div className="upd-bar-fill" style={{ width: `${pct}%` }} />
                        </div>
                        <div className="upd-progress-text">
                            <span>{pct}%</span>
                            <span>{sizeLine}</span>
                        </div>
                    </>}

                {phase === "installing" && <div className="upd-status">
                        <i className="ph ph-arrows-clockwise" /> Installing update and restarting…
                    </div>}

                {phase === "error" && <div className="upd-error">
                        <i className="ph ph-warning-circle" />
                        <span>{errorText(error)}</span>
                    </div>}

                <div className="upd-actions">
                    {info.releaseUrl && <button
    className="upd-link"
    onClick={() => BrowserOpenURL(info.releaseUrl)}
    disabled={busy}
  >
                            View release
                        </button>}
                    <div className="upd-actions-spacer" />
                    {!managed && (phase === "error"
                      ? <button className="modal-btn primary" onClick={startDownload}>
                            Retry
                        </button>
                      : <button className="modal-btn primary" onClick={startDownload} disabled={busy}>
                            {phase === "downloading" ? "Downloading…" : phase === "installing" ? "Installing…" : "Update now"}
                        </button>)}
                    <button className="modal-btn" onClick={dismiss} disabled={busy}>
                        Later
                    </button>
                </div>
            </div>
        </div>;
}
