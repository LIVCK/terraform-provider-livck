package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

// settingsPlan builds a declared settings block with only probe_roles varying.
// Every other optional is null, which is what an omitted attribute looks like.
func settingsPlan(roles types.Map) *settingsModel {
	return &settingsModel{
		IntervalSeconds: types.Int64Value(60),
		TimeoutSeconds:  types.Int64Null(),
		Retries:         types.Int64Null(),
		AssignedProbes:  types.SetNull(types.StringType),
		ProbeRoles:      roles,
		Config:          jsontypes.NewNormalizedNull(),
	}
}

// settingsWire marshals the settings input as the client sends it.
func settingsWire(t *testing.T, in *client.ServiceSettingsInput) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshalling settings: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return body
}

func TestSettingsInputProbeRoles(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name  string
		roles types.Map
		want  string // raw JSON of the key, "" when the key must be absent
	}{
		{"a declared map is sent as an object", mustMap(t, map[string]string{"nyc": "reachability"}), `{"nyc":"reachability"}`},
		{"a declared empty map is sent as {}, which clears the override", mustMap(t, map[string]string{}), `{}`},
		{"a null map is sent as {}, so the organization's roles apply again", types.MapNull(types.StringType), `{}`},
		{"an unknown map leaves the key out", types.MapUnknown(types.StringType), ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, d := settingsInputFromModel(ctx, settingsPlan(c.roles))
			if d.HasError() {
				t.Fatalf("unexpected diags: %v", d)
			}
			got, sent := settingsWire(t, in)["probe_roles"]
			if c.want == "" {
				if sent {
					t.Fatalf("probe_roles must not be sent, got %s", got)
				}
				return
			}
			if string(got) != c.want {
				t.Fatalf("expected probe_roles %s on the wire, got %s (sent: %t)", c.want, got, sent)
			}
		})
	}

	t.Run("an undeclared settings block sends nothing", func(t *testing.T) {
		in, d := settingsInputFromModel(ctx, nil)
		if d.HasError() || in != nil {
			t.Fatalf("expected no settings input, got %#v (diags: %v)", in, d)
		}
	})
}

func TestServiceModelFromAPIProbeRoles(t *testing.T) {
	ctx := context.Background()

	remote := func(roles map[string]string) *client.Service {
		return &client.Service{
			ID: "svc_1", Name: "Website", CheckType: "http", Status: "up",
			Settings: &client.ServiceSettings{IntervalSeconds: 60, TimeoutSeconds: 10, Retries: 2, ProbeRoles: roles},
		}
	}
	prior := func(roles types.Map) *serviceModel {
		return &serviceModel{Tags: types.SetNull(types.StringType), Settings: settingsPlan(roles)}
	}
	read := func(t *testing.T, r *client.Service, p *serviceModel) *serviceModel {
		t.Helper()
		m, d := serviceModelFromAPI(ctx, r, p)
		if d.HasError() {
			t.Fatalf("unexpected diags: %v", d)
		}
		return m
	}

	t.Run("a service without an override reads back null", func(t *testing.T) {
		m := read(t, remote(nil), prior(types.MapNull(types.StringType)))
		if !m.Settings.ProbeRoles.IsNull() {
			t.Fatalf("expected null, got %s", m.Settings.ProbeRoles)
		}
	})

	t.Run("an override reads back as the map", func(t *testing.T) {
		roles := mustMap(t, map[string]string{"nyc": "reachability", "ffm": "full"})
		m := read(t, remote(map[string]string{"nyc": "reachability", "ffm": "full"}), prior(roles))
		if !m.Settings.ProbeRoles.Equal(roles) {
			t.Fatalf("expected %s, got %s", roles, m.Settings.ProbeRoles)
		}
	})

	// The API stores and echoes an empty map as null. Reading that null back
	// against a declared `{}` would fail the post-apply consistency check.
	t.Run("a declared empty map survives the null echo", func(t *testing.T) {
		empty := mustMap(t, map[string]string{})
		m := read(t, remote(nil), prior(empty))
		if !m.Settings.ProbeRoles.Equal(empty) {
			t.Fatalf("expected the empty map to be kept, got %s", m.Settings.ProbeRoles)
		}
	})

	t.Run("an override removed outside Terraform surfaces as drift", func(t *testing.T) {
		m := read(t, remote(nil), prior(mustMap(t, map[string]string{"nyc": "reachability"})))
		if !m.Settings.ProbeRoles.IsNull() {
			t.Fatalf("expected null (drift against the declared map), got %s", m.Settings.ProbeRoles)
		}
	})

	t.Run("an unmanaged settings block ignores the echo", func(t *testing.T) {
		m := read(t, remote(map[string]string{"nyc": "reachability"}), &serviceModel{Tags: types.SetNull(types.StringType)})
		if m.Settings != nil {
			t.Fatalf("settings must stay unmanaged, got %+v", m.Settings)
		}
	})
}
