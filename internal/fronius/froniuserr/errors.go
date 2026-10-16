// Package froniuserr defines the typed errors shared by all Fronius clients.
//
// Goal: allow the caller (collector, REST API) to react to the nature of a
// failure without inspecting strings, and guarantee that no error can mask a
// secret: no field contains an authentication value.
package froniuserr

import (
	"errors"
	"fmt"
	"strings"
)

// Kind classifies the nature of an error.
type Kind string

const (
	// KindNotFound: the endpoint does not exist on this firmware (HTTP 404).
	KindNotFound Kind = "not_found"
	// KindUnauthorized: authentication required or refused (HTTP 401/403).
	KindUnauthorized Kind = "unauthorized"
	// KindServer: internal device error (HTTP 5xx).
	KindServer Kind = "server"
	// KindDecode: unreadable, truncated or unexpected-shape body.
	KindDecode Kind = "decode"
	// KindTimeout: deadline exceeded.
	KindTimeout Kind = "timeout"
	// KindTransport: network failure (connection refused, DNS, TLS…).
	KindTransport Kind = "transport"
	// KindStatus: the envelope is well formed but the device reports an
	// application error (Head.Status.Code != 0).
	KindStatus Kind = "device_status"
	// KindReadOnly: a non-GET request attempted to reach the device.
	KindReadOnly Kind = "readonly_violation"
)

// Error is the typed error of the Fronius clients.
type Error struct {
	Kind        Kind
	Endpoint    string
	HTTPStatus  int
	Code        int    // Head.Status.Code from the firmware
	Reason      string // Head.Status.Reason
	UserMessage string // Head.Status.UserMessage
	Err         error  // underlying cause, possibly nil
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

// Unwrap exposes the underlying cause (errors.Is/As).
func (e *Error) Unwrap() error { return e.Err }

// Is lets errors.Is compare two errors by nature: a KindNotFound can thus be
// searched for without knowing the body of the error.
func (e *Error) Is(target error) bool {
	var other *Error
	if !errors.As(target, &other) {
		return false
	}
	return e.Kind == other.Kind
}

// KindOf returns the nature of an error, even wrapped.
func KindOf(err error) (Kind, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind, true
	}
	return "", false
}
