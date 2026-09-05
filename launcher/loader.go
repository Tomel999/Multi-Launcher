package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type loaderProfile struct {
	ID           string `json:"id"`
	InheritsFrom string `json:"inheritsFrom"`
	MainClass    string `json:"mainClass"`
	Arguments    struct {
		Game []json.RawMessage `json:"game"`
		JVM  []json.RawMessage `json:"jvm"`
	} `json:"arguments"`
	Libraries []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Size int64  `json:"size"`
		SHA1 string `json:"sha1"`
	} `json:"libraries"`
}

func EnsureLoader(loader, mcVersion, loaderVersion, javaPath string, onProgress ProgressFn) (string, []string, string, error) {
	switch loader {
	case "Fabric", "Quilt":
		cp, mc, err := installFabricLike(loader, mcVersion, loaderVersion, onProgress)
		return mcVersion, cp, mc, err
	case "Forge", "NeoForge":
		id, cp, mc, err := installForgeLike(loader, mcVersion, loaderVersion, javaPath, onProgress)
		return id, cp, mc, err
	}
	return "", nil, "", fmt.Errorf("unknown loader %q", loader)
}

func installFabricLike(loader, mcVersion, loaderVersion string, onProgress ProgressFn) ([]string, string, error) {
	var profileURL string
	if loader == "Fabric" {
		profileURL = fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/profile/json", mcVersion, loaderVersion)
	} else {
		profileURL = fmt.Sprintf("https://meta.quiltmc.org/v3/versions/loader/%s/%s/profile/json", mcVersion, loaderVersion)
	}
	var prof loaderProfile
	if err := fetchJSON(profileURL, &prof); err != nil {
		return nil, "", fmt.Errorf("%s profile: %w", loader, err)
	}
	var (
		classpath []string
		jobs      []job
	)
	for _, lib := range prof.Libraries {
		if lib.Name == "" {
			continue
		}
		if strings.Contains(lib.Name, ":natives-") {
			continue
		}
		path := mavenPath(lib.Name)
		dest := filepath.Join(librariesDir(), filepath.FromSlash(path))
		base := lib.URL
		if base == "" {
			base = "https://maven.fabricmc.net/"
		}
		classpath = append(classpath, dest)
		jobs = append(jobs, job{strings.TrimSuffix(base, "/") + "/" + path, dest, lib.Size, ""})
	}
	tr := newTracker(loader, onProgress)
	tr.setTotal(int64(len(jobs)), 0)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	if firstErr != nil {
		return nil, "", firstErr
	}
	tr.emit()
	return classpath, prof.MainClass, nil
}

func installForgeLike(loader, mcVersion, loaderVersion, javaPath string, onProgress ProgressFn) (string, []string, string, error) {
	var installerURL string
	if loader == "Forge" {
		id := mcVersion + "-" + loaderVersion
		installerURL = fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", id, id)
	} else {
		installerURL = fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", loaderVersion, loaderVersion)
	}
	dest := filepath.Join(Root(), "loaders", loader, loaderVersion+".jar")
	tr := newTracker(loader, onProgress)
	tr.setTotal(1, 0)
	if err := downloadTo(installerURL, dest, 0, tr); err != nil {
		return "", nil, "", err
	}
	tr.emit()

	if err := runInstaller(javaPath, dest); err != nil {
		return "", nil, "", err
	}

	id := loaderInstalledID(loader, mcVersion, loaderVersion)
	v, err := GetVersionMeta(id)
	if err != nil {
		return "", nil, "", fmt.Errorf("installer produced no version json for %s: %w", id, err)
	}
	os.Remove(dest)
	return id, buildInstallerClasspath(v, onProgress), v.MainClass, nil
}

func runInstaller(java, installerJar string) error {
	profiles := filepath.Join(Root(), "launcher_profiles.json")
	if _, err := os.Stat(profiles); err != nil {
		os.WriteFile(profiles, []byte(`{"profiles":{},"selectedProfile":"","clientToken":"00000000-0000-0000-0000-000000000000"}`), 0o644)
	}
	cmd := exec.Command(windowlessJava(java), "-jar", installerJar, "--installClient", Root())
	cmd.Dir = Root()
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("loader installer failed: %w\n%s", err, string(out))
	}
	return nil
}

func loaderInstalledID(loader, mcVersion, loaderVersion string) string {
	if loader == "Forge" {
		return mcVersion + "-forge-" + loaderVersion
	}
	return "neoforge-" + loaderVersion
}

func buildInstallerClasspath(v *VersionMeta, onProgress ProgressFn) []string {
	jar := ""
	if v.ID != "" {
		if _, err := os.Stat(clientJarPath(v.ID)); err == nil {
			jar = clientJarPath(v.ID)
		}
	}
	classpath := []string{}
	if jar != "" {
		classpath = append(classpath, jar)
	}
	var (
		jobs       []job
		nativeJars []Artifact
		totalBytes int64
	)
	for _, lib := range v.Libraries {
		if !rulesAllow(lib.Rules) {
			continue
		}
		if isModernNatives(lib) {
			if matchesModernNatives(lib.Name) {
				if art, ok := libraryArtifact(lib); ok {
					dest := libraryDest(art)
					nativeJars = append(nativeJars, art)
					jobs = append(jobs, job{art.URL, dest, art.Size, ""})
					totalBytes += art.Size
				}
			}
			continue
		}
		if lib.Downloads.Artifact.URL != "" || lib.Downloads.Artifact.Path != "" {
			dest := libraryDest(lib.Downloads.Artifact)
			classpath = append(classpath, dest)
			jobs = append(jobs, job{lib.Downloads.Artifact.URL, dest, lib.Downloads.Artifact.Size, ""})
			totalBytes += lib.Downloads.Artifact.Size
		}
		for classifier, nat := range lib.Downloads.Classifiers {
			nc, hasNative := lib.Natives[osName()]
			if hasNative && classifier == nc {
				dest := libraryDest(nat)
				nativeJars = append(nativeJars, nat)
				jobs = append(jobs, job{nat.URL, dest, nat.Size, ""})
				totalBytes += nat.Size
			}
		}
	}
	tr := newTracker("libraries", onProgress)
	tr.setTotal(int64(len(jobs)), totalBytes)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	tr.emit()
	if v.ID != "" {
		ndir := nativesDir(v.ID)
		for _, nat := range nativeJars {
			if err := extractZip(libraryDest(nat), ndir); err != nil {
				continue
			}
		}
	}
	return classpath
}
