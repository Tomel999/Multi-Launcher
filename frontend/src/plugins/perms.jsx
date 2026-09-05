export const PERM_LABELS = {
  "events:*": "Listen to launcher events",
  "fs:instance:read": "Read instance files",
  "fs:instance:write": "Write instance files",
  "fs:mods:write": "Write into the mods folder (runs with the game)",
  "network:custom": "Make network requests",
  "ui:theme": "Change theme colors",
  "ui:slots": "Add UI elements",
  "ui:notifications": "Show notifications",
  "accounts:read": "Read account names",
  "instances:read": "List instances",
  "instances:open": "Open instance folders",
  "launch:start": "Start game instances",
  "mods:read": "List instance mods",
  "worlds:read": "List instance worlds",
  "logs:read": "Read game logs",
  "ui:views": "Add whole views (sidebar tabs)",
  "ui:override": "Hide or replace built-in views",
  "runtime:wasm": "Run compiled WebAssembly code (native-level trust)"
};
export function PermChips({ perms }) {
  return <div className="plugin-card-perms">
            {(perms || []).map((perm) => <span key={perm} className="plugin-perm" title={PERM_LABELS[perm] || perm}>
                    {PERM_LABELS[perm] || perm}
                </span>)}
            {(!perms || perms.length === 0) && <span style={{ fontSize: 13, color: "var(--color-text-dim)" }}>No permissions requested.</span>}
        </div>;
}
