package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static/*
var assets embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(assets, "static")
	files := http.FileServer(http.FS(sub))
	index, _ := fs.ReadFile(sub, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // The desktop embeds a persistent WebView2 user-data profile, while
        // UI assets keep stable /app.js and /style.css paths across upgrades.
        // Never reuse yesterday's UI after a successful backend upgrade.
        w.Header().Set("Cache-Control","no-store, max-age=0")
        w.Header().Set("Pragma","no-cache")
        if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "." || p == "" || p == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write(index)
			}
			return
		}

		if _, err := fs.Stat(sub, p); err != nil {
			// SPA fallback: serve the shell directly. Never hand /index.html to
			// http.FileServer because it canonically redirects it to ./, which
			// would turn an SPA fallback into an infinite redirect loop.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write(index)
			}
			return
		}

		rr := r.Clone(r.Context())
		rr.URL.Path = "/" + p
		files.ServeHTTP(w, rr)
	})
}
