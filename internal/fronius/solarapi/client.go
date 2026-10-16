package solarapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/froniuserr"
)

// PowerFlowPath est le chemin de l'unique endpoint utilisé en phase 1.
const PowerFlowPath = "/solar_api/v1/GetPowerFlowRealtimeData.fcgi"

// maxBodyBytes borne la lecture : la réponse réelle fait quelques kilo-octets.
const maxBodyBytes = 1 << 20 // 1 MiB

// Client interroge la Solar API.
type Client struct {
	base    *url.URL
	doer    fronius.Doer
	timeout time.Duration
	log     *slog.Logger
}

// New construit un client.
//
// doer peut être nil : un transport HTTP par défaut est alors utilisé. En
// pratique l'appelant fournit fronius.ReadOnly pour garantir structurellement
// l'absence d'écriture. timeout borne chaque requête, y compris la lecture du
// corps ; une échéance plus courte déjà présente sur le contexte l'emporte.
func New(baseURL string, doer fronius.Doer, timeout time.Duration, log *slog.Logger) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("URL Solar API %q: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("URL Solar API %q : schéma http(s) attendu", baseURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("URL Solar API %q : hôte manquant", baseURL)
	}
	if doer == nil {
		doer = &http.Client{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Client{base: u, doer: doer, timeout: timeout, log: log}, nil
}

// GetPowerFlowRealtimeData interroge l'endpoint unique de la phase 1.
func (c *Client) GetPowerFlowRealtimeData(ctx context.Context) (PowerFlowEnvelope, error) {
	var out PowerFlowEnvelope

	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	endpoint := c.base.JoinPath(PowerFlowPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return out, &froniuserr.Error{Kind: froniuserr.KindTransport, Endpoint: PowerFlowPath, Err: err}
	}
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.doer.Do(req)
	if err != nil {
		return out, classifyTransportError(ctx, PowerFlowPath, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return out, classifyTransportError(ctx, PowerFlowPath, err)
	}

	c.log.Debug("solar api",
		"solar_api_path", PowerFlowPath,
		"status", resp.StatusCode,
		"bytes", len(body),
		"duration_ms", time.Since(start).Milliseconds())

	if resp.StatusCode != http.StatusOK {
		return out, statusError(PowerFlowPath, resp.StatusCode)
	}
	if len(body) > maxBodyBytes {
		return out, &froniuserr.Error{
			Kind:     froniuserr.KindDecode,
			Endpoint: PowerFlowPath,
			Err:      fmt.Errorf("corps de %d octets : dépasse la limite de %d MiB", len(body), maxBodyBytes>>20),
		}
	}

	var raw rawEnvelope
	if err := json.Unmarshal(body, &raw); err != nil {
		return out, &froniuserr.Error{
			Kind:       froniuserr.KindDecode,
			Endpoint:   PowerFlowPath,
			HTTPStatus: resp.StatusCode,
			Err:        err,
		}
	}
	if raw.Body == nil || raw.Body.Data == nil {
		return out, &froniuserr.Error{
			Kind:       froniuserr.KindDecode,
			Endpoint:   PowerFlowPath,
			HTTPStatus: resp.StatusCode,
			Err:        errors.New("Body.Data absent de la réponse"),
		}
	}

	out = PowerFlowEnvelope{Body: PowerFlowBody{Data: *raw.Body.Data}, Head: raw.Head}

	if raw.Head.Status.Code != 0 {
		return out, &froniuserr.Error{
			Kind:        froniuserr.KindStatus,
			Endpoint:    PowerFlowPath,
			HTTPStatus:  resp.StatusCode,
			Code:        raw.Head.Status.Code,
			Reason:      raw.Head.Status.Reason,
			UserMessage: raw.Head.Status.UserMessage,
		}
	}

	return out, nil
}

// statusError traduit les codes HTTP non 200.
//
// 429 est classé avec les erreurs serveur : l'appareil demande explicitement de
// ralentir, et le collecteur applique alors son backoff.
func statusError(endpoint string, status int) error {
	kind := froniuserr.KindServer
	switch status {
	case http.StatusNotFound:
		kind = froniuserr.KindNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = froniuserr.KindUnauthorized
	}
	return &froniuserr.Error{Kind: kind, Endpoint: endpoint, HTTPStatus: status}
}

// classifyTransportError distingue l'expiration du délai des autres pannes réseau.
func classifyTransportError(ctx context.Context, endpoint string, err error) error {
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
