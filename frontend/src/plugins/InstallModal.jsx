import { PermChips } from "./perms";
export function InstallModal({ pending, busy, onConfirm, onCancel }) {
  if (!pending) return null;
  const m = pending.manifest;
  return <div className="modal-overlay" onClick={onCancel}>
            <div className="modal" onClick={(e) => e.stopPropagation()}>
                <div style={{ fontSize: 15, fontWeight: 700, color: "var(--color-text)" }}>
                    Install "{m.name}"?
                </div>
                <div style={{ fontSize: 13, color: "var(--color-text-dim)" }}>
                    v{m.version}{m.author ? ` by ${m.author}` : ""}
                </div>
                <div style={{ fontSize: 13, color: "var(--color-text)", marginTop: 4 }}>
                    This plugin requests permissions:
                </div>
                <PermChips perms={(m.permissions || []).filter((p) => p !== "runtime:wasm")} />
                {(m.runtime === "wasm" || (m.permissions || []).includes("runtime:wasm")) && (
                    <div style={{
                        fontSize: 13, color: "var(--color-warning, #f59e0b)", marginTop: 8,
                        border: "1px solid var(--color-warning, #f59e0b)", borderRadius: 6, padding: "6px 10px"
                    }}>
                        ⚠️ This plugin runs compiled WebAssembly code — a different trust level than ordinary JS plugins.
                    </div>
                )}
                <div style={{ display: "flex", justifyContent: "flex-end", gap: 10, marginTop: 4 }}>
                    <button className="modal-btn" onClick={onCancel} disabled={busy}>Cancel</button>
                    <button className="modal-btn primary" onClick={onConfirm} disabled={busy}>
                        {busy ? "Installing\u2026" : "Install"}
                    </button>
                </div>
            </div>
        </div>;
}
