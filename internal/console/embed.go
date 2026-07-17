package console

import (
	"embed"
	"io/fs"
)

// embeddedSPA holds the production frontend build baked into the binary. The
// build output is written to webdist/ by `mise run fe:build`; a checkout that
// has not built the frontend contains only the .gitkeep placeholder, so
// spaFS() reports whether a real build is present.
//
//go:embed all:webdist
var embeddedSPA embed.FS

// spaFS returns the embedded SPA rooted at webdist/, and whether a real build
// (an index.html) is present. When absent, the handler serves a placeholder.
func spaFS() (fs.FS, bool) {
	sub, err := fs.Sub(embeddedSPA, "webdist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}
