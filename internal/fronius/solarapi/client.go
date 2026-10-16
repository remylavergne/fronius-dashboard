package solarapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/froniuserr"
)

// PowerFlowPath is the path of the single endpoint used in phase 1.
const PowerFlowPath = "/solar_api/v1/GetPowerFlowRealtimeData.fcgi"

// maxBodyBytes bounds the read: the real response is a few kilobytes.
const maxBodyBytes = 1 << 20 // 1 MiB

// Client queries the Solar API.
type Client struct {
	base    *url.URL
	doer    fronius.Doer
	timeout time.Duration
	log     *slog.Logger
}

// New builds a client.
//
// doer may be nil: a default HTTP transport is then used. In practice the caller
// provides fronius.ReadOnly to structurally guarantee the absence of writes.
// timeout bounds each request, including the body read; a shorter deadline
// already present on the context takes precedence.
func New(baseURL string, doer fronius.Doer, timeout time.Duration, log *slog.Logger) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("Solar API URL %q: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("Solar API URL %q: http(s) scheme expected", baseURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("Solar API URL %q: missing host", baseURL)
	}
	if doer == nil {
		doer = &http.Client{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Client{base: u, doer: doer, timeout: timeout, log: log}, nil
}

// GetPowerFlowRealtimeData queries the single endpoint of phase 1.
func (c *Client) GetPowerFlowRealtimeData(ctx context.Context) (PowerFlowEnvelope, error) {
	return c.getPowerFlow(ctx, nil)
}

// GetPowerFlowRealtimeDataAt queries the endpoint asking for the curve at a past
// instant, via the "at" parameter.
//
// This variant is a SIMULATOR EXTENSION (cmd/fronius-sim): the "at" parameter
// does not belong to the Solar API and a real inverter will ignore it. It only
// serves development history backfill, never current acquisition.
func (c *Client) GetPowerFlowRealtimeDataAt(ctx context.Context, at time.Time) (PowerFlowEnvelope, error) {
	return c.getPowerFlow(ctx, &at)
}

func (c *Client) getPowerFlow(ctx context.Context, at *time.Time) (PowerFlowEnvelope, error) {
	var out PowerFlowEnvelope

	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	endpoint := c.base.JoinPath(PowerFlowPath)
	if at != nil {
		q := url.Values{}
		q.Set("at", at.Format(time.RFC3339))
		endpoint.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return out, &froniuserr.Error{Kind: froniuserr.KindTransport, Endpoint: PowerFlowPath, Err: err}
	}
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.doer.Do(req)
	if err != nil {
		return out, fronius.ClassifyTransport(ctx, PowerFlowPath, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return out, fronius.ClassifyTransport(ctx, PowerFlowPath, err)
	}

	c.log.Debug("solar api",
		"solar_api_path", PowerFlowPath,
		"status", resp.StatusCode,
		"bytes", len(body),
		"duration_ms", time.Since(start).Milliseconds())

	if resp.StatusCode != http.StatusOK {
		return out, fronius.ClassifyStatus(PowerFlowPath, resp.StatusCode)
	}
	if len(body) > maxBodyBytes {
		return out, &froniuserr.Error{
			Kind:     froniuserr.KindDecode,
			Endpoint: PowerFlowPath,
			Err:      fmt.Errorf("body of %d bytes: exceeds the limit of %d MiB", len(body), maxBodyBytes>>20),
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
			Err:        errors.New("Body.Data absent from the response"),
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
