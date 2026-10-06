// Package web embeds the built frontend (web/dist) into the executable.
package web

import (
	"embed"
	"io/fs"
)

// The all: prefix keeps the .gitkeep placeholder, so the package compiles before the
// frontend has been built.
//
//go:embed all:dist
var distribution embed.FS

// Files returns the built frontend rooted at dist/.
func Files() fs.FS {
	files, err := fs.Sub(distribution, "dist")
	if err != nil {
		panic(err)
	}
	return files
}
