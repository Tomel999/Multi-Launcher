package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func lunarAssetIndexID(version string) string {
	mapping := map[string]string{
		"1.7.10": "1.7.10", "1.8.9": "1.8", "1.12.2": "1.12", "1.16.5": "1.16",
		"1.17.1": "1.17", "1.18.2": "1.18", "1.19.4": "1.19", "1.20.1": "1.20",
		"1.20.4": "1.20", "1.21": "1.21", "1.21.1": "1.21", "1.21.3": "1.21",
		"1.21.4": "1.21",
	}
	if id, ok := mapping[version]; ok {
		return id
	}
	return version
}

func lunarInstanceAssetsDir(instName string) string {
	p := filepath.Join(InstanceDir(instName), "assets")
	os.MkdirAll(p, 0o755)
	return p
}

func lunarEnsureAssets(version, instName string, log LogFn, onProgress ProgressFn) (string, error) {
	minor := 0
	if parts := strings.Split(version, "."); len(parts) >= 2 {
		fmt.Sscanf(parts[1], "%d", &minor)
	}
	if minor < 6 && strings.HasPrefix(version, "1.") {
		if minor < 3 || strings.HasPrefix(version, "1.5") {
			return "pre-1.6", nil
		}
		return "legacy", nil
	}
	indexID := lunarAssetIndexID(version)
	sharedIndexes := filepath.Join(assetsDir(), "indexes")
	os.MkdirAll(sharedIndexes, 0o755)
	sharedIndex := filepath.Join(sharedIndexes, indexID+".json")
	indexes := filepath.Join(lunarInstanceAssetsDir(instName), "indexes")
	os.MkdirAll(indexes, 0o755)
	indexFile := filepath.Join(indexes, indexID+".json")
	if _, err := os.Stat(indexFile); err == nil {
		if _, err := os.Stat(sharedIndex); err != nil {
			linkOrCopy(indexFile, sharedIndex)
		}
	} else if _, err := os.Stat(sharedIndex); err == nil {
		linkOrCopy(sharedIndex, indexFile)
	}
	if _, err := os.Stat(sharedIndex); err != nil {
		var manifest struct {
			Versions []struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			} `json:"versions"`
		}
		if err := fetchJSON(lunarVersionManifest, &manifest); err != nil {
			return "", err
		}
		for _, v := range manifest.Versions {
			if v.ID != version {
				continue
			}
			var vdata struct {
				AssetIndex struct {
					ID  string `json:"id"`
					URL string `json:"url"`
				} `json:"assetIndex"`
			}
			if err := fetchJSON(v.URL, &vdata); err != nil {
				return "", err
			}
			if vdata.AssetIndex.URL == "" {
				return "", fmt.Errorf("no asset index for %s", version)
			}
			if err := downloadTo(vdata.AssetIndex.URL, sharedIndex, 0, nil); err != nil {
				return "", err
			}
			break
		}
		mirrorFile(sharedIndex, indexFile)
	}
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return "", err
	}
	var idx struct {
		Objects map[string]struct {
			Hash string `json:"hash"`
			Size int64  `json:"size"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		return "", err
	}
	sharedObjects := filepath.Join(assetsDir(), "objects")
	instObjects := filepath.Join(lunarInstanceAssetsDir(instName), "objects")
	jobs := make([]job, 0, len(idx.Objects))
	var totalBytes int64
	for _, o := range idx.Objects {
		if len(o.Hash) < 2 {
			continue
		}
		rel := filepath.Join(o.Hash[:2], o.Hash)
		shared := filepath.Join(sharedObjects, rel)
		if inst, err := os.Stat(filepath.Join(instObjects, rel)); err == nil && inst.Size() > 0 {
			mirrorFile(filepath.Join(instObjects, rel), shared)
		}
		jobs = append(jobs, job{
			url:  fmt.Sprintf("%s/%s/%s", lunarResourceURL, o.Hash[:2], o.Hash),
			dest: shared,
			size: o.Size,
		})
		totalBytes += o.Size
	}
	tr := newTracker("lunar-assets", onProgress)
	tr.setTotal(int64(len(jobs)), totalBytes)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	if firstErr != nil {
		return "", firstErr
	}
	for _, j := range jobs {
		rel, err := filepath.Rel(sharedObjects, j.dest)
		if err != nil {
			continue
		}
		mirrorFile(j.dest, filepath.Join(instObjects, rel))
	}
	tr.emit()
	return indexID, nil
}

func lunarEnsureVanilla(version, instName string, log LogFn, onProgress ProgressFn) error {
	base := InstanceDir(instName)
	verDir := filepath.Join(base, "versions", version)
	os.MkdirAll(verDir, 0o755)
	jsonFile := filepath.Join(verDir, version+".json")
	sharedJSON := versionJSONPath(version)
	if _, err := os.Stat(jsonFile); err == nil {
		if _, err := os.Stat(sharedJSON); err != nil {
			os.MkdirAll(filepath.Dir(sharedJSON), 0o755)
			linkOrCopy(jsonFile, sharedJSON)
		}
	} else if _, err := os.Stat(sharedJSON); err == nil {
		linkOrCopy(sharedJSON, jsonFile)
	}
	var v VersionMeta
	if _, err := os.Stat(sharedJSON); err != nil {
		var manifest struct {
			Versions []struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			} `json:"versions"`
		}
		if err := fetchJSON(lunarVersionManifest, &manifest); err != nil {
			return err
		}
		url := ""
		for _, mv := range manifest.Versions {
			if mv.ID == version {
				url = mv.URL
				break
			}
		}
		if url == "" {
			log("  " + version + " missing from Mojang manifest — Genesis will fetch it")
			return nil
		}
		if err := downloadTo(url, sharedJSON, 0, nil); err != nil {
			return err
		}
		mirrorFile(sharedJSON, jsonFile)
	}
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.Downloads.Client.URL == "" && len(v.Libraries) == 0 {
		return nil
	}
	jobs := make([]job, 0, len(v.Libraries)+1)
	var totalBytes int64
	jarPath := filepath.Join(verDir, version+".jar")
	sharedJar := clientJarPath(version)
	sharedJarOK := false
	if v.Downloads.Client.SHA1 != "" {
		if h, err := sha1File(sharedJar); err == nil && h == v.Downloads.Client.SHA1 {
			sharedJarOK = true
		} else if h, err := sha1File(jarPath); err == nil && h == v.Downloads.Client.SHA1 {
			os.MkdirAll(filepath.Dir(sharedJar), 0o755)
			os.Remove(sharedJar)
			if linkOrCopy(jarPath, sharedJar) == nil {
				sharedJarOK = true
			}
		}
		if sharedJarOK {
			if h, err := sha1File(jarPath); err != nil || h != v.Downloads.Client.SHA1 {
				os.Remove(jarPath)
			}
			mirrorFile(sharedJar, jarPath)
		}
	}
	needJar := v.Downloads.Client.SHA1 != "" && !sharedJarOK
	if needJar {
		os.Remove(sharedJar)
		jobs = append(jobs, job{url: v.Downloads.Client.URL, dest: sharedJar, size: v.Downloads.Client.Size})
		totalBytes += v.Downloads.Client.Size
	}
	sharedLibs := librariesDir()
	instLibs := filepath.Join(base, "libraries")
	for _, lib := range v.Libraries {
		if !rulesAllow(lib.Rules) {
			continue
		}
		if isModernNatives(lib) {
			if strings.HasSuffix(lib.Name, modernNativesClassifier()) && lib.Downloads.Artifact.URL != "" {
				jobs = append(jobs, seedSharedLibJob(lib.Downloads.Artifact, sharedLibs, instLibs))
				totalBytes += lib.Downloads.Artifact.Size
			}
			continue
		}
		if lib.Downloads.Artifact.URL != "" {
			jobs = append(jobs, seedSharedLibJob(lib.Downloads.Artifact, sharedLibs, instLibs))
			totalBytes += lib.Downloads.Artifact.Size
		}
		if nc, ok := lib.Natives[osName()]; ok {
			if nat, ok := lib.Downloads.Classifiers[nc]; ok && nat.URL != "" {
				jobs = append(jobs, seedSharedLibJob(nat, sharedLibs, instLibs))
				totalBytes += nat.Size
			}
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	log(fmt.Sprintf("  Downloading game files: %d files (%.1f MB)", len(jobs), float64(totalBytes)/(1024*1024)))
	tr := newTracker("lunar-game", onProgress)
	tr.setTotal(int64(len(jobs)), totalBytes)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	if firstErr != nil {
		return firstErr
	}
	if needJar {
		if h, err := sha1File(sharedJar); err != nil || h != v.Downloads.Client.SHA1 {
			os.Remove(sharedJar)
			return fmt.Errorf("client jar sha1 mismatch for %s", version)
		}
		os.Remove(jarPath)
		mirrorFile(sharedJar, jarPath)
	}
	for _, j := range jobs {
		if j.dest == sharedJar {
			continue
		}
		rel, err := filepath.Rel(sharedLibs, j.dest)
		if err != nil {
			continue
		}
		mirrorFile(j.dest, filepath.Join(instLibs, rel))
	}
	tr.emit()
	return nil
}

func seedSharedLibJob(art Artifact, sharedLibs, instLibs string) job {
	shared := filepath.Join(sharedLibs, filepath.FromSlash(art.Path))
	if inst, err := os.Stat(filepath.Join(instLibs, filepath.FromSlash(art.Path))); err == nil && inst.Size() > 0 {
		mirrorFile(filepath.Join(instLibs, filepath.FromSlash(art.Path)), shared)
	}
	return job{url: art.URL, dest: shared, size: art.Size}
}
