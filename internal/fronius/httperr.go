// Classification des erreurs HTTP/transport partagée par tous les clients
// Fronius (Solar API et chemins internes).
//
// Un seul endroit décide à quelle nature d'erreur correspond un code HTTP : les
// deux clients classent donc identiquement, et le collecteur comme le détecteur
// peuvent réagir par froniuserr.KindOf sans inspecter de chaînes.
package fronius

import (
	"context"
	"errors"
	"net"
	"net/http"

	"fronius-supervisor/internal/fronius/froniuserr"
)

// ClassifyStatus traduit les codes HTTP non 200.
//
// 429 est classé avec les erreurs serveur : l'appareil demande explicitement de
// ralentir, et le collecteur applique alors son backoff.
func ClassifyStatus(endpoint string, status int) error {
	kind := froniuserr.KindServer
	switch status {
	case http.StatusNotFound:
		kind = froniuserr.KindNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = froniuserr.KindUnauthorized
	}
	return &froniuserr.Error{Kind: kind, Endpoint: endpoint, HTTPStatus: status}
}

// ClassifyTransport distingue l'expiration du délai des autres pannes réseau.
func ClassifyTransport(ctx context.Context, endpoint string, err error) error {
	kind := froniuserr.KindTransport
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		kind = froniuserr.KindTimeout
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		kind = froniuserr.KindTimeout
	}
	return &froniuserr.Error{Kind: kind, Endpoint: endpoint, Err: err}
}
