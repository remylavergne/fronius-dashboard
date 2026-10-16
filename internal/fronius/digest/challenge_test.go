package digest

import (
	"errors"
	"strings"
	"testing"
)

func TestParseChallengeVariants(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		want      Challenge
		wantErr   error
		errSubstr string
	}{
		{
			name:   "double quotes",
			header: `Digest realm="Webinterface area", nonce="sim-nonce-1", qop="auth"`,
			want:   Challenge{Realm: "Webinterface area", Nonce: "sim-nonce-1", Qop: "auth"},
		},
		{
			name:   "single quotes",
			header: `Digest realm='Webinterface area', nonce='abc'`,
			want:   Challenge{Realm: "Webinterface area", Nonce: "abc"},
		},
		{
			name:   "bare values",
			header: `Digest realm=no-quotes, nonce=abc`,
			want:   Challenge{Realm: "no-quotes", Nonce: "abc"},
		},
		{
			name:   "scheme absent",
			header: `realm="Webinterface area", nonce="abc"`,
			want:   Challenge{Realm: "Webinterface area", Nonce: "abc"},
		},
		{
			name:   "qop list: first token kept",
			header: `Digest realm="r", nonce="n", qop="auth,auth-int"`,
			want:   Challenge{Realm: "r", Nonce: "n", Qop: "auth"},
		},
		{
			name:   "opaque, stale and algorithm=MD5",
			header: `Digest realm="r", nonce="n", opaque="op", stale=true, algorithm=MD5`,
			want:   Challenge{Realm: "r", Nonce: "n", Opaque: "op", Algorithm: "MD5", Stale: true},
		},
		{
			name:   "stale case-insensitive",
			header: `Digest realm="r", nonce="n", stale=TRUE`,
			want:   Challenge{Realm: "r", Nonce: "n", Stale: true},
		},
		{
			name:      "algorithm SHA-256 refused and named",
			header:    `Digest realm="r", nonce="n", algorithm=SHA-256`,
			wantErr:   ErrUnsupportedAlgorithm,
			errSubstr: "SHA-256",
		},
		{
			name:    "missing realm",
			header:  `Digest nonce="n"`,
			wantErr: ErrIncompleteChallenge,
		},
		{
			name:    "missing nonce",
			header:  `Digest realm="r"`,
			wantErr: ErrIncompleteChallenge,
		},
		{
			name:    "empty realm",
			header:  `Digest realm="", nonce="n"`,
			wantErr: ErrIncompleteChallenge,
		},
		{
			name:    "empty header",
			header:  "",
			wantErr: ErrNoChallenge,
		},
		{
			name:    "header without a key=value pair",
			header:  "Digest",
			wantErr: ErrNoChallenge,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseChallenge(tc.header)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error %q does not name %q", err, tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseChallenge: %v", err)
			}
			if got != tc.want {
				t.Fatalf("challenge = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestParseChallengeQuoteInsideValueTruncates: the bundle pattern stops at the
// first quote met inside the value — `realm="a'b"` yields "a", not "ab".
// Behaviour reproduced identically: a real device's realm contains none, and
// diverging from it would be inventing tolerance.
func TestParseChallengeQuoteInsideValueTruncates(t *testing.T) {
	got, err := ParseChallenge(`Digest realm="a'b", nonce="c'd"`)
	if err != nil {
		t.Fatalf("ParseChallenge: %v", err)
	}
	if got.Realm != "a" || got.Nonce != "c" {
		t.Fatalf("challenge = %+v, want realm=a nonce=c", got)
	}
}
