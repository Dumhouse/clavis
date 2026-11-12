package server

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/pkg/problem"
)

const notBuiltPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Clavis — web UI not built</title>
<style>body{font-family:ui-sans-serif,system-ui,sans-serif;background:#09090b;color:#fafafa;display:grid;place-items:center;height:100vh;margin:0}main{max-width:36rem;padding:2rem}code{background:#1a1a1f;border:1px solid #26262c;border-radius:6px;padding:.15rem .4rem}</style>
</head><body><main><h1>Web UI not built</h1>
<p>The API is running, but this binary was compiled without the React app.</p>
<p>Run <code>make web</code> then rebuild, or use <code>make dev</code> for hot reload.</p>
</main></body></html>`

// spaHandler serves the embedded SPA with history fallback. In dev mode it
// proxies to the Vite dev server for hot reload (M0-29).
func spaHandler(cfg *config.Config, dist fs.FS) http.Handler {
	var devProxy http.Handler
	if cfg.Server.Dev {
		target := os.Getenv("CLAVIS_WEB_DEV_URL")
		if target == "" {
			target = "http://127.0.0.1:5173"
		}
		if u, err := url.Parse(target); err == nil && u.Host != "" {
			devProxy = httputil.NewSingleHostReverseProxy(u) //nolint:gosec // G704: dev-only proxy to the operator-configured Vite server
		}
	}

	fileServer := http.FileServer(http.FS(dist))
	index, indexErr := fs.ReadFile(dist, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			p := problem.New(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
			problem.Write(w, p)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			problem.Write(w, problem.NotFound("No such API endpoint."))
			return
		}
		if devProxy != nil {
			devProxy.ServeHTTP(w, r)
			return
		}
		if indexErr != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, notBuiltPage)
			}
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		info, err := fs.Stat(dist, name)
		if err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		if path.Ext(name) != "" {
			problem.Write(w, problem.NotFound("No such static file."))
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(index)
		}
	})
}
