package fronius

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"fronius-supervisor/internal/fronius/froniuserr"
)

// spyDoer compte les appels transmis.
type spyDoer struct {
	calls int
	last  *http.Request
}

func (s *spyDoer) Do(req *http.Request) (*http.Response, error) {
	s.calls++
	s.last = req
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusOK)
	return rec.Result(), nil
}

func newRequest(t *testing.T, method string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, "http://127.0.0.1:8081/solar_api/v1/GetPowerFlowRealtimeData.fcgi", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

func TestReadOnlyAllowsGet(t *testing.T) {
	spy := &spyDoer{}
	ro := ReadOnly{Inner: spy}

	resp, err := ro.Do(newRequest(t, http.MethodGet))
	if err != nil {
		t.Fatalf("Do(GET): %v", err)
	}
	defer resp.Body.Close()
	if spy.calls != 1 {
		t.Fatalf("appels transmis = %d, attendu 1", spy.calls)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestReadOnlyRefusesMutatingMethods(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			spy := &spyDoer{}
			ro := ReadOnly{Inner: spy}

			resp, err := ro.Do(newRequest(t, method))
			if err == nil {
				resp.Body.Close()
				t.Fatalf("%s devait être refusé", method)
			}
			if resp != nil {
				t.Fatalf("%s : une réponse non nulle a été retournée", method)
			}
			if spy.calls != 0 {
				t.Fatalf("%s : la requête a atteint le transport (%d appels)", method, spy.calls)
			}
			kind, ok := froniuserr.KindOf(err)
			if !ok || kind != froniuserr.KindReadOnly {
				t.Fatalf("%s : KindOf = (%q, %v), attendu %q", method, kind, ok, froniuserr.KindReadOnly)
			}
			var typed *froniuserr.Error
			if !errors.As(err, &typed) {
				t.Fatal("errors.As n'a pas retrouvé *froniuserr.Error")
			}
			if typed.Endpoint == "" {
				t.Fatal("l'endpoint refusé doit être renseigné")
			}
		})
	}
}

func TestReadOnlyNilInner(t *testing.T) {
	ro := ReadOnly{}
	_, err := ro.Do(newRequest(t, http.MethodGet))
	if err == nil {
		t.Fatal("un transport nul devait échouer proprement")
	}
	kind, _ := froniuserr.KindOf(err)
	if kind != froniuserr.KindTransport {
		t.Fatalf("KindOf = %q, attendu %q", kind, froniuserr.KindTransport)
	}
}

func TestReadOnlyNilRequest(t *testing.T) {
	ro := ReadOnly{Inner: &spyDoer{}}
	if _, err := ro.Do(nil); err == nil {
		t.Fatal("une requête nulle devait être refusée")
	}
}

// TestReadOnlyPropagatesTransportError vérifie qu'aucune erreur n'est avalée.
func TestReadOnlyPropagatesTransportError(t *testing.T) {
	want := errors.New("connexion refusée")
	ro := ReadOnly{Inner: failingDoer{err: want}}

	_, err := ro.Do(newRequest(t, http.MethodGet))
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, attendu %v", err, want)
	}
}

type failingDoer struct{ err error }

func (f failingDoer) Do(*http.Request) (*http.Response, error) { return nil, f.err }
