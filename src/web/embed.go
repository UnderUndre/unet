package web

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var DistFS embed.FS

// DistSub returns an fs.FS rooted at the dist/ directory, suitable for
// serving with http.FileServer.
func DistSub() fs.FS {
	sub, err := fs.Sub(DistFS, "dist")
	if err != nil {
		// Should never happen with a valid embed.
		panic("web: failed to create sub filesystem: " + err.Error())
	}
	return sub
}
