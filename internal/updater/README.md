# Updater

Self-update support for Multi Launcher, backed by GitHub Releases.

The updater checks for a newer release a few seconds after launch, shows the
release changelog in a modal, downloads the platform build, verifies it with
SHA-256, and relaunches the updated app. Everything is cancellable, bounded by
timeouts, and designed to fail silently: if GitHub is unreachable the launcher
simply keeps working.

Repository: **Tomel999/Multi-Launcher** (see `updateOwner` / `updateRepo` in
`app.go`).

---

## 1. Release asset naming convention (REQUIRED)

**Your CI/release pipeline must produce exactly these file names.** The updater
matches on the full name, so a rename on the release page silently disables
updates for that platform (the check logs `no_asset` and does nothing).

```
Multi-Launcher-<goos>-<goarch><ext>
Multi-Launcher-<goos>-<goarch><ext>.sha256
```

`<goos>` and `<goarch>` are Go's `runtime.GOOS` / `runtime.GOARCH` values, in
lowercase. The extension depends on the platform:

| Platform            | Asset file name                          | Contents                        |
| ------------------- | ---------------------------------------- | ------------------------------- |
| Windows x64         | `Multi-Launcher-windows-amd64.exe`       | the portable `.exe`             |
| Windows ARM64       | `Multi-Launcher-windows-arm64.exe`       | the portable `.exe`             |
| macOS Intel         | `Multi-Launcher-darwin-amd64.zip`        | a `.zip` containing `*.app`     |
| macOS Apple Silicon | `Multi-Launcher-darwin-arm64.zip`        | a `.zip` containing `*.app`     |
| Linux x64           | `Multi-Launcher-linux-amd64.tar.gz`      | a `.tar.gz` containing the binary |
| Linux ARM64         | `Multi-Launcher-linux-arm64.tar.gz`      | a `.tar.gz` containing the binary |

Plus a **`.sha256` sidecar for every binary asset above**, e.g.
`Multi-Launcher-windows-amd64.exe.sha256`.

Rules:

- Names are **case-sensitive** and must not include a version number. The
  updater always reads the `latest` release, so versions belong in the git tag
  only.
- The prefix `Multi-Launcher` matches `outputfilename` in `wails.json`
  (`"Multi Launcher"`) with spaces turned into hyphens. If you rename the app,
  update `AssetPrefix` in `asset.go` **and** your pipeline together.
- Only publish assets you actually support. A release that ships, say, only the
  Windows build is fine — other platforms see `no_asset` and stay on their
  current version.

### First-install formats (not consumed by the updater)

Besides the six assets above, the release may ship human-friendly installers.
The updater **ignores** them (they never match `AssetPrefix-goos-goarch`); they
exist only for first-time installation:

| File | Installs to | Self-update afterwards? |
| --- | --- | --- |
| `Multi-Launcher-windows-amd64-setup.exe` | `%LOCALAPPDATA%\Programs` (per-user NSIS, no admin) | yes — updater-owned |
| `Multi-Launcher-darwin-<arch>.dmg` | drag to `/Applications` | yes, if user-owned |
| `Multi-Launcher-linux-amd64.tar.xz` | manual extract | yes (portable) |
| `Multi-Launcher-linux-amd64.AppImage` | run anywhere (`chmod +x`) | yes (portable); needs `libfuse2t64` on Ubuntu 24.04+ |
| `multilauncher_<ver>_amd64.deb` / `multilauncher-<ver>-1.x86_64.rpm` | `/usr/bin` via apt/dnf | no — manager-owned (`system`), updates via apt/dnf |

Every file, updater asset or installer, ships with a `.sha256` sidecar.

### Archive layout

- **macOS `.zip`** — must contain **exactly one** `*.app` bundle. A single
  wrapping directory is fine (it is searched recursively); two bundles is an
  error. Keep the bundle signed: the updater copies it with `ditto` and does
  **not** re-sign.
- **Linux `.tar.gz`** — must contain the launcher binary. The updater prefers
  an entry whose base name matches the running executable, then falls back to
  the first executable file.

### Generating the checksum files

Any `sha256sum` output is accepted (`<hex>  <filename>`, `<hex> *<filename>`,
or a bare `<hex>`), lowercase or uppercase:

```bash
sha256sum "Multi-Launcher-windows-amd64.exe" > "Multi-Launcher-windows-amd64.exe.sha256"
sha256sum "Multi-Launcher-darwin-arm64.zip"  > "Multi-Launcher-darwin-arm64.zip.sha256"
sha256sum "Multi-Launcher-linux-amd64.tar.gz" > "Multi-Launcher-linux-amd64.tar.gz.sha256"
```

**Verification fails closed.** If the sidecar is missing, unreadable, malformed
or does not match, the download is discarded and the user sees an `update:error`
event with `kind: "checksum"`. The existing installation is never touched.

### Example: GitHub Actions release step

```yaml
- name: Build
  run: wails build -platform windows/amd64 -ldflags "-X 'multilauncherwails/internal/version.Version=${{ github.ref_name }}'"

- name: Package + checksum
  shell: bash
  run: |
    cd build/bin
    mv "Multi Launcher.exe" "Multi-Launcher-windows-amd64.exe"
    sha256sum "Multi-Launcher-windows-amd64.exe" > "Multi-Launcher-windows-amd64.exe.sha256"

- name: Publish
  uses: softprops/action-gh-release@v2
  with:
    files: build/bin/Multi-Launcher-windows-amd64.exe*
```

---

## 2. Version stamping

The running version lives in `internal/version.Version` and defaults to `dev`.
Stamp it at build time:

```bash
wails build -ldflags "-X 'multilauncherwails/internal/version.Version=v1.2.3'"
```

The version is compared against the release `tag_name` using semantic
versioning (a leading `v` is optional; `v1.2` and other partial forms are
rejected as ambiguous).

Automatic startup checks are **skipped on `dev` builds** — a hand-built binary
would otherwise be replaced by a release build mid-development. Opt in with
`MULTILAUNCHER_UPDATE_CHECK_DEV=1`.

---

## 3. Environment variables

| Variable | Purpose | Example |
| --- | --- | --- |
| `GH_TOKEN` | Optional GitHub token sent as a bearer header to the **API only**. Raises the rate limit from 60 to 5000 requests/hour. Never logged, never forwarded to download hosts. | `ghp_xxxx` |
| `GITHUB_TOKEN` | Fallback if `GH_TOKEN` is unset. | `ghp_xxxx` |
| `MULTILAUNCHER_NO_UPDATE_CHECK` | Set to `1` to disable the automatic startup check entirely. | `1` |
| `MULTILAUNCHER_UPDATE_CHECK_DEV` | Set to `1` to enable the startup check on `dev` builds. | `1` |
| `MULTILAUNCHER_NO_AUTO_UPDATE` | Set to `1` to keep the classic modal flow ("Update now" / "Later") instead of silent auto-update. | `1` |

Unauthenticated checks are fine for normal use — one request per launch is far
below the 60/hour budget. Tokens are only needed for machines that restart the
launcher constantly (CI, shared boxes).

---

## 4. Contract with the frontend

Bound methods (`frontend/wailsjs/go/main/App.js`):

| Method | Returns | Notes |
| --- | --- | --- |
| `CheckForUpdate()` | `UpdateInfo \| null` | `null` means up to date. Throws a typed error on failure. |
| `DownloadUpdate()` | `void` | Starts the download and returns immediately. |
| `ApplyUpdate()` | `void` | Installs the verified download and quits the app. |
| `GetAppVersion()` | `string` | The running version, or `"dev"`. |

Events (see `UpdateModal.jsx`):

| Event | Payload | Fired when |
| --- | --- | --- |
| `update:available` | `UpdateInfo` | A newer release exists for this platform. |
| `update:progress` | `{ percent, downloaded, total, assetName }` | During download (throttled to every 256 KiB or 200 ms). |
| `update:downloaded` | `{ path }` | Download finished **and** its SHA-256 matched. |
| `update:error` | `{ message, kind, stage }` | Any failure. `kind` is `network`, `http`, `rate_limit`, `not_found`, `decode`, `no_asset`, `checksum`, `install` or `managed`. |

`UpdateInfo` fields: `current`, `version`, `tagName`, `name`, `changelog`,
`publishedAt` (RFC 3339), `releaseUrl`, `assetName`, `size`, plus `managed`
(`{ id, name, command }`) when the install is manager-owned.

### Silent auto-update (default)

A portable install never shows the modal: after startup the release downloads
in the background (`update:progress` still fires, so a progress UI could hook
in), and when the SHA-256 verifies, `update:downloaded` fires and the file
waits in the cache. The frontend shows a small "installs when you quit" toast
with a "Restart now" button. On shutdown (`OnShutdown` hook) the staged file
is installed via `InstallWithoutRelaunch` — the same swap as the manual flow
but without spawning a fresh instance.

Set `MULTILAUNCHER_NO_AUTO_UPDATE=1` for the notify flow: `update:available`
opens the modal, "Update now" downloads, and the verified file is installed
with a relaunch immediately.

### Manager-owned installs

Before downloading, the backend classifies the running executable
(`managed.go`). A match means the updater must never touch the files:

| ID | Detected how | Update path |
| --- | --- | --- |
| `flatpak` | `FLATPAK_ID` env | automatic via `flatpak-spawn --host flatpak update <id>` when the manifest allows host talk; otherwise the modal shows the command |
| `snap` | `SNAP_NAME` / `SNAP_INSTANCE_NAME` env, or `/snap/` prefix | modal shows `snap refresh <name>` (snapd also auto-refreshes on its own) |
| `aur` | executable under `/usr/` on Arch (`/etc/arch-release`) | modal shows the `yay` command |
| `brew` | executable under `.../Caskroom/...` or `.../Cellar/...` | modal shows `brew upgrade --cask` |
| `system` | executable under `/usr/` elsewhere | modal names the system package manager |
| `programfiles` | executable under `Program Files` | modal suggests reinstalling from the release |

Every `Install` entry point refuses these with `kind: "managed"`, so even a
manually-triggered `ApplyUpdate()` cannot half-replace a managed install.

---

## 5. How the install works per platform

- **Windows** — a running `.exe` cannot overwrite itself, so `Install` writes a
  self-deleting batch script to `%TEMP%` and launches it detached. The script
  repeatedly tries to `move` the running binary (which Windows refuses while it
  is locked, for up to 120 s), moves the staged download over it, relaunches
  the app, then deletes the staged file and itself. If the copy fails, the
  previous binary is restored.
- **macOS** — the `.zip` is extracted to a temp directory, the current `.app`
  bundle is moved aside, the new bundle is copied in with `ditto` (preserving
  symlinks, resource forks and extended attributes), the old bundle is deleted,
  and the app is reopened with `open -n`. Nothing is re-signed. On failure the
  previous bundle is restored.
- **Linux** — the `.tar.gz` is extracted (or the raw binary/AppImage used
  directly), the new file is staged next to the running executable and renamed
  over it. POSIX allows replacing a running binary, and the rename is atomic,
  so no helper script is needed. The app is then relaunched in a new session
  with the same arguments.

Downloads are staged in the user cache directory
(`~/.cache/.multilauncher/updates`, `%LOCALAPPDATA%\...`, `~/Library/Caches/...`)
rather than next to the executable, so a download never pollutes the macOS
`.app` bundle or a read-only install directory.

---

## 6. Known limitations

- **Elevation is not handled.** A `Program Files` install is classified as
  manager-owned (`programfiles`): the modal suggests reinstalling from the
  release instead of attempting a swap. Adding a UAC/elevation prompt is
  future work.
- **Deltas are not supported** — every update is a full binary download.
- **Rollback is manual.** The old binary/bundle is deleted after a successful
  swap; recovering means reinstalling a previous release by hand.
- The startup check runs **once per launch**. In notify mode, choosing "Later"
  in the modal dismisses it until the next restart. In silent mode there is no
  prompt at all.

## 7. Tests

```bash
go test ./internal/updater/ ./internal/version/
```

Covers semver comparison (including malformed input), mocked GitHub API
responses and error kinds, asset selection for every platform, download
progress/throttling, checksum verification (including mismatch and missing
sidecar), and the archive helpers with zip-slip protection.
