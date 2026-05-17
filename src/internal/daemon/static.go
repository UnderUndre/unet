package daemon

import (
	"bytes"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/underundre/unet/web"
)

// staticHandler serves the embedded React frontend assets.
// For SPA routing: non-API, non-static-asset paths return index.html.
type staticHandler struct {
	fileServer http.Handler
	indexHTML  []byte
	fileSystem fs.FS
}

// newStaticHandler creates a handler that serves embedded web/dist/ assets.
func newStaticHandler() *staticHandler {
	sub := web.DistSub()

	// Read index.html once for SPA fallback responses.
	indexHTML, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		slog.Error("failed to read embedded index.html", "error", err)
		indexHTML = []byte("<!DOCTYPE html><html><body><h1>Frontend not built</h1></body></html>")
	}

	return &staticHandler{
		fileServer: http.FileServer(http.FS(sub)),
		indexHTML:  indexHTML,
		fileSystem: sub,
	}
}

// ServeHTTP implements http.Handler.
func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the request path.
	cleanPath := path.Clean(r.URL.Path)

	// Try to serve an actual static file first.
	if cleanPath != "/" && cleanPath != "/index.html" {
		// Check if the file exists in the embedded FS.
		if h.fileExists(cleanPath) {
			// Set long-lived cache headers for hashed static assets.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			h.fileServer.ServeHTTP(w, r)
			return
		}
	}

	// SPA fallback: return index.html for all other routes.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(h.indexHTML))
}

// fileExists checks whether a file exists at the given path in the embedded FS.
func (h *staticHandler) fileExists(filePath string) bool {
	// Strip leading slash.
	name := strings.TrimPrefix(filePath, "/")
	if name == "" {
		return false
	}

	stat, err := fs.Stat(h.fileSystem, name)
	if err != nil {
		return false
	}
	// Only serve regular files, not directories.
	return !stat.IsDir()
}

// emptyFS is a minimal fs.FS implementation used as a fallback.
type emptyFS struct{}

func (emptyFS) Open(_ string) (fs.File, error) { return nil, fs.ErrNotExist }
