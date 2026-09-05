# Multi Launcher - Project Memory

## Auto-update system
- Updater reads from GitHub Releases: `api.github.com/repos/Tomel999/Multi-Launcher/releases/latest`
- Owner/repo configured in `app.go`: `updateOwner = "Tomel999"`, `updateRepo = "Multi-Launcher"`
- Asset naming convention: `Multi-Launcher-<goos>-<goarch><ext>` (+ `.sha256` sidecar)
- SHA-256 verification is mandatory — no sidecar = no install
- Windows: batch script swap (running .exe can't be overwritten)
- Linux: atomic rename over running binary
- macOS: ditto copy of .app bundle
- CI: `.github/workflows/build.yml` — triggers on `tags: ["v*"]`
- Version stamped via ldflags: `-X 'multilauncherwails/internal/version.Version=$VERSION'`

## CI issues fixed
- NSIS makensis not found on windows-latest runner: choco installs to varying paths.
  Fix: dynamic search across all common locations + fallback `find /c/` scan.
  GITHUB_PATH alone is insufficient — bash shells in same job don't inherit it.
  Each step (Ensure/Build/Package) must independently resolve makensis path.
- Node 20 deprecated on GitHub Actions; upgraded to Node 24.
