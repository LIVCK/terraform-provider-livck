package client

import (
	"context"
	"net/http"
	"net/url"
)

// Statuspage mirrors the StatuspageResource echo.
type Statuspage struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	NameTranslations map[string]string `json:"name_translations"`
	Slug             string            `json:"slug"`
	IsPublished      bool              `json:"is_published"`
	AccessType       string            `json:"access_type"`
	// Appearance
	PrimaryColor         *string `json:"primary_color"`
	SecondaryColor       *string `json:"secondary_color"`
	CustomCSS            *string `json:"custom_css"`
	ImprintURL           *string `json:"imprint_url"`
	PrivacyPolicyURL     *string `json:"privacy_policy_url"`
	ShowLogo             bool    `json:"show_logo"`
	ShowAffectedServices bool    `json:"show_affected_services"`
	ShowIncidentHistory  bool    `json:"show_incident_history"`
	// Served asset URLs (managed via the dedicated asset endpoints)
	LogoURL     *string `json:"logo_url"`
	LogoDarkURL *string `json:"logo_dark_url"`
	FaviconURL  *string `json:"favicon_url"`
	// Access
	HasPassword        bool        `json:"has_password"`
	EmailWhitelist     []string    `json:"email_whitelist"`
	SubscriberChannels []string    `json:"subscriber_channels"`
	Components         []Component `json:"components"`
}

// StatuspageInput: translatable Name is `any` (plain string or {locale: value}
// map). Nullable appearance fields keep omitempty - the provider only sends the
// keys the practitioner actually set, and passes an explicit null (via a pointer
// to nil is not possible with omitempty, so clearing is done by sending the zero
// where meaningful). Password is write-only (never echoed; see HasPassword).
// Every field is omitempty: the provider only sends the appearance/access keys
// the practitioner actually manages (a null Optional attribute stops managing
// that field, mirroring the tag/translation "null = unmanaged" idiom), so an
// unmanaged field is never clobbered. Password is write-only (never echoed).
type StatuspageInput struct {
	Name                 any       `json:"name,omitempty"`
	Slug                 *string   `json:"slug,omitempty"`
	PrimaryColor         *string   `json:"primary_color,omitempty"`
	SecondaryColor       *string   `json:"secondary_color,omitempty"`
	CustomCSS            *string   `json:"custom_css,omitempty"`
	ImprintURL           *string   `json:"imprint_url,omitempty"`
	PrivacyPolicyURL     *string   `json:"privacy_policy_url,omitempty"`
	ShowLogo             *bool     `json:"show_logo,omitempty"`
	ShowAffectedServices *bool     `json:"show_affected_services,omitempty"`
	ShowIncidentHistory  *bool     `json:"show_incident_history,omitempty"`
	AccessType           *string   `json:"access_type,omitempty"`
	Password             *string   `json:"password,omitempty"`
	EmailWhitelist       *[]string `json:"email_whitelist,omitempty"`
	SubscriberChannels   *[]string `json:"subscriber_channels,omitempty"`
}

// UploadStatuspageAsset POSTs a logo/logo-dark/favicon file (multipart). asset
// is the URL slug (`logo`, `logo-dark`, `favicon`).
func (c *Client) UploadStatuspageAsset(ctx context.Context, statuspageID, asset, filename string, content []byte) (*Statuspage, error) {
	var env dataEnvelope[Statuspage]
	path := "/statuspages/" + url.PathEscape(statuspageID) + "/assets/" + url.PathEscape(asset)
	if err := c.doMultipart(ctx, path, "file", filename, content, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// DeleteStatuspageAsset clears a logo/logo-dark/favicon collection.
func (c *Client) DeleteStatuspageAsset(ctx context.Context, statuspageID, asset string) (*Statuspage, error) {
	var env dataEnvelope[Statuspage]
	path := "/statuspages/" + url.PathEscape(statuspageID) + "/assets/" + url.PathEscape(asset)
	if err := c.do(ctx, http.MethodDelete, path, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// Component mirrors the StatuspageComponentResource echo.
type Component struct {
	ID                      string            `json:"id"`
	Name                    string            `json:"name"`
	NameTranslations        map[string]string `json:"name_translations"`
	Description             *string           `json:"description"`
	DescriptionTranslations map[string]string `json:"description_translations"`
	Status                  string            `json:"status"`
	IsGroup                 bool              `json:"is_group"`
	IsVisible               bool              `json:"is_visible"`
	DisplayOrder            int64             `json:"display_order"`
	ParentID                *string           `json:"parent_id"`
	SyncTagID               *string           `json:"sync_tag_id"`
	SyncNewVisible          bool              `json:"sync_new_visible"`
	IsSyncManaged           bool              `json:"is_sync_managed"`
	ShowUptimeBars          bool              `json:"show_uptime_bars"`
	HideOperationalChildren bool              `json:"hide_operational_children"`
	Service                 *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"service"`
}

// ComponentInput: description/service_id/parent_id are sent WITHOUT omitempty
// on purpose - clearing a link (re-parenting to root, unlinking a service)
// requires an explicit null; an omitted key means "keep" server-side.
type ComponentInput struct {
	Name                    any     `json:"name,omitempty"`
	Description             any     `json:"description"`
	ServiceID               *string `json:"service_id"`
	ParentID                *string `json:"parent_id"`
	IsGroup                 *bool   `json:"is_group,omitempty"`
	IsVisible               *bool   `json:"is_visible,omitempty"`
	DisplayOrder            *int64  `json:"display_order,omitempty"`
	SyncTagID               *string `json:"sync_tag_id"`
	SyncNewVisible          *bool   `json:"sync_new_visible,omitempty"`
	ShowUptimeBars          *bool   `json:"show_uptime_bars,omitempty"`
	HideOperationalChildren *bool   `json:"hide_operational_children,omitempty"`
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
