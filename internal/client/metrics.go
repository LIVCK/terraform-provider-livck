package client

import (
	"context"
	"net/http"
	"net/url"
)

// Metric mirrors the StatuspageMetricResource echo.
type Metric struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	DisplayType  string   `json:"display_type"`
	Suffix       string   `json:"suffix"`
	ComponentID  *string  `json:"component_id"`
	IsVisible    bool     `json:"is_visible"`
	DisplayOrder int64    `json:"display_order"`
	Series       []Series `json:"series"`
}

// MetricInput: component_id is sent WITHOUT omitempty on purpose — detaching
// a chart from its component requires an explicit null.
type MetricInput struct {
	Name        string  `json:"name,omitempty"`
	DisplayType string  `json:"display_type,omitempty"`
	Suffix      *string `json:"suffix,omitempty"`
	ComponentID *string `json:"component_id"`
	IsVisible   *bool   `json:"is_visible,omitempty"`
}

// Series mirrors the StatuspageSeriesResource echo.
type Series struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	MetricType   string  `json:"metric_type"`
	Color        *string `json:"color"`
	DisplayOrder int64   `json:"display_order"`
	Service      *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"service"`
}

// SeriesInput: color without omitempty — an explicit null resets it.
type SeriesInput struct {
	Name       string  `json:"name,omitempty"`
	ServiceID  string  `json:"service_id,omitempty"`
	MetricType string  `json:"metric_type,omitempty"`
	Color      *string `json:"color"`
}

func (c *Client) CreateMetric(ctx context.Context, statuspageID string, in MetricInput) (*Metric, error) {
	var env dataEnvelope[Metric]
	if err := c.do(ctx, http.MethodPost, "/statuspages/"+url.PathEscape(statuspageID)+"/metrics", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetMetric(ctx context.Context, statuspageID, id string) (*Metric, error) {
	var env dataEnvelope[Metric]
	if err := c.do(ctx, http.MethodGet, "/statuspages/"+url.PathEscape(statuspageID)+"/metrics/"+url.PathEscape(id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateMetric(ctx context.Context, statuspageID, id string, in MetricInput) (*Metric, error) {
	var env dataEnvelope[Metric]
	if err := c.do(ctx, http.MethodPatch, "/statuspages/"+url.PathEscape(statuspageID)+"/metrics/"+url.PathEscape(id), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) DeleteMetric(ctx context.Context, statuspageID, id string) error {
	return c.do(ctx, http.MethodDelete, "/statuspages/"+url.PathEscape(statuspageID)+"/metrics/"+url.PathEscape(id), nil, nil)
}

func (c *Client) CreateSeries(ctx context.Context, statuspageID, metricID string, in SeriesInput) (*Series, error) {
	var env dataEnvelope[Series]
	if err := c.do(ctx, http.MethodPost, seriesBase(statuspageID, metricID), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetSeries(ctx context.Context, statuspageID, metricID, id string) (*Series, error) {
	var env dataEnvelope[Series]
	if err := c.do(ctx, http.MethodGet, seriesBase(statuspageID, metricID)+"/"+url.PathEscape(id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateSeries(ctx context.Context, statuspageID, metricID, id string, in SeriesInput) (*Series, error) {
	var env dataEnvelope[Series]
	if err := c.do(ctx, http.MethodPatch, seriesBase(statuspageID, metricID)+"/"+url.PathEscape(id), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) DeleteSeries(ctx context.Context, statuspageID, metricID, id string) error {
	return c.do(ctx, http.MethodDelete, seriesBase(statuspageID, metricID)+"/"+url.PathEscape(id), nil, nil)
}

func seriesBase(statuspageID, metricID string) string {
	return "/statuspages/" + url.PathEscape(statuspageID) + "/metrics/" + url.PathEscape(metricID) + "/series"
}
