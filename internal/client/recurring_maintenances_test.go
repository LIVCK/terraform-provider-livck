package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNumOrLastKeepsNumbersAsNumbers(t *testing.T) {
	cases := map[NumOrLast]string{"15": `15`, "last": `"last"`, "1": `1`}
	for in, want := range cases {
		raw, err := json.Marshal(in)
		if err != nil || string(raw) != want {
			t.Errorf("%q marshalled to %s (%v), want %s", in, raw, err, want)
		}

		var back NumOrLast
		if err := json.Unmarshal(raw, &back); err != nil || back != in {
			t.Errorf("%s unmarshalled to %q (%v), want %q", raw, back, err, in)
		}
	}

	var bad NumOrLast
	if err := json.Unmarshal([]byte(`1.5`), &bad); err == nil {
		t.Error("a fraction must be refused")
	}
}

func TestRecurringMaintenanceInputSendsOnlyWhatIsSet(t *testing.T) {
	raw, _ := json.Marshal(RecurringMaintenanceInput{})
	if string(raw) != `{}` {
		t.Fatalf("an empty input must be an empty object, got %s", raw)
	}

	raw, _ = json.Marshal(RecurringMaintenanceInput{ClearDescription: true, ServiceIDs: &[]string{}})
	if string(raw) != `{"description":null,"service_ids":[]}` {
		t.Fatalf("expected an explicit null and an empty list, got %s", raw)
	}
}

func TestRecurringMaintenanceErrorEnvelopes(t *testing.T) {
	var status int
	var body string
	var sent map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sent = map[string]any{}
		_ = json.Unmarshal(raw, &sent)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(srv.URL, "lvk_test")

	t.Run("422 carries the violations", func(t *testing.T) {
		status = http.StatusUnprocessableEntity
		body = `{"message":"Overlap.","errors":{"schedule.duration_minutes":["Overlap."]},"violations":[{"field":"schedule.duration_minutes","code":"windows_overlap","override_id":null}]}`

		_, err := c.UpdateRecurringMaintenance(context.Background(), "rm_1", RecurringMaintenanceInput{Paused: new(bool)})
		var ve *ValidationError
		if !errors.As(err, &ve) || len(ve.Violations) != 1 || ve.Violations[0].Code != "windows_overlap" {
			t.Fatalf("expected the violation, got %#v", err)
		}
		if sent["paused"] != false || len(sent) != 1 {
			t.Fatalf("expected only paused in the body, got %v", sent)
		}
	})

	t.Run("409 carries its code", func(t *testing.T) {
		status = http.StatusConflict
		body = `{"message":"Ended.","code":"state_conflict"}`

		_, err := c.UpdateRecurringMaintenance(context.Background(), "rm_1", RecurringMaintenanceInput{})
		var ae *APIError
		if !errors.As(err, &ae) || ae.StatusCode != http.StatusConflict || ae.Code != "state_conflict" {
			t.Fatalf("expected a 409 with code, got %#v", err)
		}
	})

	t.Run("404 is ErrNotFound", func(t *testing.T) {
		status = http.StatusNotFound
		body = `{"message":"The requested resource was not found."}`

		if _, err := c.GetRecurringMaintenance(context.Background(), "rm_1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("the echo decodes", func(t *testing.T) {
		status = http.StatusOK
		body = `{"data":{"id":"rm_1","title_translations":{"en":"Nightly"},"description_translations":null,"timezone":"UTC",
			"schedule":{"v":1,"frequency":"monthly","interval":1,"month":{"mode":"day","day":"last"},"anchor_date":"2026-01-01","start_time":"02:00","duration_minutes":60},
			"service_ids":[],"tag_ids":[],"statuspage_ids":[],"notify_start":false,"notify_end":true,"paused":false,"paused_reason":"plan_limit",
			"state":"paused","next_start_at":null,"next_end_at":null,"lock_version":3}}`

		rm, err := c.GetRecurringMaintenance(context.Background(), "rm_1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rm.Schedule.Month == nil || *rm.Schedule.Month.Day != "last" || *rm.PausedReason != "plan_limit" || rm.NextStartAt != nil || !rm.NotifyEnd {
			t.Fatalf("echo decoded wrongly: %+v", rm)
		}
	})
}
