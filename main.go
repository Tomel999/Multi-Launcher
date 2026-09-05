package main

import (
	"embed"
	"net/http"
	"path/filepath"
	"strings"

	"multilauncherwails/launcher"
	"multilauncherwails/launcher/plugin"
	"multilauncherwails/panorama"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Multi Launcher",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline' https:; font-src 'self' data: https:; img-src 'self' data: https:; connect-src 'self'")
					if strings.HasPrefix(r.URL.Path, "/panoramas/") {
						http.StripPrefix("/panoramas/", http.FileServer(http.Dir(panorama.CacheRoot()))).ServeHTTP(w, r)
						return
					}
					if strings.HasPrefix(r.URL.Path, "/skins/") {
						http.StripPrefix("/skins/", http.FileServer(http.Dir(launcher.SkinsDir()))).ServeHTTP(w, r)
						return
					}
					if strings.HasPrefix(r.URL.Path, "/plugins/") {
						rest := strings.TrimPrefix(r.URL.Path, "/plugins/")
						id, file, ok := strings.Cut(rest, "/frontend/")
						if ok && id != "" && file != "" {
							if !plugin.ValidID(id) {
								http.NotFound(w, r)
								return
							}
							dir := filepath.Join(pluginsRoot(), id, "frontend")
							http.StripPrefix("/plugins/"+id+"/frontend/", http.FileServer(http.Dir(dir))).ServeHTTP(w, r)
							return
						}
						http.NotFound(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		DragAndDrop:      &options.DragAndDrop{EnableFileDrop: true},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
