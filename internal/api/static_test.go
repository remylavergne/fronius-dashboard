package api

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// apiOnly answers every request with a JSON 404, like apiMux does for unknown
// paths. Its body identifies the delegation: a test can then tell "the API
// answered" from "the interface served a file".
func apiOnly() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found"}}`)
	})
}

// mapAssets is a minimal interface tree. The mode is explicit so the source is
// regular files; the modification time stays zero, as with embed.FS.
func mapAssets(index string) fstest.MapFS {
	return fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte(index), Mode: 0o644},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)"), Mode: 0o644},
	}
}

func serve(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// TestStaticHandlerFilesAndFallback pins the serving rules for an fs.FS source.
func TestStaticHandlerFilesAndFallback(t *testing.T) {
	h := staticHandler(apiOnly(), mapAssets("<html>app</html>"))

	t.Run("index at the root", func(t *testing.T) {
		rec := serve(h, http.MethodGet, "/")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET / = %d, want 200", rec.Code)
		}
		if got := rec.Body.String(); got != "<html>app</html>" {
			t.Fatalf("GET / body = %q", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Fatalf("GET / Content-Type = %q", got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "" {
			t.Fatalf("GET / Cache-Control = %q, want none", got)
		}
	})

	t.Run("real file served as-is", func(t *testing.T) {
		rec := serve(h, http.MethodGet, "/assets/app.js")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /assets/app.js = %d, want 200", rec.Code)
		}
		if got := rec.Body.String(); got != "console.log(1)" {
			t.Fatalf("GET /assets/app.js body = %q", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
			t.Fatalf("GET /assets/app.js Content-Type = %q", got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "" {
			t.Fatalf("GET /assets/app.js Cache-Control = %q, want none", got)
		}
	})

	t.Run("extension-less route falls back to index", func(t *testing.T) {
		for _, target := range []string{"/capabilities", "/settings/network", "/deep/link"} {
			rec := serve(h, http.MethodGet, target)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", target, rec.Code)
			}
			if got := rec.Body.String(); got != "<html>app</html>" {
				t.Fatalf("GET %s body = %q, want index", target, got)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Fatalf("GET %s Content-Type = %q", target, got)
			}
		}
	})

	t.Run("missing asset stays a 404", func(t *testing.T) {
		rec := serve(h, http.MethodGet, "/assets/missing.css")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /assets/missing.css = %d, want 404", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "app") {
			t.Fatalf("a missing asset must not fall back to index: %q", rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("the API must answer the asset miss, Content-Type = %q", got)
		}
	})

	t.Run("api paths bypass the interface", func(t *testing.T) {
		rec := serve(h, http.MethodGet, "/api/v1/live")
		if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("GET /api/v1/live = %d %q", rec.Code, rec.Header().Get("Content-Type"))
		}
		if strings.Contains(rec.Body.String(), "app") {
			t.Fatalf("an /api/ path reached the interface: %q", rec.Body.String())
		}
	})

	t.Run("explicit index.html redirects to the root path", func(t *testing.T) {
		rec := serve(h, http.MethodGet, "/index.html")
		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("GET /index.html = %d, want 301", rec.Code)
		}
		if got := rec.Header().Get("Location"); got != "./" {
			t.Fatalf("Location = %q, want %q", got, "./")
		}
	})

	t.Run("no other method special-cased", func(t *testing.T) {
		// The static service does not police verbs: FileServer answers HEAD
		// and rejects POST for a real file, exactly as before.
		rec := serve(h, http.MethodHead, "/assets/app.js")
		if rec.Code != http.StatusOK {
			t.Fatalf("HEAD /assets/app.js = %d, want 200", rec.Code)
		}
	})
}

// TestStaticHandlerWithoutIndex: a source with no index.html still serves the
// files it holds — the old handler did, and an incomplete FRONIUS_WEB_DIR must
// not make its assets disappear. Only the routing fallback is lost.
func TestStaticHandlerWithoutIndex(t *testing.T) {
	assets := fstest.MapFS{
		"app.js": &fstest.MapFile{Data: []byte("console.log(1)"), Mode: 0o644},
	}
	h := staticHandler(apiOnly(), assets)

	rec := serve(h, http.MethodGet, "/app.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("GET /app.js = %d %q, want the real file", rec.Code, rec.Body.String())
	}
	for _, target := range []string{"/", "/route", "/missing.js"} {
		rec := serve(h, http.MethodGet, target)
		if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("GET %s = %d %q, want the API's JSON 404", target, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

// TestStaticHandlerNilIsAPIOnly covers the no-frontend case without going
// through webui.FS.
func TestStaticHandlerNilIsAPIOnly(t *testing.T) {
	h := staticHandler(apiOnly(), nil)
	rec := serve(h, http.MethodGet, "/")
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET / = %d %q, want the API's JSON 404", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// TestResolveAssetsPrecedence pins the order: an explicit directory wins over
// the embedded copy, the embedded copy is used when no directory is set, and
// neither yields nil.
func TestResolveAssetsPrecedence(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("from-dir"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	embedded := func() (fs.FS, bool) { return mapAssets("from-embed"), true }

	readIndex := func(t *testing.T, source fs.FS) string {
		t.Helper()
		if source == nil {
			t.Fatal("source is nil")
		}
		data, err := fs.ReadFile(source, "index.html")
		if err != nil {
			t.Fatalf("ReadFile(index.html): %v", err)
		}
		return string(data)
	}

	t.Run("WebDir wins over the embedded copy", func(t *testing.T) {
		// Handler-level, so the precedence is observed end to end.
		h := staticHandler(apiOnly(), resolveAssets(webDir, embedded))
		rec := serve(h, http.MethodGet, "/")
		if got := rec.Body.String(); got != "from-dir" {
			t.Fatalf("GET / body = %q, want the directory's index", got)
		}
	})

	t.Run("embedded copy used without WebDir", func(t *testing.T) {
		source := resolveAssets("", embedded)
		if got := readIndex(t, source); got != "from-embed" {
			t.Fatalf("index = %q, want the embedded one", got)
		}
		h := staticHandler(apiOnly(), source)
		rec := serve(h, http.MethodGet, "/")
		if got := rec.Body.String(); got != "from-embed" {
			t.Fatalf("GET / body = %q, want the embedded index", got)
		}
	})

	t.Run("neither source yields nil", func(t *testing.T) {
		if source := resolveAssets("", func() (fs.FS, bool) { return nil, false }); source != nil {
			t.Fatal("resolveAssets returned a source with no WebDir and no embedded interface")
		}
	})
}

// TestHandlerUsesEmbeddedSource checks the wiring in New: with no WebDir and an
// embedded interface, Handler serves it. The real webui.FS has no interface in
// this repository's plain build, so the selection helper is exercised with a
// stub here and webui.FS itself is covered by its package test.
func TestHandlerUsesEmbeddedSource(t *testing.T) {
	h := staticHandler(apiOnly(), mapAssets("<html>embedded</html>"))
	rec := serve(h, http.MethodGet, "/dashboard")
	if rec.Code != http.StatusOK || rec.Body.String() != "<html>embedded</html>" {
		t.Fatalf("GET /dashboard = %d %q", rec.Code, rec.Body.String())
	}
}

// TestDirectorySourceUsesTheSameHandler proves os.DirFS is not a second code
// path: the same handler serves a real directory with the same rules.
func TestDirectorySourceUsesTheSameHandler(t *testing.T) {
	webDir := t.TempDir()
	files := map[string]string{
		"index.html":       "<html>dir-app</html>",
		"assets/style.css": "body{}",
	}
	for name, content := range files {
		full := filepath.Join(webDir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	h := staticHandler(apiOnly(), os.DirFS(webDir))

	if rec := serve(h, http.MethodGet, "/assets/style.css"); rec.Code != http.StatusOK || rec.Body.String() != "body{}" {
		t.Fatalf("GET /assets/style.css = %d %q", rec.Code, rec.Body.String())
	}
	if rec := serve(h, http.MethodGet, "/route"); rec.Code != http.StatusOK || rec.Body.String() != "<html>dir-app</html>" {
		t.Fatalf("GET /route = %d %q", rec.Code, rec.Body.String())
	}
	if rec := serve(h, http.MethodGet, "/missing.js"); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /missing.js = %d, want 404", rec.Code)
	}
}
