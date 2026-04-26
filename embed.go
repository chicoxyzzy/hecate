// Package hecate is a thin module-root package whose only job is hosting the
// //go:embed directive for the built UI assets. Go's embed directive cannot
// reach paths above the source file's directory, so the embed has to live at
// the module root where ui/dist sits.
package hecate

import (
	"embed"
	"io/fs"
)

// UIDistFS holds the built UI bundle. The Makefile / CI runs `bun run build`
// before `go build`, which populates ui/dist with the real React app. When
// that hasn't happened, the embedded directory contains only the .gitkeep
// placeholder and consumers fall back to a friendly "UI not built" page.
//
//go:embed all:ui/dist
var UIDistFS embed.FS

// UISubFS returns the embedded ui/dist subtree (or nil if the embed is
// somehow unreadable). Callers should treat nil as "UI not built" and serve
// a fallback page.
func UISubFS() fs.FS {
	sub, err := fs.Sub(UIDistFS, "ui/dist")
	if err != nil {
		return nil
	}
	return sub
}
