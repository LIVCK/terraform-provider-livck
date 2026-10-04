package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

// statuspagePlan builds a model with every attribute null except the name,
// which is what a minimal config looks like in a plan.
func statuspagePlan() *statuspageModel {
	return &statuspageModel{
		ID:                    types.StringNull(),
		Name:                  types.StringValue("Acme Status"),
		NameTranslations:      types.MapNull(types.StringType),
		Slug:                  types.StringNull(),
		Published:             types.BoolNull(),
		PrimaryColor:          types.StringNull(),
		SecondaryColor:        types.StringNull(),
		CustomCSS:             types.StringNull(),
		ImprintURL:            types.StringNull(),
		PrivacyPolicyURL:      types.StringNull(),
		ShowLogo:              types.BoolNull(),
		LogoSize:              types.StringNull(),
		ShowLivi:              types.BoolNull(),
		ShowAffectedServices:  types.BoolNull(),
		ShowIncidentHistory:   types.BoolNull(),
		Appearance:            types.StringNull(),
		AllowAppearanceSwitch: types.BoolNull(),
		AccessType:            types.StringNull(),
		Password:              types.StringNull(),
		HasPassword:           types.BoolNull(),
		EmailWhitelist:        types.SetNull(types.StringType),
		SubscriberChannels:    types.SetNull(types.StringType),
		Logo:                  types.StringNull(),
		LogoDark:              types.StringNull(),
		Favicon:               types.StringNull(),
		LogoURL:               types.StringNull(),
		LogoDarkURL:           types.StringNull(),
		FaviconURL:            types.StringNull(),
		LogoHash:              types.StringNull(),
		LogoDarkHash:          types.StringNull(),
		FaviconHash:           types.StringNull(),
	}
}

// wireBody marshals the input exactly as the client sends it and decodes the
// result, so a test can assert which keys go over the wire.
func wireBody(t *testing.T, in client.StatuspageInput) map[string]any {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return body
}

// decodeStatuspage decodes a response body the way the client does.
func decodeStatuspage(t *testing.T, raw string) *client.Statuspage {
	t.Helper()
	var page client.Statuspage
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return &page
}

var displayKeys = []string{"appearance", "allow_appearance_switch", "logo_size"}

// An attribute the practitioner does not set must not reach the API: on update
// that would overwrite a value chosen in the console, on create it would
// replace the server default.
func TestBrandingInputLeavesUnsetFieldsOut(t *testing.T) {
	ctx := context.Background()

	t.Run("null attributes are not sent", func(t *testing.T) {
		var d diag.Diagnostics
		in, has := brandingInput(ctx, statuspagePlan(), &d)
		if d.HasError() {
			t.Fatalf("unexpected diags: %v", d)
		}
		if has {
			t.Fatal("nothing is set, so create must not send a follow-up update")
		}
		if body := wireBody(t, in); len(body) != 0 {
			t.Fatalf("expected an empty body, got %v", body)
		}
	})

	t.Run("unknown attributes are not sent", func(t *testing.T) {
		// On create, an unconfigured Optional+Computed attribute plans as unknown.
		plan := statuspagePlan()
		plan.Appearance = types.StringUnknown()
		plan.AllowAppearanceSwitch = types.BoolUnknown()
		plan.LogoSize = types.StringUnknown()

		var d diag.Diagnostics
		in, has := brandingInput(ctx, plan, &d)
		if d.HasError() {
			t.Fatalf("unexpected diags: %v", d)
		}
		if has {
			t.Fatal("unknown values must not trigger a follow-up update")
		}
		body := wireBody(t, in)
		for _, key := range displayKeys {
			if _, sent := body[key]; sent {
				t.Errorf("%s must not be sent while unknown, body: %v", key, body)
			}
		}
	})
}

func TestBrandingInputSendsSetFields(t *testing.T) {
	plan := statuspagePlan()
	plan.Appearance = types.StringValue("dark")
	plan.AllowAppearanceSwitch = types.BoolValue(false)
	plan.LogoSize = types.StringValue("large")

	var d diag.Diagnostics
	in, has := brandingInput(context.Background(), plan, &d)
	if d.HasError() {
		t.Fatalf("unexpected diags: %v", d)
	}
	if !has {
		t.Fatal("set values must trigger the follow-up update on create")
	}

	body := wireBody(t, in)
	if body["appearance"] != "dark" {
		t.Errorf("appearance: expected dark, got %#v", body["appearance"])
	}
	// A deliberate false must survive omitempty.
	if v, sent := body["allow_appearance_switch"]; !sent || v != false {
		t.Errorf("allow_appearance_switch: expected an explicit false, got %#v (sent: %t)", v, sent)
	}
	if body["logo_size"] != "large" {
		t.Errorf("logo_size: expected large, got %#v", body["logo_size"])
	}
}

// A server that predates a field leaves its key out. That has to read as null:
// "" is not a valid value, and false would claim the visitor switch is off.
func TestStatuspageModelFromAPIReadsMissingFieldsAsNull(t *testing.T) {
	remote := decodeStatuspage(t, `{
		"id": "sp_1", "name": "Acme Status", "slug": "acme", "is_published": true,
		"access_type": "public", "show_logo": true, "show_livi": true,
		"show_affected_services": true, "show_incident_history": true
	}`)

	for name, prior := range map[string]*statuspageModel{"apply": statuspagePlan(), "import": nil} {
		t.Run(name, func(t *testing.T) {
			var d diag.Diagnostics
			m := statuspageModelFromAPI(context.Background(), remote, prior, &d)
			if d.HasError() {
				t.Fatalf("unexpected diags: %v", d)
			}
			if !m.Appearance.IsNull() {
				t.Errorf("appearance: expected null, got %s", m.Appearance)
			}
			if !m.AllowAppearanceSwitch.IsNull() {
				t.Errorf("allow_appearance_switch: expected null, got %s", m.AllowAppearanceSwitch)
			}
			if !m.LogoSize.IsNull() {
				t.Errorf("logo_size: expected null, got %s", m.LogoSize)
			}
		})
	}
}

// What the server returns lands in the state unchanged, and written back from
// the state it produces the same values on the wire.
func TestStatuspageFieldsRoundTrip(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		appearance  string
		allowSwitch bool
		logoSize    string
	}{
		{"system", true, "medium"},
		{"dark", false, "large"},
		{"light", false, "small"},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s/%t/%s", c.appearance, c.allowSwitch, c.logoSize), func(t *testing.T) {
			remote := decodeStatuspage(t, fmt.Sprintf(`{
				"id": "sp_1", "name": "Acme Status", "slug": "acme", "is_published": true,
				"access_type": "public", "appearance": %q, "allow_appearance_switch": %t, "logo_size": %q
			}`, c.appearance, c.allowSwitch, c.logoSize))

			var d diag.Diagnostics
			m := statuspageModelFromAPI(ctx, remote, statuspagePlan(), &d)
			if d.HasError() {
				t.Fatalf("unexpected diags: %v", d)
			}
			if !m.Appearance.Equal(types.StringValue(c.appearance)) {
				t.Errorf("appearance: expected %q, got %s", c.appearance, m.Appearance)
			}
			if !m.AllowAppearanceSwitch.Equal(types.BoolValue(c.allowSwitch)) {
				t.Errorf("allow_appearance_switch: expected %t, got %s", c.allowSwitch, m.AllowAppearanceSwitch)
			}
			if !m.LogoSize.Equal(types.StringValue(c.logoSize)) {
				t.Errorf("logo_size: expected %q, got %s", c.logoSize, m.LogoSize)
			}

			in, _ := brandingInput(ctx, m, &d)
			if d.HasError() {
				t.Fatalf("unexpected diags: %v", d)
			}
			body := wireBody(t, in)
			if body["appearance"] != c.appearance {
				t.Errorf("appearance on the wire: expected %q, got %#v", c.appearance, body["appearance"])
			}
			if body["allow_appearance_switch"] != c.allowSwitch {
				t.Errorf("allow_appearance_switch on the wire: expected %t, got %#v", c.allowSwitch, body["allow_appearance_switch"])
			}
			if body["logo_size"] != c.logoSize {
				t.Errorf("logo_size on the wire: expected %q, got %#v", c.logoSize, body["logo_size"])
			}
		})
	}
}
