package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// Probe is a monitoring location (external identifier: code).
type Probe struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Location    string `json:"location"`
	CountryCode string `json:"country_code"`
}

func (c *Client) ListProbes(ctx context.Context) ([]Probe, error) {
	var env dataEnvelope[[]Probe]
	if err := c.do(ctx, http.MethodGet, "/probes", nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// CheckTypeCatalog returns the raw per-check-type field/condition catalog —
// the schema is deep and check-type-specific, so it is exposed as JSON.
func (c *Client) CheckTypeCatalog(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/meta/check-types", nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
