package client

import (
	"context"
	"net/http"
	"net/url"
)

// Statuspage mirrors the StatuspageResource echo.
type Statuspage struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Slug        string      `json:"slug"`
	IsPublished bool        `json:"is_published"`
	AccessType  string      `json:"access_type"`
	Components  []Component `json:"components"`
}

type StatuspageInput struct {
	Name string  `json:"name,omitempty"`
	Slug *string `json:"slug,omitempty"`
}

// Component mirrors the StatuspageComponentResource echo.
type Component struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	Status       string  `json:"status"`
	IsGroup      bool    `json:"is_group"`
	IsVisible    bool    `json:"is_visible"`
	DisplayOrder int64   `json:"display_order"`
	ParentID     *string `json:"parent_id"`
	Service      *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"service"`
}

// ComponentInput: description/service_id/parent_id are sent WITHOUT omitempty
// on purpose — clearing a link (re-parenting to root, unlinking a service)
// requires an explicit null; an omitted key means "keep" server-side.
type ComponentInput struct {
	Name         string  `json:"name,omitempty"`
	Description  *string `json:"description"`
	ServiceID    *string `json:"service_id"`
	ParentID     *string `json:"parent_id"`
	IsGroup      *bool   `json:"is_group,omitempty"`
	IsVisible    *bool   `json:"is_visible,omitempty"`
	DisplayOrder *int64  `json:"display_order,omitempty"`
}

func (c *Client) CreateStatuspage(ctx context.Context, in StatuspageInput) (*Statuspage, error) {
	var env dataEnvelope[Statuspage]
	if err := c.do(ctx, http.MethodPost, "/statuspages", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetStatuspage(ctx context.Context, id string) (*Statuspage, error) {
	var env dataEnvelope[Statuspage]
	if err := c.do(ctx, http.MethodGet, "/statuspages/"+url.PathEscape(id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateStatuspage(ctx context.Context, id string, in StatuspageInput) (*Statuspage, error) {
	var env dataEnvelope[Statuspage]
	if err := c.do(ctx, http.MethodPatch, "/statuspages/"+url.PathEscape(id), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) DeleteStatuspage(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/statuspages/"+url.PathEscape(id), nil, nil)
}

func (c *Client) PublishStatuspage(ctx context.Context, id string, publish bool) (*Statuspage, error) {
	action := "/publish"
	if !publish {
		action = "/unpublish"
	}
	var env dataEnvelope[Statuspage]
	if err := c.do(ctx, http.MethodPost, "/statuspages/"+url.PathEscape(id)+action, struct{}{}, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) CreateComponent(ctx context.Context, statuspageID string, in ComponentInput) (*Component, error) {
	var env dataEnvelope[Component]
	if err := c.do(ctx, http.MethodPost, "/statuspages/"+url.PathEscape(statuspageID)+"/components", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetComponent(ctx context.Context, statuspageID, id string) (*Component, error) {
	var env dataEnvelope[Component]
	if err := c.do(ctx, http.MethodGet, "/statuspages/"+url.PathEscape(statuspageID)+"/components/"+url.PathEscape(id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateComponent(ctx context.Context, statuspageID, id string, in ComponentInput) (*Component, error) {
	var env dataEnvelope[Component]
	if err := c.do(ctx, http.MethodPatch, "/statuspages/"+url.PathEscape(statuspageID)+"/components/"+url.PathEscape(id), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) DeleteComponent(ctx context.Context, statuspageID, id string) error {
	return c.do(ctx, http.MethodDelete, "/statuspages/"+url.PathEscape(statuspageID)+"/components/"+url.PathEscape(id), nil, nil)
}
