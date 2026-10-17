package api

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// resolveAssets selects the frontend source, in decreasing precedence:
//
//  1. an explicit FRONIUS_WEB_DIR always wins, even when it is incomplete — an
//     operator who points at a directory expects that directory to be the
//     truth, not a silent fallback to the embedded copy;
//  2. the interface embedded at build time, when this binary was built with
//     assets (webui.FS reports one);
//  3. nothing, which leaves the API-only behaviour.
//
// The embedded probe is a function so that the precedence can be tested without
// rebuilding the binary. nil means "no frontend".
func resolveAssets(webDir string, embedded func() (fs.FS, bool)) fs.FS {
	if webDir != "" {
		return os.DirFS(webDir)
	}
	if assets, ok := embedded(); ok {
		return assets
	}
	return nil
}

// staticHandler serves the interface from assets and delegates everything else
// to the API. The same implementation serves a directory (os.DirFS) and the
// compiled-in copy (embed.FS), so both sources cannot drift apart.
//
// Behaviour, pinned by tests:
//   - /api/ paths always reach the API, never the interface;
//   - an existing file is served as-is: its Content-Type from the extension,
//     its Last-Modified when the source carries a modification time, and no
//     Cache-Control of its own;
//   - a missing path without an extension falls back to index.html, which is
//     what client-side routes need;
//   - a missing path that carries an extension is an asset miss. It is not a
//     route: it must stay a 404 instead of returning HTML for a script or a
//     stylesheet the browser asked for. The API answers, so the error keeps the
//     JSON envelope of every other 404;
//   - a source without index.html still serves the files it holds; it only
//     loses the routing fallback, and the API answers for the rest. A
//     FRONIUS_WEB_DIR pointing at an incomplete build keeps its assets.
//
// A nil assets is the API-only case and returns api unchanged.
func staticHandler(api http.Handler, assets fs.FS) http.Handler {
	if assets == nil {
		return api
	}
	files := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		// path.Clean on a rooted path removes any ".." segment, and fs.FS
		// rejects the rest: the request can only address the asset tree.
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" {
			if info, err := fs.Stat(assets, name); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
			if path.Ext(name) != "" {
				api.ServeHTTP(w, r)
				return
			}
		}
		// Stat per request, not once at construction: a deployment into an
		// explicit WebDir is picked up without a restart, as before.
		if _, err := fs.Stat(assets, "index.html"); err != nil {
			api.ServeHTTP(w, r)
			return
		}
		// Root, a directory, or an extension-less route: the SPA entry point.
		http.ServeFileFS(w, r, assets, "index.html")
	})
}
