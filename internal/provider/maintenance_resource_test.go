package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

func mustRFC3339(t *testing.T, s string) timetypes.RFC3339 {
	t.Helper()
	v, d := timetypes.NewRFC3339Value(s)
	if d.HasError() {
		t.Fatalf("building timestamp %q: %v", s, d)
	}
	return v
}

// maintenancePlan builds a minimal model with only the end varying - every other
// attribute is null, which is what an unset optional looks like in a plan.
func maintenancePlan(t *testing.T, end timetypes.RFC3339) *maintenanceModel {
	t.Helper()
	return &maintenanceModel{
		Title:             types.StringValue("Database upgrade"),
		TitleTranslations: types.MapNull(types.StringType),
		Type:              types.StringNull(),
		ScheduledStart:    mustRFC3339(t, "2026-08-01T22:00:00Z"),
		ScheduledEnd:      end,
		ServiceIDs:        types.SetNull(types.StringType),
		StatuspageIDs:     types.SetNull(types.StringType),
		AutoStart:         types.BoolNull(),
		AutoComplete:      types.BoolNull(),
		Notify24h:         types.BoolNull(),
		Notify1h:          types.BoolNull(),
		NotifyStart:       types.BoolNull(),
		NotifyComplete:    types.BoolNull(),
	}
}

// An omitted scheduled_end must reach the API as an explicit JSON null, not as a
// dropped key: `omitempty` would make PATCH mean "keep the current end", so an
// existing window could never be reopened.
func TestMaintenanceInputTransmitsNullEndExplicitly(t *testing.T) {
	ctx := context.Background()

	t.Run("unset end marshals to an explicit null", func(t *testing.T) {
		in, d := maintenanceInputFromModel(ctx, maintenancePlan(t, timetypes.NewRFC3339Null()))
		if d.HasError() {
			t.Fatalf("unexpected diags: %v", d)
		}
		if in.ScheduledEnd != nil {
			t.Fatalf("expected a nil end, got %q", *in.ScheduledEnd)
		}

		body, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshalling input: %v", err)
		}
		if !strings.Contains(string(body), `"scheduled_end":null`) {
			t.Fatalf("scheduled_end must be sent as an explicit null, body was: %s", body)
		}
	})

	t.Run("set end is passed through", func(t *testing.T) {
		end, d := timetypes.NewRFC3339Value("2026-08-02T02:00:00Z")
		if d.HasError() {
			t.Fatalf("building end: %v", d)
		}
		in, diags := maintenanceInputFromModel(ctx, maintenancePlan(t, end))
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if in.ScheduledEnd == nil || *in.ScheduledEnd != "2026-08-02T02:00:00Z" {
			t.Fatalf("expected the configured end to be sent, got %#v", in.ScheduledEnd)
		}
	})
}

// A null echo must round-trip as a null attribute. Parsing "" as a timestamp
// would raise diags, and echoing "" against a null plan would trip Terraform's
// post-apply consistency check ("provider produced inconsistent result").
func TestMaintenanceModelFromAPIHandlesOpenEndedWindow(t *testing.T) {
	ctx := context.Background()

	remote := &client.Maintenance{
		ID:             "mnt_abc",
		Title:          "Database upgrade",
		Type:           "planned",
		Status:         "scheduled",
		ScheduledStart: "2026-08-01T22:00:00+00:00",
		ScheduledEnd:   nil,
	}

	t.Run("null echo against a null plan stays null", func(t *testing.T) {
		state, d := maintenanceModelFromAPI(ctx, remote, maintenancePlan(t, timetypes.NewRFC3339Null()))
		if d.HasError() {
			t.Fatalf("unexpected diags: %v", d)
		}
		if !state.ScheduledEnd.IsNull() {
			t.Fatalf("expected a null end, got %q", state.ScheduledEnd.ValueString())
		}
	})

	t.Run("an end appearing server-side surfaces as drift", func(t *testing.T) {
		withEnd := *remote
		echoed := "2026-08-02T02:00:00+00:00"
		withEnd.ScheduledEnd = &echoed

		state, d := maintenanceModelFromAPI(ctx, &withEnd, maintenancePlan(t, timetypes.NewRFC3339Null()))
		if d.HasError() {
			t.Fatalf("unexpected diags: %v", d)
		}
		if state.ScheduledEnd.IsNull() {
			t.Fatal("a server-side end must not be swallowed into a null state")
		}
	})
}
