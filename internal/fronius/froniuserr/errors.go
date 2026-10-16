// Package froniuserr définit les erreurs typées partagées par tous les clients
// Fronius.
//
// Objectif : permettre à l'appelant (collecteur, API REST) de réagir à la
// nature d'une panne sans inspecter de chaînes de caractères, et garantir
// qu'aucune erreur ne peut masquer un secret : aucun champ ne contient de
// valeur d'authentification.
package froniuserr

import (
	"errors"
	"fmt"
	"strings"
)

// Kind classe la nature d'une erreur.
type Kind string

const (
	// KindNotFound : l'endpoint n'existe pas sur ce firmware (HTTP 404).
	KindNotFound Kind = "not_found"
	// KindUnauthorized : authentification requise ou refusée (HTTP 401/403).
	KindUnauthorized Kind = "unauthorized"
	// KindServer : erreur interne de l'appareil (HTTP 5xx).
	KindServer Kind = "server"
	// KindDecode : corps illisible, tronqué ou de forme inattendue.
	KindDecode Kind = "decode"
	// KindTimeout : délai d'attente dépassé.
	KindTimeout Kind = "timeout"
	// KindTransport : panne réseau (connexion refusée, DNS, TLS…).
	KindTransport Kind = "transport"
	// KindStatus : l'enveloppe est bien formée mais l'appareil signale une
	// erreur applicative (Head.Status.Code != 0).
	KindStatus Kind = "device_status"
	// KindReadOnly : une requête non-GET a tenté d'atteindre l'appareil.
	KindReadOnly Kind = "readonly_violation"
)

// Error est l'erreur typée des clients Fronius.
type Error struct {
	Kind        Kind
	Endpoint    string
	HTTPStatus  int
	Code        int    // Head.Status.Code du firmware
	Reason      string // Head.Status.Reason
	UserMessage string // Head.Status.UserMessage
	Err         error  // cause sous-jacente, éventuellement nil
}

func (e *Error) Error() string {
	parts := []string{string(e.Kind)}
	if e.Endpoint != "" {
		parts = append(parts, e.Endpoint)
	}
	if e.HTTPStatus != 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", e.HTTPStatus))
	}
	if e.Code != 0 {
		parts = append(parts, fmt.Sprintf("code %d", e.Code))
	}
	if e.Reason != "" {
		parts = append(parts, "reason: "+e.Reason)
	}
	if e.UserMessage != "" && e.UserMessage != e.Reason {
		parts = append(parts, "message: "+e.UserMessage)
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	return "fronius: " + strings.Join(parts, " | ")
}

// Unwrap expose la cause sous-jacente (errors.Is/As).
func (e *Error) Unwrap() error { return e.Err }

// Is permet à errors.Is de comparer deux erreurs par nature : une KindNotFound
// peut ainsi être recherchée sans connaître le corps de l'erreur.
func (e *Error) Is(target error) bool {
	var other *Error
	if !errors.As(target, &other) {
		return false
	}
	return e.Kind == other.Kind
}

// KindOf retourne la nature d'une erreur, même enveloppée.
func KindOf(err error) (Kind, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind, true
	}
	return "", false
}
