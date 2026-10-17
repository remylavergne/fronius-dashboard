// Package webui exposes the single-page interface carried by the binary.
//
// The assets are compiled in from internal/webui/assets, which is committed
// with only a .gitkeep placeholder: `go:embed` refuses a directory that matches
// no file, and the directory must exist for a plain `go build` to work without
// a frontend build. `make build` and the Docker build copy the real files in
// before compiling; the embedded set is therefore empty in a plain build and
// FS reports that honestly instead of pretending to serve a frontend.
package webui

import (
	"embed"
	"io/fs"
)

// assets holds everything under internal/webui/assets at compile time. The
// `all:` prefix also matches files whose name starts with "." or "_", so the
// placeholder alone keeps the pattern valid.
//
//go:embed all:assets
var assets embed.FS

// FS returns the interface files rooted at the assets directory, and whether
// this binary carries a real frontend.
//
// The second result is false when the assets hold no index.html — the plain
// build, where only .gitkeep is embedded. Callers then behave as if no
// interface were compiled in and fall back to their own serving or to the API
// alone. The first result is nil in that case, so it can never be mistaken for
// a usable file system.
func FS() (fs.FS, bool) {
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		// Unreachable: the embed directive guarantees a directory named
		// assets. Report "nothing embedded" rather than a partial source.
		return nil, false
	}
	if _, err := fs.Stat(root, "index.html"); err != nil {
		return nil, false
	}
	return root, true
}
