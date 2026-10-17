package webui

import (
	"io/fs"
	"testing"
)

// TestFSReportsAnInterfaceExactlyWhenItHasAnIndex pins the contract rather than
// the working tree. FS() must say "usable" exactly when the embed holds an
// index.html and return a nil file system otherwise. Whether the assets were
// copied in depends on the build (`make build`, the Docker build); a plain
// `go build` embeds only the committed placeholder — so a test with a fixed
// answer would follow the build state instead of the code.
func TestFSReportsAnInterfaceExactlyWhenItHasAnIndex(t *testing.T) {
	embedded, ok := FS()
	_, err := fs.Stat(assets, "assets/index.html")
	hasIndex := err == nil

	if ok != hasIndex {
		t.Fatalf("FS() ok=%v, but assets/index.html present=%v", ok, hasIndex)
	}
	if !ok {
		if embedded != nil {
			t.Fatal("FS() returned a file system alongside ok=false")
		}
		return
	}
	// When it says usable, the returned FS is rooted at the assets: the index is
	// reachable at "index.html", not at "assets/index.html".
	if _, err := fs.Stat(embedded, "index.html"); err != nil {
		t.Fatalf("index.html is not reachable in the returned file system: %v", err)
	}
}
