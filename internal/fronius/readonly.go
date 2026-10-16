// Package fronius contient les primitives partagées par les clients de
// l'onduleur.
//
// Invariant : l'application ne modifie jamais la configuration du matériel.
// Tous les clients sont construits derrière ReadOnly, qui refuse toute méthode
// autre que GET avant même d'ouvrir une connexion. L'ordre de composition est
// volontairement ReadOnly{Inner: digest.Transport{…}} : la garde est à
// l'extérieur, donc l'authentification ne peut pas transformer une requête
// interdite en requête acceptable.
package fronius

import (
	"fmt"
	"net/http"

	"fronius-supervisor/internal/fronius/froniuserr"
)

// Doer est le sous-ensemble de *http.Client utilisé par les clients Fronius.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// ReadOnly est un Doer qui n'autorise que les requêtes GET.
//
// Le refus est structurel : il ne dépend ni de la configuration, ni d'un flag,
// ni du bon vouloir de l'appelant. La seule opération distante possible est une
// lecture.
type ReadOnly struct {
	Inner Doer
}

// Do exécute req si et seulement si sa méthode est GET.
func (r ReadOnly) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, &froniuserr.Error{
			Kind: froniuserr.KindReadOnly,
			Err:  fmt.Errorf("requête nulle"),
		}
	}
	if req.Method != http.MethodGet {
		endpoint := ""
		if req.URL != nil {
			endpoint = req.URL.Path
		}
		return nil, &froniuserr.Error{
			Kind:     froniuserr.KindReadOnly,
			Endpoint: endpoint,
			Err:      fmt.Errorf("méthode %s interdite : seuls les GET sont autorisés vers l'onduleur", req.Method),
		}
	}
	if r.Inner == nil {
		return nil, &froniuserr.Error{
			Kind:     froniuserr.KindTransport,
			Endpoint: req.URL.Path,
			Err:      fmt.Errorf("aucun transport configuré (ReadOnly.Inner est nul)"),
		}
	}
	return r.Inner.Do(req)
}
