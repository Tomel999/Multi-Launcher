package launcher

import (
	"archive/zip"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const ogulniegaLauncherURL = "https://ogulniega.com/files/launcher.json"
const ogulniegaClientBaseURL = "https://ogulniega.com/files/client_versions"

type ogulniegaVersion struct {
	LoaderName       string   `json:"loader_name"`
	Name             string   `json:"name"`
	MinecraftVersion string   `json:"minecraft_version"`
	FabricVersion    string   `json:"fabric_version"`
	JavaName         string   `json:"java_name"`
	JvmArgs          []string `json:"jvm_args"`
}

type ogulniegaLauncherFile struct {
	Versions                  []ogulniegaVersion `json:"versions"`
	DefaultProfileURL         string             `json:"default_profile_url"`
	DefaultProfileFallbackURL string             `json:"default_profile_fallback_url"`
}

func ogulniegaCachePath() string {
	return filepath.Join(ClientsDataDir(), "ogulniega", "launcher.json")
}

func ogulniegaLoad() (*ogulniegaLauncherFile, error) {
	var lf ogulniegaLauncherFile
	fetchErr := fetchJSON(ogulniegaLauncherURL, &lf)
	if fetchErr == nil && len(lf.Versions) > 0 {
		if data, merr := json.Marshal(&lf); merr == nil {
			p := ogulniegaCachePath()
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, data, 0o644)
		}
		return &lf, nil
	}
	if data, rerr := os.ReadFile(ogulniegaCachePath()); rerr == nil {
		var cached ogulniegaLauncherFile
		if json.Unmarshal(data, &cached) == nil && len(cached.Versions) > 0 {
			return &cached, nil
		}
	}
	if fetchErr != nil {
		return nil, fmt.Errorf("ogulniega: %w", fetchErr)
	}
	return nil, fmt.Errorf("ogulniega: empty version list")
}

func ogulniegaModule(name string) string {
	if i := strings.LastIndex(name, "-"); i > 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return ""
}

func ogulniegaVersions() ([]ClientVersion, error) {
	lf, err := ogulniegaLoad()
	if err != nil {
		return nil, err
	}
	byMC := map[string][]string{}
	for _, v := range lf.Versions {
		if v.Name == "" || v.MinecraftVersion == "" {
			continue
		}
		mod := ogulniegaModule(v.Name)
		dup := false
		for _, m := range byMC[v.MinecraftVersion] {
			if m == mod {
				dup = true
				break
			}
		}
		if !dup {
			byMC[v.MinecraftVersion] = append(byMC[v.MinecraftVersion], mod)
		}
	}
	out := make([]ClientVersion, 0, len(byMC))
	for mc, mods := range byMC {
		filtered := mods[:0]
		for _, m := range mods {
			if m != "" {
				filtered = append(filtered, m)
			}
		}
		out = append(out, ClientVersion{ID: mc, Modules: filtered})
	}
	sort.Slice(out, func(i, j int) bool { return lunarVersionLess(out[i].ID, out[j].ID) })
	return out, nil
}

func ogulniegaFind(lf *ogulniegaLauncherFile, version, module string) (*ogulniegaVersion, error) {
	for i := range lf.Versions {
		if lf.Versions[i].Name == version {
			return &lf.Versions[i], nil
		}
	}
	var cand []ogulniegaVersion
	for _, v := range lf.Versions {
		if v.MinecraftVersion == version {
			cand = append(cand, v)
		}
	}
	if len(cand) == 0 {
		return nil, fmt.Errorf("ogulniega: unknown version %q", version)
	}
	if module != "" {
		for i := range cand {
			if cand[i].Name == version+"-"+module || ogulniegaModule(cand[i].Name) == module {
				return &cand[i], nil
			}
		}
		return nil, fmt.Errorf("ogulniega: version %q has no module %q", version, module)
	}
	if len(cand) == 1 {
		return &cand[0], nil
	}
	return nil, fmt.Errorf("ogulniega: version %q needs a module", version)
}

func ogulniegaLoaderVersion(v *ogulniegaVersion) string {
	fromName := ""
	if i := strings.LastIndex(v.LoaderName, "-"); i >= 0 && i+1 < len(v.LoaderName) {
		fromName = v.LoaderName[i+1:]
	}
	best := v.FabricVersion
	if best == "" {
		return fromName
	}
	if fromName != "" && lunarVersionLess(best, fromName) {
		best = fromName
	}
	return best
}

type ogulniegaMod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	FallbackURL string `json:"fallback_url"`
	SHA512      string `json:"sha512"`
}

type ogulniegaClientManifest struct {
	Mods []ogulniegaMod `json:"mods"`
}

func ogulniegaClientManifestFetch(name string) (*ogulniegaClientManifest, error) {
	var cv ogulniegaClientManifest
	if err := fetchJSON(ogulniegaClientBaseURL+"/"+name+".json", &cv); err != nil {
		return nil, fmt.Errorf("ogulniega %s: %w", name, err)
	}
	return &cv, nil
}

func sha512File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ogulniegaModValid(dest, want string) bool {
	fi, err := os.Stat(dest)
	if err != nil || fi.Size() == 0 {
		return false
	}
	if want == "" {
		return true
	}
	sum, err := sha512File(dest)
	return err == nil && strings.EqualFold(sum, want)
}

func ogulniegaModDest(dir string, m ogulniegaMod) string {
	if m.ID == "optifine" {
		return filepath.Join(dir, "preinstalled", filepath.Base(m.Name))
	}
	return filepath.Join(dir, filepath.Base(m.Name))
}

func ogulniegaEnsureMods(entry *ogulniegaVersion, instDir string, log LogFn, onProgress ProgressFn) error {
	cv, err := ogulniegaClientManifestFetch(entry.Name)
	if err != nil {
		return err
	}
	dir := filepath.Join(instDir, "mods", entry.Name)
	os.MkdirAll(filepath.Join(dir, "preinstalled"), 0o755)
	keep := map[string]bool{}
	var jobs []job
	for _, m := range cv.Mods {
		if m.Name == "" || m.URL == "" {
			continue
		}
		dest := ogulniegaModDest(dir, m)
		if rel, rerr := filepath.Rel(dir, dest); rerr == nil {
			keep[rel] = true
		}
		if !ogulniegaModValid(dest, m.SHA512) {
			os.Remove(dest)
			jobs = append(jobs, job{url: m.URL, dest: dest})
		}
	}
	tr := newTracker("ogulniega-mods", onProgress)
	tr.setTotal(int64(len(jobs)), 0)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	tr.emit()
	for _, m := range cv.Mods {
		if m.Name == "" || m.URL == "" {
			continue
		}
		dest := ogulniegaModDest(dir, m)
		if ogulniegaModValid(dest, m.SHA512) {
			continue
		}
		if m.FallbackURL == "" {
			if firstErr != nil {
				return firstErr
			}
			return fmt.Errorf("ogulniega %s: mod %s failed to verify", entry.Name, m.Name)
		}
		os.Remove(dest)
		log("    [mod] retry " + m.Name + " via fallback")
		if err := downloadTo(m.FallbackURL, dest, 0, tr); err != nil {
			return fmt.Errorf("ogulniega %s: mod %s: %w", entry.Name, m.Name, err)
		}
		if !ogulniegaModValid(dest, m.SHA512) {
			os.Remove(dest)
			return fmt.Errorf("ogulniega %s: mod %s failed to verify", entry.Name, m.Name)
		}
	}
	for _, sub := range []string{".", "preinstalled"} {
		base := filepath.Join(dir, sub)
		if fis, err := os.ReadDir(base); err == nil {
			for _, fi := range fis {
				if fi.IsDir() {
					continue
				}
				rel := fi.Name()
				if sub != "." {
					rel = filepath.Join(sub, fi.Name())
				}
				if !keep[rel] {
					os.Remove(filepath.Join(base, fi.Name()))
				}
			}
		}
	}
	return nil
}

func ogulniegaExtractMissing(src, dest string) (int, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	n := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		out, ok := safeExtractPath(dest, f.Name)
		if !ok {
			continue
		}
		if _, err := os.Stat(out); err == nil {
			continue
		}
		os.MkdirAll(filepath.Dir(out), 0o755)
		rc, err := f.Open()
		if err != nil {
			return n, err
		}
		w, err := os.Create(out)
		if err != nil {
			rc.Close()
			return n, err
		}
		_, err = io.Copy(w, rc)
		rc.Close()
		w.Close()
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func ogulniegaEnsureProfile(lf *ogulniegaLauncherFile, instDir string, log LogFn, onProgress ProgressFn) error {
	if lf.DefaultProfileURL == "" {
		return nil
	}
	zipPath := filepath.Join(ClientsDataDir(), "ogulniega", "default_config.zip")
	if fi, err := os.Stat(zipPath); err != nil || fi.Size() == 0 {
		tmp := zipPath + ".new"
		os.Remove(tmp)
		tr := newTracker("ogulniega-profile", onProgress)
		tr.setTotal(1, 0)
		err := downloadTo(lf.DefaultProfileURL, tmp, 0, tr)
		if err != nil && lf.DefaultProfileFallbackURL != "" {
			log("    [profile] retry via fallback")
			err = downloadTo(lf.DefaultProfileFallbackURL, tmp, 0, tr)
		}
		tr.emit()
		if err != nil {
			os.Remove(tmp)
			return fmt.Errorf("ogulniega default profile: %w", err)
		}
		os.Remove(zipPath)
		if err := os.Rename(tmp, zipPath); err != nil {
			return fmt.Errorf("ogulniega default profile: %w", err)
		}
	}
	n, err := ogulniegaExtractMissing(zipPath, instDir)
	if err != nil {
		return fmt.Errorf("ogulniega default profile: %w", err)
	}
	log(fmt.Sprintf("  Default profile: %d new files", n))
	return nil
}

func LaunchOgulniega(instName, version, module string, acc Account, opts LaunchOptions, log LogFn, state StateFn, onProgress ProgressFn) error {
	lf, err := ogulniegaLoad()
	if err != nil {
		return err
	}
	entry, err := ogulniegaFind(lf, version, module)
	if err != nil {
		return err
	}
	mc := entry.MinecraftVersion
	javaPath := opts.JavaPath
	if javaPath == "" {
		major := 0
		if vm, verr := GetVersionMeta(mc); verr == nil {
			major, _ = RequiredJavaMajor(vm)
		}
		if major == 0 {
			major = parseJavaRuntime(entry.JavaName)
		}
		if major == 0 {
			major = 21
		}
		if javaPath, err = EnsureJava(major, onProgress); err != nil {
			return err
		}
	} else if _, serr := os.Stat(javaPath); serr != nil {
		return fmt.Errorf("custom java path not found: %s", javaPath)
	}
	_, classpath, err := EnsureInstalled(mc, onProgress)
	if err != nil {
		return err
	}
	loaderVer := ogulniegaLoaderVersion(entry)
	if loaderVer == "" {
		return fmt.Errorf("ogulniega %q: no loader version", entry.Name)
	}
	launchVersion, loaderCP, mainClass, err := EnsureLoader("Fabric", mc, loaderVer, javaPath, onProgress)
	if err != nil {
		return err
	}
	classpath = append(classpath, loaderCP...)
	if len(entry.JvmArgs) > 0 {
		opts.JvmArgs = strings.TrimSpace(strings.Join(entry.JvmArgs, " ") + " " + opts.JvmArgs)
	}
	instDir := InstanceDir(instName)
	os.MkdirAll(filepath.Join(instDir, "mods"), 0o755)
	log("  Default profile...")
	if err := ogulniegaEnsureProfile(lf, instDir, log, onProgress); err != nil {
		return err
	}
	log("  Ogulniega mods...")
	if err := ogulniegaEnsureMods(entry, instDir, log, onProgress); err != nil {
		return err
	}
	if acc.Name == "" {
		acc.Name = "Player"
	}
	return Launch(launchVersion, instName, acc, classpath, mainClass, javaPath, opts, log, state)
}
