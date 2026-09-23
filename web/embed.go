package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var assets embed.FS

func Handler() (http.Handler, error) {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil, fmt.Errorf("open UIR web assets: %w", err)
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read UIR web entry point: %w", err)
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(index)
			return
		}
		if _, err := fs.Stat(dist, name); err == nil {
			files.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	}), nil
}
