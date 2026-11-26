package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// RecurrenceScheduleVersion is the only `schedule.v` the API understands.
const RecurrenceScheduleVersion = 1

// RecurrenceSchedule is the rule of a recurring maintenance. The API stores and
// returns it exactly as sent: no defaults are filled in, start_time is HH:MM and
// weekdays come back sorted and without duplicates. Keys that do not belong to
// the frequency (weekdays on a daily rule, month on a weekly one) are refused,
// hence the omitempty on them.
type RecurrenceSchedule struct {
	V               int              `json:"v"`
	Frequency       string           `json:"frequency"`
	Interval        int64            `json:"interval"`
	Weekdays        []int64          `json:"weekdays,omitempty"`
	Month           *RecurrenceMonth `json:"month,omitempty"`
	AnchorDate      string           `json:"anchor_date"`
	StartTime       string           `json:"start_time"`
	DurationMinutes int64            `json:"duration_minutes"`
	UntilDate       *string          `json:"until_date,omitempty"`
}

// RecurrenceMonth selects the day of a monthly rule: mode "day" with Day, or
// mode "nth_weekday" with Nth and Weekday.
type RecurrenceMonth struct {
	Mode    string     `json:"mode"`
	Day     *NumOrLast `json:"day,omitempty"`
	Nth     *NumOrLast `json:"nth,omitempty"`
	Weekday *int64     `json:"weekday,omitempty"`
}

// NumOrLast is a JSON value that is either a whole number or the string
// "last". It is kept as its decimal string ("15", "last") and written back as
// a number when it is one, because the API refuses numeric strings.
type NumOrLast string

func (n NumOrLast) MarshalJSON() ([]byte, error) {
	if v, err := strconv.ParseInt(string(n), 10, 64); err == nil {
		return []byte(strconv.FormatInt(v, 10)), nil
	}
	return json.Marshal(string(n))
}

func (n *NumOrLast) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		*n = NumOrLast(s)
		return nil
	}

	var v int64
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("expected a whole number or \"last\": %w", err)
	}
	*n = NumOrLast(strconv.FormatInt(v, 10))
	return nil
}

// RecurringMaintenance mirrors the RecurringMaintenanceResource echo.
type RecurringMaintenance struct {
	ID                      string             `json:"id"`
	Title                   string             `json:"title"`
	TitleTranslations       map[string]string  `json:"title_translations"`
	DescriptionTranslations map[string]string  `json:"description_translations"`
	Timezone                string             `json:"timezone"`
	Schedule                RecurrenceSchedule `json:"schedule"`
	RuleText                *string            `json:"rule_text"`
	ServiceIDs              []string           `json:"service_ids"`
	TagIDs                  []string           `json:"tag_ids"`
	StatuspageIDs           []string           `json:"statuspage_ids"`
	NotifyStart             bool               `json:"notify_start"`
	NotifyEnd               bool               `json:"notify_end"`
	// Paused is true only for a pause by hand. A pause caused by the plan
	// limit reads false here; PausedReason says why it is paused at all.
	Paused       bool    `json:"paused"`
	PausedReason *string `json:"paused_reason"`
	State        string  `json:"state"`
	NextStartAt  *string `json:"next_start_at"`
	NextEndAt    *string `json:"next_end_at"`
	WindowStart  *string `json:"window_start"`
	LockVersion  int64   `json:"lock_version"`
}

// RecurringMaintenanceInput is the body of create and update. On update only
// the keys sent change, so every field is optional. Description has no
// omitempty: an explicit null removes it, which is how a description dropped
// from the configuration is cleared. ClearDescription marks that case; a nil
// Description without it omits the key.
type RecurringMaintenanceInput struct {
	Title any `json:"-"` // map[string]string per language, or a plain string

	Description      map[string]string   `json:"-"`
	ClearDescription bool                `json:"-"`
	Timezone         *string             `json:"-"`
	Schedule         *RecurrenceSchedule `json:"-"`
	ServiceIDs       *[]string           `json:"-"`
	TagIDs           *[]string           `json:"-"`
	StatuspageIDs    *[]string           `json:"-"`
	NotifyStart      *bool               `json:"-"`
	NotifyEnd        *bool               `json:"-"`
	Paused           *bool               `json:"-"`
}

// MarshalJSON writes only the keys that are set, plus an explicit null for a
// description that is being cleared.
func (in RecurringMaintenanceInput) MarshalJSON() ([]byte, error) {
	body := map[string]any{}
	if in.Title != nil {
		body["title"] = in.Title
	}
	switch {
	case in.Description != nil:
		body["description"] = in.Description
	case in.ClearDescription:
		body["description"] = nil
	}
	if in.Timezone != nil {
		body["timezone"] = *in.Timezone
	}
	if in.Schedule != nil {
		body["schedule"] = in.Schedule
	}
	if in.ServiceIDs != nil {
		body["service_ids"] = *in.ServiceIDs
	}
	if in.TagIDs != nil {
		body["tag_ids"] = *in.TagIDs
	}
	if in.StatuspageIDs != nil {
		body["statuspage_ids"] = *in.StatuspageIDs
	}
	if in.NotifyStart != nil {
		body["notify_start"] = *in.NotifyStart
	}
	if in.NotifyEnd != nil {
		body["notify_end"] = *in.NotifyEnd
	}
	if in.Paused != nil {
		body["paused"] = *in.Paused
	}
	return json.Marshal(body)
}

func (c *Client) CreateRecurringMaintenance(ctx context.Context, in RecurringMaintenanceInput) (*RecurringMaintenance, error) {
	var env dataEnvelope[RecurringMaintenance]
	if err := c.do(ctx, http.MethodPost, "/recurring-maintenances", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetRecurringMaintenance(ctx context.Context, id string) (*RecurringMaintenance, error) {
	var env dataEnvelope[RecurringMaintenance]
	if err := c.do(ctx, http.MethodGet, "/recurring-maintenances/"+url.PathEscape(id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateRecurringMaintenance(ctx context.Context, id string, in RecurringMaintenanceInput) (*RecurringMaintenance, error) {
	var env dataEnvelope[RecurringMaintenance]
	if err := c.do(ctx, http.MethodPatch, "/recurring-maintenances/"+url.PathEscape(id), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// DeleteRecurringMaintenance removes the maintenance. A window running at that
// moment ends right away.
func (c *Client) DeleteRecurringMaintenance(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/recurring-maintenances/"+url.PathEscape(id), nil, nil)
}
