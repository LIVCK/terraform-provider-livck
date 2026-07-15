package client

import (
	"context"
	"net/http"
	"net/url"
)

type Tag struct {
	ID    string  `json:"id"`
	Key   string  `json:"key"`
	Value *string `json:"value"`
	Color string  `json:"color"`
	Label string  `json:"label"`
}

// TagInput: value is sent WITHOUT omitempty so an explicit null turns a
// key:value tag back into a bare label on update; color omitted lets the
// server derive a deterministic one.
type TagInput struct {
	Key   string  `json:"key,omitempty"`
	Value *string `json:"value"`
	Color *string `json:"color,omitempty"`
}

func (c *Client) CreateTag(ctx context.Context, in TagInput) (*Tag, error) {
	var env dataEnvelope[Tag]
	if err := c.do(ctx, http.MethodPost, "/tags", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetTag(ctx context.Context, id string) (*Tag, error) {
	var env dataEnvelope[Tag]
	if err := c.do(ctx, http.MethodGet, "/tags/"+url.PathEscape(id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateTag(ctx context.Context, id string, in TagInput) (*Tag, error) {
	var env dataEnvelope[Tag]
	if err := c.do(ctx, http.MethodPatch, "/tags/"+url.PathEscape(id), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) DeleteTag(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/tags/"+url.PathEscape(id), nil, nil)
}
