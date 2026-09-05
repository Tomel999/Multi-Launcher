package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// pruneFixture builds a fake shared root: two versions sharing one library,
// each with a unique library and classifier, plus natives and asset indexes.
func pruneFixture(t *testing.T) string {
	t.Helper()
	restore := SetRootForTests(t.TempDir())
	t.Cleanup(restore)

	write := func(rel, content string) {
		p := filepath.Join(root(), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	meta := func(id, assetID string, libs ...Library) VersionMeta {
		return VersionMeta{ID: id, AssetIndex: struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		}{ID: assetID}, Libraries: libs}
	}
	lib := func(path string) Library {
		var l Library
		l.Downloads.Artifact.Path = path
		return l
	}
	libWithClassifier := func(path, classifierPath string) Library {
		l := lib(path)
		l.Downloads.Classifiers = map[string]Artifact{"natives-windows": {Path: classifierPath}}
		return l
	}
	shared := lib("com/mojang/shared-1.jar")
	for _, v := range []struct {
		id, asset string
		libs      []Library
	}{
		{"26.2", "26", []Library{shared, lib("com/mojang/only-26.2.jar"), libWithClassifier("com/mojang/base-26.2.jar", "com/mojang/base-26.2-natives.jar")}},
		{"26.1", "25", []Library{shared, lib("com/mojang/only-26.1.jar")}},
	} {
		data, _ := json.Marshal(meta(v.id, v.asset, v.libs...))
		write("versions/"+v.id+"/"+v.id+".json", string(data))
		write("versions/"+v.id+"/"+v.id+".jar", "jar-"+v.id)
		write("natives/"+v.id+"/natives.dat", "natives")
		write("assets/indexes/"+v.asset+".json", "{}")
	}
	write("libraries/com/mojang/shared-1.jar", "shared")
	write("libraries/com/mojang/only-26.2.jar", "unique")
	write("libraries/com/mojang/base-26.2.jar", "base")
	write("libraries/com/mojang/base-26.2-natives.jar", "base-natives")
	write("libraries/com/mojang/only-26.1.jar", "other")
	write("libraries/net/fabricmc/loader.jar", "fabric-must-survive")
	return root()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestPruneOrphanedFiles(t *testing.T) {
	root := pruneFixture(t)
	rep, err := PruneOrphanedFiles("26.2", map[string]bool{"26.1": true})
	if err != nil {
		t.Fatalf("PruneOrphanedFiles: %v", err)
	}
	if rep.FreedBytes <= 0 {
		t.Error("expected freed bytes to be accounted")
	}
	for _, gone := range []string{
		"versions/26.2",
		"natives/26.2",
		"assets/indexes/26.json",
		"libraries/com/mojang/only-26.2.jar",
		"libraries/com/mojang/base-26.2.jar",
		"libraries/com/mojang/base-26.2-natives.jar",
	} {
		if exists(filepath.Join(root, filepath.FromSlash(gone))) {
			t.Errorf("%s should have been removed", gone)
		}
	}
	for _, kept := range []string{
		"versions/26.1/26.1.json",
		"natives/26.1",
		"assets/indexes/25.json",
		"libraries/com/mojang/shared-1.jar",
		"libraries/com/mojang/only-26.1.jar",
		"libraries/net/fabricmc/loader.jar",
	} {
		if !exists(filepath.Join(root, filepath.FromSlash(kept))) {
			t.Errorf("%s should have been kept", kept)
		}
	}
}

func TestPruneOrphanedFilesKeepsUsedVersion(t *testing.T) {
	root := pruneFixture(t)
	rep, err := PruneOrphanedFiles("26.2", map[string]bool{"26.2": true, "26.1": true})
	if err != nil {
		t.Fatalf("PruneOrphanedFiles: %v", err)
	}
	if len(rep.Removed) != 0 || rep.FreedBytes != 0 {
		t.Errorf("used version must prune nothing, got %+v", rep)
	}
	if !exists(filepath.Join(root, "versions", "26.2")) {
		t.Error("versions/26.2 should still exist")
	}
}

func TestPruneOrphanedFilesMissingVersion(t *testing.T) {
	pruneFixture(t)
	rep, err := PruneOrphanedFiles("99.99", map[string]bool{})
	if err != nil {
		t.Fatalf("PruneOrphanedFiles: %v", err)
	}
	if len(rep.Removed) != 0 {
		t.Errorf("missing version must remove nothing, got %+v", rep)
	}
}

func TestPruneLoaderJar(t *testing.T) {
	root := pruneFixture(t)
	jar := filepath.Join(root, "loaders", "Forge", "47.2.0.jar")
	if err := os.MkdirAll(filepath.Dir(jar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jar, []byte("forge"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := PruneLoaderJar("Forge", "47.2.0", map[string]bool{})
	if err != nil {
		t.Fatalf("PruneLoaderJar: %v", err)
	}
	if exists(jar) {
		t.Error("unused loader jar should have been removed")
	}
	if rep.FreedBytes <= 0 {
		t.Error("expected freed bytes to be accounted")
	}

	if err := os.MkdirAll(filepath.Dir(jar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jar, []byte("forge"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err = PruneLoaderJar("Forge", "47.2.0", map[string]bool{"Forge\x0047.2.0": true})
	if err != nil {
		t.Fatalf("PruneLoaderJar: %v", err)
	}
	if !exists(jar) || len(rep.Removed) != 0 {
		t.Error("used loader jar must be kept")
	}
}
