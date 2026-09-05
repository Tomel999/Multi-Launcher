package launcher

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var iconCache = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

var jarMu sync.Mutex
var jarAttempted = map[string]bool{}

func ensureClientJar(mcVersion string) {
	jarMu.Lock()
	if jarAttempted[mcVersion] {
		jarMu.Unlock()
		return
	}
	jarAttempted[mcVersion] = true
	jarMu.Unlock()
	go func() {
		if v, err := GetVersionMeta(mcVersion); err == nil {
			InstallClientJar(v, nil)
		}
	}()
}

func GetItemIcon(instName, mcVersion, itemID string) string {
	key := instName + "|" + itemID
	iconCache.Lock()
	if v, ok := iconCache.m[key]; ok {
		iconCache.Unlock()
		return v
	}
	iconCache.Unlock()

	ns, path, ok := strings.Cut(itemID, ":")
	if !ok {
		ns, path = "minecraft", itemID
	}
	var b64 string
	if ns == "minecraft" {
		jar := clientJarPath(mcVersion)
		if _, err := os.Stat(jar); err != nil {
			ensureClientJar(mcVersion)
			return ""
		}
		b64 = iconFromJar(jar, ns, path)
	} else {
		b64 = iconFromMods(InstanceDir(instName), ns, path)
	}

	iconCache.Lock()
	if b64 != "" {
		iconCache.m[key] = b64
	}
	iconCache.Unlock()
	return b64
}

func iconFromMods(instDir, ns, path string) string {
	jars, _ := filepath.Glob(filepath.Join(instDir, "mods", "*.jar"))
	for _, j := range jars {
		if b64 := iconFromJar(j, ns, path); b64 != "" {
			return b64
		}
	}
	return ""
}

func iconFromJar(jarPath, ns, name string) string {
	base := "assets/" + ns + "/"
	if b64 := jarEntryPNG(jarPath, base+"textures/item/"+name+".png"); b64 != "" {
		return b64
	}
	if b64 := jarEntryPNG(jarPath, base+"textures/block/"+name+".png"); b64 != "" {
		return b64
	}
	if entry := modelTexture(jarPath, base+"items/"+name+".json"); entry != "" {
		return jarEntryPNG(jarPath, entry)
	}
	return ""
}

func modelTexture(jarPath, entry string) string {
	textures := map[string]string{}
	data := readJarEntry(jarPath, entry)
	if data == nil {
		return ""
	}
	var im struct {
		Model struct {
			Model string `json:"model"`
		} `json:"model"`
	}
	if json.Unmarshal(data, &im) == nil && im.Model.Model != "" {
		ns, path, _ := strings.Cut(im.Model.Model, ":")
		collectModelTextures(jarPath, "assets/"+ns+"/models/"+path+".json", 0, textures)
	} else {
		collectModelTextures(jarPath, entry, 0, textures)
	}
	for _, k := range []string{"up", "particle", "top"} {
		if t, ok := textures[k]; ok {
			if entry := textureEntry(t, textures); entry != "" {
				return entry
			}
		}
	}
	for _, t := range textures {
		if entry := textureEntry(t, textures); entry != "" {
			return entry
		}
	}
	return ""
}

func collectModelTextures(jarPath, entry string, depth int, acc map[string]string) {
	if depth > 8 {
		return
	}
	data := readJarEntry(jarPath, entry)
	if data == nil {
		return
	}
	var m struct {
		Parent   string            `json:"parent"`
		Textures map[string]string `json:"textures"`
	}
	if json.Unmarshal(data, &m) != nil {
		return
	}
	for k, v := range m.Textures {
		if _, ok := acc[k]; !ok {
			acc[k] = v
		}
	}
	if m.Parent != "" {
		ns, path, _ := strings.Cut(m.Parent, ":")
		collectModelTextures(jarPath, "assets/"+ns+"/models/"+path+".json", depth+1, acc)
	}
}

func textureEntry(ref string, textures map[string]string) string {
	if strings.HasPrefix(ref, "#") {
		if v, ok := textures[strings.TrimPrefix(ref, "#")]; ok && v != ref {
			return textureEntry(v, textures)
		}
		return ""
	}
	ns, path, _ := strings.Cut(ref, ":")
	return "assets/" + ns + "/textures/" + path + ".png"
}

func jarEntryPNG(jarPath, entry string) string {
	data := readJarEntry(jarPath, entry)
	if data == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func readJarEntry(jarPath, entry string) []byte {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == entry {
			rc, err := f.Open()
			if err != nil {
				return nil
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return nil
			}
			return data
		}
	}
	return nil
}
