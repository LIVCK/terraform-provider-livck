package provider

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/livck/terraform-provider-livck/internal/client"
)

// addRecurringMaintenanceError turns an API refusal into diagnostics: a 422
// becomes one error per refused field, placed on the attribute it belongs to;
// 402, 403, 404 and 409 get a sentence that says what to do. resuming marks an
// update that sets `paused = false`, the one case where the plan limit can
// refuse an existing maintenance.
func addRecurringMaintenanceError(diags *diag.Diagnostics, summary string, err error, resuming bool) {
	var ve *client.ValidationError
	if errors.As(err, &ve) && len(ve.Violations) > 0 {
		for _, v := range ve.Violations {
			detail := violationDetail(ve, v)
			if p, ok := recurringViolationPath(v.Field); ok {
				diags.AddAttributeError(p, summary, detail)
			} else {
				diags.AddError(summary, v.Field+": "+detail)
			}
		}
		return
	}

	if errors.Is(err, client.ErrNotFound) {
		diags.AddError(summary, "The recurring maintenance does not exist (any more), or the API token cannot see it. "+
			"Run terraform apply again to create it anew, or check that the token belongs to the right organization.")
		return
	}

	var ae *client.APIError
	if !errors.As(err, &ae) {
		addAPIError(diags, summary, err)
		return
	}

	switch ae.StatusCode {
	case http.StatusPaymentRequired:
		if resuming {
			diags.AddAttributeError(path.Root("paused"), summary,
				ae.Message+"\n\nResuming would exceed the number of recurring maintenances your plan allows, so it "+
					"stays paused. Delete or pause another recurring maintenance, or upgrade the plan, then apply again.")
			return
		}
		diags.AddError(summary, ae.Message+"\n\nYour plan allows no further recurring maintenance. Delete or pause "+
			"another one, or upgrade the plan, then apply again.")
	case http.StatusForbidden:
		diags.AddError(summary, ae.Message+"\n\nThe API token lacks an ability. Recurring maintenances need "+
			"maintenances.view, .create, .edit and .delete; grant them to the token and apply again.")
	case http.StatusConflict:
		if ae.Code == "state_conflict" {
			diags.AddAttributeError(path.Root("paused"), summary, ae.Message+"\n\nThe schedule has no further "+
				"window, so the maintenance has ended and can no longer be paused or resumed. Move "+
				"schedule.until_date into the future, or leave paused as it was.")
			return
		}
		diags.AddError(summary, ae.Message+"\n\nSomeone changed the recurring maintenance at the same moment. Run "+
			"terraform apply again.")
	default:
		addAPIError(diags, summary, err)
	}
}

// violationDetail is the server's text for the field, plus what to do in
// Terraform terms where the text alone does not say it.
func violationDetail(ve *client.ValidationError, v client.Violation) string {
	text := strings.Join(ve.Errors[v.Field], " ")
	if text == "" {
		text = ve.Message
	}

	hint := recurringViolationHint(v)
	if hint == "" {
		return text + " (" + v.Code + ")"
	}
	return text + " (" + v.Code + ")\n\n" + hint
}

func recurringViolationHint(v client.Violation) string {
	if v.OverrideID != nil {
		return "An exception saved for this maintenance (" + *v.OverrideID + ") would no longer fit the new " +
			"schedule. Remove or change that exception in the console or through the API, then apply again."
	}

	switch v.Code {
	case "tag_targets_not_allowed":
		return "The API token belongs to a member restricted to access areas, and such a token cannot set " +
			"tag_ids. Use service_ids, or a token of a member without area restrictions."
	case "unknown_services", "unknown_statuspages":
		return "Check the ids. A token restricted to access areas sees only the services and status pages " +
			"of its areas."
	case "unknown_tags":
		return "Check the ids; use the public id of a livck_tag of the same organization."
	case "invalid_rule.unknown_key":
		return "Remove the attribute: weekdays belongs to a weekly schedule, the month block to a monthly one."
	case "invalid_rule.timezone":
		return "Use an IANA time zone name such as Europe/Berlin or UTC."
	case "no_future_window":
		return "Move schedule.until_date into the future, or check anchor_date and weekdays."
	}

	return "Correct the value in the configuration and apply again."
}

// recurringViolationPath maps the API's dotted field path onto the attribute
// it comes from. Indexes into lists ("service_ids.3", "schedule.weekdays.0")
// point at the whole set, since set elements have no stable position.
func recurringViolationPath(field string) (path.Path, bool) {
	parts := strings.Split(field, ".")

	switch parts[0] {
	case "title", "description":
		// A bare "title" answers a title sent as a plain string (the API reports
		// it under the key it was sent with); "title.de" answers the map.
		if parts[0] == "title" && len(parts) == 1 {
			return path.Root("title"), true
		}
		p := path.Root(parts[0] + "_translations")
		if len(parts) > 1 {
			p = p.AtMapKey(parts[1])
		}
		return p, true
	case "timezone", "notify_start", "notify_end", "paused", "service_ids", "tag_ids", "statuspage_ids":
		return path.Root(parts[0]), true
	case "schedule":
		p := path.Root("schedule")
		if len(parts) < 2 {
			return p, true
		}
		switch parts[1] {
		case "frequency", "interval", "weekdays", "anchor_date", "start_time", "duration_minutes", "until_date":
			return p.AtName(parts[1]), true
		case "month":
			p = p.AtName("month")
			if len(parts) > 2 {
				switch parts[2] {
				case "mode", "day", "nth", "weekday":
					p = p.AtName(parts[2])
				}
			}
			return p, true
		}
		// `v` and anything the provider does not know: the block as a whole.
		return p, true
	}

	return path.Empty(), false
}
