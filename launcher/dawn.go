package launcher

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func dawnManifestNegative(version string) bool {
	p := filepath.Join(featherBaseDir("dawn"), "versions", "dawn", version+".404")
	fi, err := os.Stat(p)
	return err == nil && time.Since(fi.ModTime()) < 6*time.Hour
}

func writeDawnManifestNegative(version string) {
	p := filepath.Join(featherBaseDir("dawn"), "versions", "dawn", version+".404")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
}

func fakeAccessToken(username, uuid string) string {
	sum := sha1.Sum([]byte(uuid + ":" + username + ":dawn"))
	return hex.EncodeToString(sum[:])
}

func dawnAuthProxy(username, uuid string) (string, func()) {
	fakeProfile := []byte(`{"id":"` + uuid + `","name":"` + username + `","properties":[{"name":"premium","value":"true"}]}`)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		var body []byte
		switch {
		case strings.Contains(p, "profile") || strings.Contains(p, "user"):
			body = fakeProfile
		case strings.Contains(p, "certificates"):
			body = []byte(`{"profiles":[]}`)
		case strings.Contains(p, "join"):
			w.WriteHeader(http.StatusNoContent)
			return
		case strings.Contains(p, "privileges"):
			body = []byte(`{"onlineChat":{"enabled":true},"multiplayerServer":{"enabled":true},"multiplayerRealms":{"enabled":true},"telemetry":{"enabled":false}}`)
		case strings.Contains(p, "drm") || strings.Contains(p, "license") || strings.Contains(p, "entitlement"):
			body = []byte(`{"valid":true,"reason":"ok","entitlements":["game"]}`)
		case strings.Contains(p, "authenticate") || strings.Contains(p, "refresh") || strings.Contains(p, "validate") || strings.Contains(p, "token"):
			body = []byte(`{"accessToken":"fake","clientToken":"fake"}`)
		default:
			body = []byte(`{"status":"OK"}`)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write(body)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", func() {}
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	return "http://127.0.0.1:" + fmt.Sprint(ln.Addr().(*net.TCPAddr).Port) + "/", func() { srv.Close() }
}
