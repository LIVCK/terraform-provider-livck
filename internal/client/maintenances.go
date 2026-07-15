package client

import (
	"context"
	"net/http"
	"net/url"
)

// Maintenance mirrors the MaintenanceResource echo.
type Maintenance struct {
	ID                string            `json:"id"`
	Title             string            `json:"title"`
	TitleTranslations map[string]string `json:"title_translations"`
	Type              string            `json:"type"`
	Status            string            `json:"status"`
	ScheduledStart    string            `json:"scheduled_start"`
	ScheduledEnd      string            `json:"scheduled_end"`
	AutoStart         bool              `json:"auto_start"`
	AutoComplete      bool              `json:"auto_complete"`
	Services          []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"services"`
	Statuspages []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"statuspages"`
}

type MaintenanceInput struct {
	Title          any       `json:"title,omitempty"`
	Type           *string   `json:"type,omitempty"`
	ScheduledStart string    `json:"scheduled_start,omitempty"`
	ScheduledEnd   string    `json:"scheduled_end,omitempty"`
	ServiceIDs     *[]string `json:"service_ids,omitempty"`
	StatuspageIDs  *[]string `json:"statuspage_ids,omitempty"`
	AutoStart      *bool     `json:"auto_start,omitempty"`
	AutoComplete   *bool     `json:"auto_complete,omitempty"`
	Notify24h      *bool     `json:"notify_24h,omitempty"`
	Notify1h       *bool     `json:"notify_1h,omitempty"`
	NotifyStart    *bool     `json:"notify_start,omitempty"`
	NotifyComplete *bool     `json:"notify_complete,omitempty"`
}

func (c *Client) CreateMaintenance(ctx context.Context, in MaintenanceInput) (*Maintenance, error) {
	var env dataEnvelope[Maintenance]
	if err := c.do(ctx, http.MethodPost, "/maintenances", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetMaintenance(ctx context.Context, id string) (*Maintenance, error) {
	var env dataEnvelope[Maintenance]
	if err := c.do(ctx, http.MethodGet, "/maintenances/"+url.PathEscape(id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateMaintenance(ctx context.Context, id string, in MaintenanceInput) (*Maintenance, error) {
	var env dataEnvelope[Maintenance]
	if err := c.do(ctx, http.MethodPatch, "/maintenances/"+url.PathEscape(id), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) DeleteMaintenance(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/maintenances/"+url.PathEscape(id), nil, nil)
}
