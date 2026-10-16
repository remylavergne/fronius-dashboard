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
			name:   "guillemets doubles",
			header: `Digest realm="Webinterface area", nonce="sim-nonce-1", qop="auth"`,
			want:   Challenge{Realm: "Webinterface area", Nonce: "sim-nonce-1", Qop: "auth"},
		},
		{
			name:   "guillemets simples",
			header: `Digest realm='Webinterface area', nonce='abc'`,
			want:   Challenge{Realm: "Webinterface area", Nonce: "abc"},
		},
		{
			name:   "valeurs nues",
			header: `Digest realm=sans-guillemets, nonce=abc`,
			want:   Challenge{Realm: "sans-guillemets", Nonce: "abc"},
		},
		{
			name:   "scheme absent",
			header: `realm="Webinterface area", nonce="abc"`,
			want:   Challenge{Realm: "Webinterface area", Nonce: "abc"},
		},
		{
			name:   "qop liste : premier jeton retenu",
			header: `Digest realm="r", nonce="n", qop="auth,auth-int"`,
			want:   Challenge{Realm: "r", Nonce: "n", Qop: "auth"},
		},
		{
			name:   "opaque, stale et algorithm=MD5",
			header: `Digest realm="r", nonce="n", opaque="op", stale=true, algorithm=MD5`,
			want:   Challenge{Realm: "r", Nonce: "n", Opaque: "op", Algorithm: "MD5", Stale: true},
		},
		{
			name:   "stale insensible à la casse",
			header: `Digest realm="r", nonce="n", stale=TRUE`,
			want:   Challenge{Realm: "r", Nonce: "n", Stale: true},
		},
		{
			name:      "algorithm SHA-256 refusé et nommé",
			header:    `Digest realm="r", nonce="n", algorithm=SHA-256`,
			wantErr:   ErrUnsupportedAlgorithm,
			errSubstr: "SHA-256",
		},
		{
			name:    "realm manquant",
			header:  `Digest nonce="n"`,
			wantErr: ErrIncompleteChallenge,
		},
		{
			name:    "nonce manquant",
			header:  `Digest realm="r"`,
			wantErr: ErrIncompleteChallenge,
		},
		{
			name:    "realm vide",
			header:  `Digest realm="", nonce="n"`,
			wantErr: ErrIncompleteChallenge,
		},
		{
			name:    "en-tête vide",
			header:  "",
			wantErr: ErrNoChallenge,
		},
		{
			name:    "en-tête sans paire clé=valeur",
			header:  "Digest",
			wantErr: ErrNoChallenge,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseChallenge(tc.header)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("erreur = %v, attendu %v", err, tc.wantErr)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("erreur %q ne nomme pas %q", err, tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseChallenge: %v", err)
			}
			if got != tc.want {
				t.Fatalf("défi = %+v, attendu %+v", got, tc.want)
			}
		})
	}
}

// TestParseChallengeQuoteInsideValueTruncates : le motif du bundle s'arrête au
// premier guillemet rencontré dans la valeur — `realm="a'b"` donne « a », pas
// « ab ». Comportement reproduit à l'identique : le realm d'un appareil réel
// n'en contient pas, et s'en écarter serait inventer une tolérance.
func TestParseChallengeQuoteInsideValueTruncates(t *testing.T) {
	got, err := ParseChallenge(`Digest realm="a'b", nonce="c'd"`)
	if err != nil {
		t.Fatalf("ParseChallenge: %v", err)
	}
	if got.Realm != "a" || got.Nonce != "c" {
		t.Fatalf("défi = %+v, attendu realm=a nonce=c", got)
	}
}
