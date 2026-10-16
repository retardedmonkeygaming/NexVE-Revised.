// Package web embeds the compiled frontend (frontend/dist -> dist/) into the
// single nexved binary. `make build` copies the Vite output here before
// `go build`, so a production install is exactly one file.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded frontend filesystem rooted at dist/.
// When the frontend has not been built, dist/ contains only a .gitkeep and
// the API layer serves a friendly "build the frontend" placeholder page.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("web: embedded dist missing: " + err.Error()) // cannot happen with go:embed
	}
	return sub
}

// Built reports whether a real frontend bundle is embedded.
func Built() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
