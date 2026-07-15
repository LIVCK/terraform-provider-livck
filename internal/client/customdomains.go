package client

import (
	"context"
	"net/http"
	"net/url"
)

type CustomDomain struct {
	ID             string  `json:"id"`
	Hostname       string  `json:"hostname"`
	Status         string  `json:"status"`
	CnameTarget    string  `json:"cname_target"`
	TxtRecordName  string  `json:"txt_record_name"`
	TxtRecordValue string  `json:"txt_record_value"`
	Verified       bool    `json:"verified"`
	VerifiedAt     *string `json:"verified_at"`
	LastErrorCode  *string `json:"last_error_code"`
}

type CustomDomainInput struct {
	Hostname string `json:"hostname"`
}

func (c *Client) CreateCustomDomain(ctx context.Context, statuspageID string, in CustomDomainInput) (*CustomDomain, error) {
	var env dataEnvelope[CustomDomain]
	if err := c.do(ctx, http.MethodPost, "/statuspages/"+url.PathEscape(statuspageID)+"/custom-domains", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetCustomDomain(ctx context.Context, statuspageID, id string) (*CustomDomain, error) {
	var env dataEnvelope[CustomDomain]
	if err := c.do(ctx, http.MethodGet, "/statuspages/"+url.PathEscape(statuspageID)+"/custom-domains/"+url.PathEscape(id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) DeleteCustomDomain(ctx context.Context, statuspageID, id string) error {
	return c.do(ctx, http.MethodDelete, "/statuspages/"+url.PathEscape(statuspageID)+"/custom-domains/"+url.PathEscape(id), nil, nil)
}
