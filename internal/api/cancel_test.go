// A client that goes away cancels the request context: a page reload, a closed
// tab, or the Vite proxy aborting upstream when its client disconnects. The
// store surfaces that cancellation, and the handlers must neither answer nor log
// it as a server failure. Reported from a real run: a fast reload produced six
// ERROR lines ("internal error", "reading device state failed", health reads).
package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"fronius-supervisor/internal/storage"
)

func TestClientCancellationIsNeitherLoggedNorAnswered(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"), loc)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var logged bytes.Buffer
	srv := New(store, testConfig(), "test", slog.New(slog.NewTextHandler(&logged, nil)))

	cases := []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{"health handler", srv.handleHealth},
		{"a read that the handler reports through internalError", func(w http.ResponseWriter, r *http.Request) {
			srv.internalError(w, "reading the latest sample", fmt.Errorf("reading the latest sample: %w", context.Canceled))
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logged.Reset()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil).WithContext(ctx)

			tc.call(rec, req)

			if logged.Len() != 0 {
				t.Fatalf("a cancelled request must not be logged: %s", logged.String())
			}
			if rec.Body.Len() != 0 || rec.Code != http.StatusOK {
				t.Fatalf("a cancelled request must not be answered: code=%d body=%q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestIsCanceledMatchesWrappedCancellation(t *testing.T) {
	if !isCanceled(fmt.Errorf("reading: %w", context.Canceled)) {
		t.Fatal("a wrapped context.Canceled must count as a client abort")
	}
	if isCanceled(context.DeadlineExceeded) {
		t.Fatal("only a client abort is silent; a deadline stays an error")
	}
}
