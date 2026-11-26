package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

func strPtr(s string) *string { return &s }

func int64Set(t *testing.T, values ...int64) types.Set {
	t.Helper()
	set, d := types.SetValueFrom(context.Background(), types.Int64Type, values)
	if d.HasError() {
		t.Fatalf("building set: %v", d)
	}
	return set
}

func stringSetValue(t *testing.T, values ...string) types.Set {
	t.Helper()
	if values == nil {
		values = []string{}
	}
	set, d := types.SetValueFrom(context.Background(), types.StringType, values)
	if d.HasError() {
		t.Fatalf("building set: %v", d)
	}
	return set
}

func stringMapValue(t *testing.T, m map[string]string) types.Map {
	t.Helper()
	v, d := types.MapValueFrom(context.Background(), types.StringType, m)
	if d.HasError() {
		t.Fatalf("building map: %v", d)
	}
	return v
}

// recurringPlan is what a plan for a weekly maintenance looks like: configured
// values known, defaults applied, computed attributes unknown.
func recurringPlan(t *testing.T) *recurringMaintenanceModel {
	t.Helper()
	return &recurringMaintenanceModel{
		ID:                      types.StringUnknown(),
		Title:                   types.StringNull(),
		TitleTranslations:       stringMapValue(t, map[string]string{"en": "Nightly backup", "de": "Nächtliche Sicherung"}),
		DescriptionTranslations: types.MapNull(types.StringType),
		Timezone:                types.StringValue("Europe/Berlin"),
		Schedule: &recurrenceScheduleModel{
			Frequency:       types.StringValue("weekly"),
			Interval:        types.Int64Value(1),
			Weekdays:        int64Set(t, 5, 1, 3),
			AnchorDate:      types.StringValue("2026-01-01"),
			StartTime:       types.StringValue("02:00"),
			DurationMinutes: types.Int64Value(60),
			UntilDate:       types.StringNull(),
		},
		ServiceIDs:    types.SetUnknown(types.StringType),
		TagIDs:        stringSetValue(t, "tag_1"),
		StatuspageIDs: types.SetUnknown(types.StringType),
		NotifyStart:   types.BoolValue(false),
		NotifyEnd:     types.BoolValue(false),
		Paused:        types.BoolValue(false),
		PausedReason:  types.StringUnknown(),
		State:         types.StringUnknown(),
		NextStartAt:   types.StringUnknown(),
		NextEndAt:     types.StringUnknown(),
		RuleText:      types.StringUnknown(),
	}
}

// echo is what the API answers for recurringPlan: weekdays sorted, no
// description, targets as lists.
func echo() *client.RecurringMaintenance {
	return &client.RecurringMaintenance{
		ID:                "rm_abc",
		Title:             "Nightly backup",
		TitleTranslations: map[string]string{"en": "Nightly backup", "de": "Nächtliche Sicherung"},
		Timezone:          "Europe/Berlin",
		Schedule: client.RecurrenceSchedule{
			V: 1, Frequency: "weekly", Interval: 1, Weekdays: []int64{1, 3, 5},
			AnchorDate: "2026-01-01", StartTime: "02:00", DurationMinutes: 60,
		},
		RuleText:      strPtr("Weekly on Monday, Wednesday and Friday from 02:00 to 03:00 (Europe/Berlin)"),
		ServiceIDs:    []string{},
		TagIDs:        []string{"tag_1"},
		StatuspageIDs: []string{},
		State:         "idle",
		NextStartAt:   strPtr("2026-11-16T01:00:00+00:00"),
		NextEndAt:     strPtr("2026-11-16T02:00:00+00:00"),
	}
}

func marshalInput(t *testing.T, in client.RecurringMaintenanceInput) map[string]any {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}
	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return body
}

func TestRecurringCreateInputCarriesTheConfiguration(t *testing.T) {
	ctx := context.Background()

	in, d := recurringMaintenanceCreateInput(ctx, recurringPlan(t))
	if d.HasError() {
		t.Fatalf("unexpected diags: %v", d)
	}
	body := marshalInput(t, in)

	schedule := body["schedule"].(map[string]any)
	if schedule["v"] != float64(1) || schedule["interval"] != float64(1) || schedule["start_time"] != "02:00" {
		t.Fatalf("schedule not sent as configured: %v", schedule)
	}
	if days := schedule["weekdays"].([]any); len(days) != 3 {
		t.Fatalf("expected three weekdays, got %v", days)
	}
	for _, key := range []string{"month", "until_date"} {
		if _, ok := schedule[key]; ok {
			t.Fatalf("%s must be left out when not configured (the API refuses foreign keys): %v", key, schedule)
		}
	}

	// Unknown sets (not configured) are left out, configured ones sent.
	if _, ok := body["service_ids"]; ok {
		t.Fatalf("an unconfigured service_ids must not be sent: %v", body)
	}
	if tags := body["tag_ids"].([]any); len(tags) != 1 || tags[0] != "tag_1" {
		t.Fatalf("tag_ids not sent: %v", body)
	}
	if _, ok := body["description"]; ok {
		t.Fatalf("no description configured, none sent on create: %v", body)
	}
	if body["notify_start"] != false || body["notify_end"] != false || body["paused"] != false {
		t.Fatalf("the switches must always be sent on create: %v", body)
	}
	if _, ok := body["lock_version"]; ok {
		t.Fatal("lock_version is never sent: the last writer wins")
	}
	if title := body["title"].(map[string]any); title["de"] != "Nächtliche Sicherung" {
		t.Fatalf("title not sent as a translation map: %v", body["title"])
	}
}

// A single-language title goes out as a plain string (the API stores it in the
// organization's default language) and reads back as the string, with the map
// staying null; switching between the two shapes sends the new one.
func TestRecurringPlainTitle(t *testing.T) {
	ctx := context.Background()

	plan := recurringPlan(t)
	plan.Title = types.StringValue("Nightly backup")
	plan.TitleTranslations = types.MapNull(types.StringType)

	in, d := recurringMaintenanceCreateInput(ctx, plan)
	if d.HasError() {
		t.Fatalf("unexpected diags: %v", d)
	}
	if body := marshalInput(t, in); body["title"] != "Nightly backup" {
		t.Fatalf("plain title not sent as a string: %v", body["title"])
	}

	remote := echo()
	remote.TitleTranslations = map[string]string{"de": "Nightly backup"}
	state, d := recurringMaintenanceModelFromAPI(ctx, remote, plan)
	if d.HasError() {
		t.Fatalf("unexpected diags: %v", d)
	}
	if !state.Title.Equal(plan.Title) || !state.TitleTranslations.IsNull() {
		t.Fatalf("plain title must read back as the string with a null map: %v / %v", state.Title, state.TitleTranslations)
	}
	if in, _ := recurringMaintenanceUpdateInput(ctx, plan, state); in.Title != nil {
		t.Fatalf("an unchanged plain title must not be sent again: %v", in.Title)
	}

	// From the string to a map: the map is sent.
	toMap := *plan
	toMap.Title = types.StringNull()
	toMap.TitleTranslations = stringMapValue(t, map[string]string{"en": "Nightly backup", "de": "Nächtliche Sicherung"})
	in, _ = recurringMaintenanceUpdateInput(ctx, &toMap, state)
	if title, ok := in.Title.(map[string]string); !ok || title["de"] != "Nächtliche Sicherung" {
		t.Fatalf("switching to title_translations must send the map: %#v", in.Title)
	}

	// An import has no prior shape: the title reads as the full map.
	imported, _ := recurringMaintenanceModelFromAPI(ctx, remote, &recurringMaintenanceModel{})
	if !imported.Title.IsNull() || imported.TitleTranslations.IsNull() {
		t.Fatalf("an import must read the title as the map: %v / %v", imported.Title, imported.TitleTranslations)
	}

	// From a map to the string: the string is sent.
	mapState, _ := recurringMaintenanceModelFromAPI(ctx, echo(), recurringPlan(t))
	in, _ = recurringMaintenanceUpdateInput(ctx, plan, mapState)
	if in.Title != "Nightly backup" {
		t.Fatalf("switching to title must send the string: %#v", in.Title)
	}
}

func TestRecurringMonthlyScheduleIsSentWithNumbersAndLast(t *testing.T) {
	ctx := context.Background()

	plan := recurringPlan(t)
	plan.Schedule.Frequency = types.StringValue("monthly")
	plan.Schedule.Weekdays = types.SetNull(types.Int64Type)
	plan.Schedule.UntilDate = types.StringValue("2030-12-31")

	cases := map[string]struct {
		month *recurrenceMonthModel
		want  string
	}{
		"day as number": {
			month: &recurrenceMonthModel{Mode: types.StringValue("day"), Day: types.StringValue("15"), Nth: types.StringNull(), Weekday: types.Int64Null()},
			want:  `"month":{"mode":"day","day":15}`,
		},
		"last day": {
			month: &recurrenceMonthModel{Mode: types.StringValue("day"), Day: types.StringValue("last"), Nth: types.StringNull(), Weekday: types.Int64Null()},
			want:  `"month":{"mode":"day","day":"last"}`,
		},
		"last sunday": {
			month: &recurrenceMonthModel{Mode: types.StringValue("nth_weekday"), Day: types.StringNull(), Nth: types.StringValue("last"), Weekday: types.Int64Value(0)},
			want:  `"month":{"mode":"nth_weekday","nth":"last","weekday":0}`,
		},
		"second tuesday": {
			month: &recurrenceMonthModel{Mode: types.StringValue("nth_weekday"), Day: types.StringNull(), Nth: types.StringValue("2"), Weekday: types.Int64Value(2)},
			want:  `"month":{"mode":"nth_weekday","nth":2,"weekday":2}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			plan.Schedule.Month = tc.month
			in, d := recurringMaintenanceCreateInput(ctx, plan)
			if d.HasError() {
				t.Fatalf("unexpected diags: %v", d)
			}
			raw, _ := json.Marshal(in.Schedule)
			if !strings.Contains(string(raw), tc.want) || strings.Contains(string(raw), "weekdays") {
				t.Fatalf("expected %s in %s", tc.want, raw)
			}
			if !strings.Contains(string(raw), `"until_date":"2030-12-31"`) {
				t.Fatalf("until_date missing in %s", raw)
			}

			// What the API echoes maps back onto exactly the planned block.
			var echoed client.RecurrenceSchedule
			if err := json.Unmarshal(raw, &echoed); err != nil {
				t.Fatalf("decoding %s: %v", raw, err)
			}
			var diags diag.Diagnostics
			back := scheduleFromAPI(ctx, echoed, &diags)
			if diags.HasError() || !scheduleEqual(back, plan.Schedule) {
				t.Fatalf("round trip changed the schedule: %+v / %+v (%v)", back, plan.Schedule, diags)
			}
		})
	}
}

// The API answers weekdays sorted; a set compares regardless of order, so the
// configured [5, 1, 3] and the echo [1, 3, 5] are the same value, and a
// read-back state equals the plan for every configured attribute.
func TestRecurringEchoRoundTripsWithoutDiff(t *testing.T) {
	ctx := context.Background()
	plan := recurringPlan(t)

	state, d := recurringMaintenanceModelFromAPI(ctx, echo(), plan)
	if d.HasError() {
		t.Fatalf("unexpected diags: %v", d)
	}

	if !scheduleEqual(state.Schedule, plan.Schedule) {
		t.Fatalf("schedule drifted: %+v vs %+v", state.Schedule, plan.Schedule)
	}
	if !state.Schedule.Weekdays.Equal(int64Set(t, 3, 5, 1)) {
		t.Fatal("weekdays must compare as a set")
	}
	checks := map[string]bool{
		"title":          state.TitleTranslations.Equal(plan.TitleTranslations),
		"description":    state.DescriptionTranslations.IsNull(),
		"timezone":       state.Timezone.Equal(plan.Timezone),
		"tag_ids":        state.TagIDs.Equal(plan.TagIDs),
		"service_ids":    state.ServiceIDs.Equal(stringSetValue(t)),
		"statuspage_ids": state.StatuspageIDs.Equal(stringSetValue(t)),
		"notify_start":   state.NotifyStart.Equal(plan.NotifyStart),
		"notify_end":     state.NotifyEnd.Equal(plan.NotifyEnd),
		"paused":         state.Paused.Equal(plan.Paused),
		"paused_reason":  state.PausedReason.IsNull(),
		"state":          state.State.ValueString() == "idle",
		"next_start_at":  state.NextStartAt.ValueString() == "2026-11-16T01:00:00+00:00",
		"rule_text":      !state.RuleText.IsNull(),
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("%s not mapped as expected", name)
		}
	}

	// After a window has run only the computed attributes move; the next
	// update input built from the unchanged plan and that state is empty.
	ran := echo()
	ran.State = "running"
	ran.NextStartAt = strPtr("2026-11-18T01:00:00+00:00")
	ran.NextEndAt = strPtr("2026-11-18T02:00:00+00:00")
	ran.WindowStart = strPtr("2026-11-18T01:00:00+00:00")
	refreshed, d := recurringMaintenanceModelFromAPI(ctx, ran, state)
	if d.HasError() {
		t.Fatalf("unexpected diags: %v", d)
	}
	configured := *refreshed
	configured.ServiceIDs = state.ServiceIDs
	in, d := recurringMaintenanceUpdateInput(ctx, &configured, refreshed)
	if d.HasError() {
		t.Fatalf("unexpected diags: %v", d)
	}
	if body := marshalInput(t, in); len(body) != 0 {
		t.Fatalf("a window that ran must not lead to a change, got %v", body)
	}
}

func TestRecurringDescriptionAndEmptyTranslationsMapping(t *testing.T) {
	ctx := context.Background()
	remote := echo()
	remote.DescriptionTranslations = map[string]string{"en": "Database dump"}
	remote.ServiceIDs = nil

	state, d := recurringMaintenanceModelFromAPI(ctx, remote, recurringPlan(t))
	if d.HasError() {
		t.Fatalf("unexpected diags: %v", d)
	}
	if state.DescriptionTranslations.IsNull() {
		t.Fatal("a description must be read back")
	}
	if state.ServiceIDs.IsNull() || len(state.ServiceIDs.Elements()) != 0 {
		t.Fatal("a missing list reads as an empty set, never null")
	}
}

func TestRecurringUpdateInputSendsOnlyChanges(t *testing.T) {
	ctx := context.Background()
	state, d := recurringMaintenanceModelFromAPI(ctx, echo(), recurringPlan(t))
	if d.HasError() {
		t.Fatalf("unexpected diags: %v", d)
	}
	state.DescriptionTranslations = stringMapValue(t, map[string]string{"en": "Database dump"})

	t.Run("removed description is cleared with an explicit null", func(t *testing.T) {
		plan := *state
		plan.DescriptionTranslations = types.MapNull(types.StringType)
		in, _ := recurringMaintenanceUpdateInput(ctx, &plan, state)
		raw, _ := json.Marshal(in)
		if string(raw) != `{"description":null}` {
			t.Fatalf("expected only the cleared description, got %s", raw)
		}
	})

	t.Run("pausing sends paused alone", func(t *testing.T) {
		plan := *state
		plan.Paused = types.BoolValue(true)
		in, _ := recurringMaintenanceUpdateInput(ctx, &plan, state)
		raw, _ := json.Marshal(in)
		if string(raw) != `{"paused":true}` {
			t.Fatalf("expected only paused, got %s", raw)
		}
	})

	t.Run("a timezone change sends the rule along", func(t *testing.T) {
		plan := *state
		plan.Timezone = types.StringValue("UTC")
		body := marshalInput(t, mustUpdateInput(t, &plan, state))
		if body["timezone"] != "UTC" || body["schedule"] == nil || len(body) != 2 {
			t.Fatalf("expected timezone and schedule, got %v", body)
		}
	})

	t.Run("an emptied target set is sent as an empty list", func(t *testing.T) {
		plan := *state
		plan.TagIDs = stringSetValue(t)
		raw, _ := json.Marshal(mustUpdateInput(t, &plan, state))
		if string(raw) != `{"tag_ids":[]}` {
			t.Fatalf("expected tag_ids [], got %s", raw)
		}
	})

	t.Run("weekdays in another order are no change", func(t *testing.T) {
		plan := *state
		schedule := *state.Schedule
		schedule.Weekdays = int64Set(t, 5, 3, 1)
		plan.Schedule = &schedule
		raw, _ := json.Marshal(mustUpdateInput(t, &plan, state))
		if string(raw) != `{}` {
			t.Fatalf("expected no change, got %s", raw)
		}
	})
}

func mustUpdateInput(t *testing.T, plan, state *recurringMaintenanceModel) client.RecurringMaintenanceInput {
	t.Helper()
	in, d := recurringMaintenanceUpdateInput(context.Background(), plan, state)
	if d.HasError() {
		t.Fatalf("unexpected diags: %v", d)
	}
	return in
}

func TestRecurringSwitchDefaultsAreFalseAndIntervalOne(t *testing.T) {
	resp := &resource.SchemaResponse{}
	NewRecurringMaintenanceResource().Schema(context.Background(), resource.SchemaRequest{}, resp)

	for _, name := range []string{"notify_start", "notify_end", "paused"} {
		a := resp.Schema.Attributes[name].(schema.BoolAttribute)
		if !a.Optional || !a.Computed || a.Default == nil {
			t.Fatalf("%s must be optional with a default", name)
		}
		def := &defaults.BoolResponse{}
		a.Default.DefaultBool(context.Background(), defaults.BoolRequest{}, def)
		if def.PlanValue.ValueBool() {
			t.Fatalf("%s must default to false", name)
		}
	}

	interval := resp.Schema.Blocks["schedule"].(schema.SingleNestedBlock).Attributes["interval"].(schema.Int64Attribute)
	def := &defaults.Int64Response{}
	interval.Default.DefaultInt64(context.Background(), defaults.Int64Request{}, def)
	if def.PlanValue.ValueInt64() != 1 {
		t.Fatalf("interval must default to 1, got %v", def.PlanValue)
	}

	// The window attributes move with every window; a plan modifier keeping
	// the prior value would make a plan claim a stale time.
	for _, name := range []string{"next_start_at", "next_end_at", "state", "rule_text", "paused_reason"} {
		a := resp.Schema.Attributes[name].(schema.StringAttribute)
		if !a.Computed || a.Optional || len(a.PlanModifiers) != 0 {
			t.Fatalf("%s must be computed only, without plan modifiers", name)
		}
	}
	if _, ok := resp.Schema.Attributes["lock_version"]; ok {
		t.Fatal("lock_version is not part of the resource")
	}
}

func TestRecurringPausedAndPausedReason(t *testing.T) {
	cases := []struct {
		name       string
		paused     bool
		reason     *string
		prior      types.Bool
		wantPaused bool
	}{
		{"running normally", false, nil, types.BoolValue(false), false},
		{"paused by hand", true, strPtr("manual"), types.BoolValue(true), true},
		{"paused by hand in the console, config says false", true, strPtr("manual"), types.BoolValue(false), true},
		{"resumed in the console, config says true", false, nil, types.BoolValue(true), false},
		{"plan limit, config false", false, strPtr("plan_limit"), types.BoolValue(false), false},
		{"plan limit, config true", false, strPtr("plan_limit"), types.BoolValue(true), true},
		{"plan limit on import", false, strPtr("plan_limit"), types.BoolNull(), false},
		{"organization deleted, config false", false, strPtr("org_deleted"), types.BoolValue(false), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			remote := echo()
			remote.Paused = tc.paused
			remote.PausedReason = tc.reason
			if tc.reason != nil {
				remote.State = "paused"
			}

			prior := recurringPlan(t)
			prior.Paused = tc.prior
			state, d := recurringMaintenanceModelFromAPI(context.Background(), remote, prior)
			if d.HasError() {
				t.Fatalf("unexpected diags: %v", d)
			}
			if state.Paused.ValueBool() != tc.wantPaused {
				t.Fatalf("paused = %v, want %v", state.Paused.ValueBool(), tc.wantPaused)
			}
			if tc.reason == nil && !state.PausedReason.IsNull() {
				t.Fatal("paused_reason must be null when not paused")
			}
			if tc.reason != nil && state.PausedReason.ValueString() != *tc.reason {
				t.Fatalf("paused_reason = %q, want %q", state.PausedReason.ValueString(), *tc.reason)
			}
		})
	}

	t.Run("a plan limit pause warns after apply", func(t *testing.T) {
		remote := echo()
		remote.PausedReason = strPtr("plan_limit")
		var diags diag.Diagnostics
		warnPausedByPlan(&diags, remote)
		if diags.WarningsCount() != 1 || diags.HasError() {
			t.Fatalf("expected one warning, got %v", diags)
		}
	})
}

func TestRecurringScheduleValidation(t *testing.T) {
	base := func() *recurrenceScheduleModel {
		return &recurrenceScheduleModel{
			Frequency: types.StringValue("daily"), Interval: types.Int64Value(1), Weekdays: types.SetNull(types.Int64Type),
			AnchorDate: types.StringValue("2026-01-01"), StartTime: types.StringValue("02:00"),
			DurationMinutes: types.Int64Value(60), UntilDate: types.StringNull(),
		}
	}
	month := func(mode, day, nth string, weekday *int64) *recurrenceMonthModel {
		m := &recurrenceMonthModel{Mode: types.StringValue(mode), Day: types.StringNull(), Nth: types.StringNull(), Weekday: types.Int64PointerValue(weekday)}
		if day != "" {
			m.Day = types.StringValue(day)
		}
		if nth != "" {
			m.Nth = types.StringValue(nth)
		}
		return m
	}
	sunday := int64(0)

	cases := []struct {
		name   string
		mutate func(s *recurrenceScheduleModel)
		errors int
		at     string
	}{
		{"daily is fine", func(*recurrenceScheduleModel) {}, 0, ""},
		{"weekly without weekdays", func(s *recurrenceScheduleModel) { s.Frequency = types.StringValue("weekly") }, 1, "schedule.weekdays"},
		{"weekly with weekdays", func(s *recurrenceScheduleModel) {
			s.Frequency = types.StringValue("weekly")
			s.Weekdays = int64Set(t, 1)
		}, 0, ""},
		{"daily with weekdays", func(s *recurrenceScheduleModel) { s.Weekdays = int64Set(t, 1) }, 1, "schedule.weekdays"},
		{"daily with month", func(s *recurrenceScheduleModel) { s.Month = month("day", "1", "", nil) }, 1, "schedule.month"},
		{"monthly without month", func(s *recurrenceScheduleModel) { s.Frequency = types.StringValue("monthly") }, 1, "schedule.month"},
		{"monthly by day", func(s *recurrenceScheduleModel) {
			s.Frequency = types.StringValue("monthly")
			s.Month = month("day", "last", "", nil)
		}, 0, ""},
		{"monthly by day without day", func(s *recurrenceScheduleModel) {
			s.Frequency = types.StringValue("monthly")
			s.Month = month("day", "", "", nil)
		}, 1, "schedule.month.day"},
		{"monthly by day with weekday", func(s *recurrenceScheduleModel) {
			s.Frequency = types.StringValue("monthly")
			s.Month = month("day", "1", "", &sunday)
		}, 1, "schedule.month"},
		{"monthly last sunday", func(s *recurrenceScheduleModel) {
			s.Frequency = types.StringValue("monthly")
			s.Month = month("nth_weekday", "", "last", &sunday)
		}, 0, ""},
		{"monthly nth without weekday", func(s *recurrenceScheduleModel) {
			s.Frequency = types.StringValue("monthly")
			s.Month = month("nth_weekday", "", "2", nil)
		}, 1, "schedule.month"},
		{"unknown frequency is left to apply", func(s *recurrenceScheduleModel) {
			s.Frequency = types.StringUnknown()
			s.Weekdays = int64Set(t, 1)
		}, 0, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.mutate(s)
			var diags diag.Diagnostics
			validateRecurrenceSchedule(s, &diags)
			if diags.ErrorsCount() != tc.errors {
				t.Fatalf("expected %d errors, got %v", tc.errors, diags)
			}
			if tc.at != "" {
				withPath, ok := diags[0].(diag.DiagnosticWithPath)
				if !ok || withPath.Path().String() != tc.at {
					t.Fatalf("expected the error at %s, got %v", tc.at, diags[0])
				}
			}
		})
	}
}

func TestRecurringViolationPaths(t *testing.T) {
	cases := map[string]string{
		"title":                     "title",
		"title.de":                  `title_translations["de"]`,
		"description.en":            `description_translations["en"]`,
		"timezone":                  "timezone",
		"schedule":                  "schedule",
		"schedule.v":                "schedule",
		"schedule.start_time":       "schedule.start_time",
		"schedule.weekdays.0":       "schedule.weekdays",
		"schedule.duration_minutes": "schedule.duration_minutes",
		"schedule.month":            "schedule.month",
		"schedule.month.nth":        "schedule.month.nth",
		"service_ids":               "service_ids",
		"service_ids.3":             "service_ids",
		"tag_ids":                   "tag_ids",
		"statuspage_ids.0":          "statuspage_ids",
		"paused":                    "paused",
		"notify_end":                "notify_end",
	}
	for field, want := range cases {
		p, ok := recurringViolationPath(field)
		if !ok || p.String() != want {
			t.Errorf("%s -> %v (%v), want %s", field, p, ok, want)
		}
	}
	if _, ok := recurringViolationPath("something_new"); ok {
		t.Error("an unknown field must not be pinned to an attribute")
	}
}

func TestRecurringErrorsBecomeDiagnostics(t *testing.T) {
	type expectation struct {
		at       string
		contains string
	}

	cases := []struct {
		name     string
		err      error
		resuming bool
		want     []expectation
	}{
		{
			name: "422 with violations, one per field",
			err: &client.ValidationError{
				Message: "With this schedule two windows overlap.",
				Errors: map[string][]string{
					"schedule.duration_minutes": {"With this schedule two windows overlap. Shorten the duration or choose a longer interval."},
					"tag_ids":                   {"Only members who see every area can target tags."},
				},
				Violations: []client.Violation{
					{Field: "schedule.duration_minutes", Code: "windows_overlap"},
					{Field: "tag_ids", Code: "tag_targets_not_allowed"},
				},
			},
			want: []expectation{
				{"schedule.duration_minutes", "windows_overlap"},
				{"tag_ids", "Use service_ids"},
			},
		},
		{
			name: "422 that breaks an exception names it",
			err: &client.ValidationError{
				Message:    "There is no window in this period that you could move.",
				Errors:     map[string][]string{"schedule": {"There is no window in this period that you could move."}},
				Violations: []client.Violation{{Field: "schedule", Code: "replace_hits_none", OverrideID: strPtr("ovr_1")}},
			},
			want: []expectation{{"schedule", "ovr_1"}},
		},
		{
			name: "422 on an unknown field stays general",
			err: &client.ValidationError{
				Message:    "Bad.",
				Errors:     map[string][]string{"x": {"Bad."}},
				Violations: []client.Violation{{Field: "x", Code: "invalid"}},
			},
			want: []expectation{{"", "x: Bad."}},
		},
		{
			name: "402 on create",
			err:  &client.APIError{StatusCode: 402, Message: "Limit reached."},
			want: []expectation{{"", "plan allows no further recurring maintenance"}},
		},
		{
			name:     "402 on resume points at paused",
			err:      &client.APIError{StatusCode: 402, Message: "Limit reached."},
			resuming: true,
			want:     []expectation{{"paused", "stays paused"}},
		},
		{
			name: "409 for an ended maintenance points at paused",
			err:  &client.APIError{StatusCode: 409, Message: "Not in a state that allows this.", Code: "state_conflict"},
			want: []expectation{{"paused", "until_date"}},
		},
		{
			name: "409 otherwise asks to apply again",
			err:  &client.APIError{StatusCode: 409, Message: "Changed.", Code: "stale"},
			want: []expectation{{"", "apply again"}},
		},
		{
			name: "403 names the abilities",
			err:  &client.APIError{StatusCode: 403, Message: "Forbidden."},
			want: []expectation{{"", "maintenances.view"}},
		},
		{
			name: "404",
			err:  client.ErrNotFound,
			want: []expectation{{"", "does not exist"}},
		},
		{
			name: "anything else passes through",
			err:  errors.New("connection refused"),
			want: []expectation{{"", "connection refused"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics
			addRecurringMaintenanceError(&diags, "Updating the recurring maintenance failed", tc.err, tc.resuming)

			if diags.ErrorsCount() != len(tc.want) {
				t.Fatalf("expected %d errors, got %v", len(tc.want), diags)
			}
			for i, want := range tc.want {
				got := diags[i]
				at := ""
				if withPath, ok := got.(diag.DiagnosticWithPath); ok {
					at = withPath.Path().String()
				}
				if at != want.at {
					t.Errorf("error %d at %q, want %q", i, at, want.at)
				}
				if !strings.Contains(got.Detail(), want.contains) {
					t.Errorf("error %d detail %q does not contain %q", i, got.Detail(), want.contains)
				}
			}
		})
	}

}
