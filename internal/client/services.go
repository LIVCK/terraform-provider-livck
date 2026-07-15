package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Service mirrors the ServiceResource echo of the API (fields the provider manages).
type Service struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	CheckType string           `json:"check_type"`
	Target    *string          `json:"target"`
	Status    string           `json:"status"`
	IsPaused  bool             `json:"is_paused"`
	Settings  *ServiceSettings `json:"settings"`
	Tags      []Tag            `json:"tags"`
}

type ServiceSettings struct {
	IntervalSeconds int64           `json:"interval_seconds"`
	TimeoutSeconds  int64           `json:"timeout_seconds"`
	Retries         int64           `json:"retries"`
	AssignedProbes  []string        `json:"assigned_probes"`
	Config          json.RawMessage `json:"config"`
}

// ServiceSettingsInput is the write shape (settings block on store/update).
type ServiceSettingsInput struct {
	IntervalSeconds *int64          `json:"interval_seconds,omitempty"`
	TimeoutSeconds  *int64          `json:"timeout_seconds,omitempty"`
	Retries         *int64          `json:"retries,omitempty"`
	AssignedProbes  *[]string       `json:"assigned_probes,omitempty"`
	Config          json.RawMessage `json:"config,omitempty"`
}

// ServiceInput.Tags is a *pointer* to a slice: nil means "don't touch the
// assignment" (unmanaged), a non-nil empty slice sends tags: [] and clears it.
type ServiceInput struct {
	Name      string                `json:"name,omitempty"`
	CheckType string                `json:"check_type,omitempty"`
	Target    *string               `json:"target,omitempty"`
	Settings  *ServiceSettingsInput `json:"settings,omitempty"`
	Tags      *[]string             `json:"tags,omitempty"`
}

func (c *Client) CreateService(ctx context.Context, in ServiceInput) (*Service, error) {
	var env dataEnvelope[Service]
	if err := c.do(ctx, http.MethodPost, "/services", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetService(ctx context.Context, id string) (*Service, error) {
	var env dataEnvelope[Service]
	if err := c.do(ctx, http.MethodGet, "/services/"+url.PathEscape(id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateService(ctx context.Context, id string, in ServiceInput) (*Service, error) {
	var env dataEnvelope[Service]
	if err := c.do(ctx, http.MethodPatch, "/services/"+url.PathEscape(id), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) DeleteService(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/services/"+url.PathEscape(id), nil, nil)
}

func (c *Client) PauseService(ctx context.Context, id string) (*Service, error) {
	var env dataEnvelope[Service]
	if err := c.do(ctx, http.MethodPost, "/services/"+url.PathEscape(id)+"/pause", struct{}{}, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) ResumeService(ctx context.Context, id string) (*Service, error) {
	var env dataEnvelope[Service]
	if err := c.do(ctx, http.MethodPost, "/services/"+url.PathEscape(id)+"/resume", struct{}{}, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}
