package provider

import (
	"context"
	"errors"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

var (
	_ resource.Resource                   = (*recurringMaintenanceResource)(nil)
	_ resource.ResourceWithConfigure      = (*recurringMaintenanceResource)(nil)
	_ resource.ResourceWithImportState    = (*recurringMaintenanceResource)(nil)
	_ resource.ResourceWithValidateConfig = (*recurringMaintenanceResource)(nil)
)

// Bounds and formats of a recurring maintenance, as the API enforces them.
// Checking them at plan time reports a typo before anything is sent.
const (
	recurringMinDurationMinutes = 5
	recurringMaxDurationMinutes = 7 * 24 * 60
	recurringMaxInterval        = 99
	recurringMaxServices        = 400
	recurringMaxTags            = 20
	recurringMaxStatuspages     = 50
	recurringTitleMaxLength     = 255
	recurringDescMaxLength      = 5000
)

var (
	isoDatePattern    = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$`)
	clockTimePattern  = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	monthDayPattern   = regexp.MustCompile(`^([1-9]|[12]\d|3[01]|last)$`)
	nthWeekdayPattern = regexp.MustCompile(`^([1-4]|last)$`)
)

const (
	frequencyDaily   = "daily"
	frequencyWeekly  = "weekly"
	frequencyMonthly = "monthly"

	monthModeDay        = "day"
	monthModeNthWeekday = "nth_weekday"

	pausedReasonManual = "manual"
)

type recurringMaintenanceResource struct {
	client *client.Client
}

type recurringMaintenanceModel struct {
	ID                      types.String             `tfsdk:"id"`
	Title                   types.String             `tfsdk:"title"`
	TitleTranslations       types.Map                `tfsdk:"title_translations"`
	DescriptionTranslations types.Map                `tfsdk:"description_translations"`
	Timezone                types.String             `tfsdk:"timezone"`
	Schedule                *recurrenceScheduleModel `tfsdk:"schedule"`
	ServiceIDs              types.Set                `tfsdk:"service_ids"`
	TagIDs                  types.Set                `tfsdk:"tag_ids"`
	StatuspageIDs           types.Set                `tfsdk:"statuspage_ids"`
	NotifyStart             types.Bool               `tfsdk:"notify_start"`
	NotifyEnd               types.Bool               `tfsdk:"notify_end"`
	Paused                  types.Bool               `tfsdk:"paused"`
	PausedReason            types.String             `tfsdk:"paused_reason"`
	State                   types.String             `tfsdk:"state"`
	NextStartAt             types.String             `tfsdk:"next_start_at"`
	NextEndAt               types.String             `tfsdk:"next_end_at"`
	RuleText                types.String             `tfsdk:"rule_text"`
}

type recurrenceScheduleModel struct {
	Frequency       types.String          `tfsdk:"frequency"`
	Interval        types.Int64           `tfsdk:"interval"`
	Weekdays        types.Set             `tfsdk:"weekdays"`
	Month           *recurrenceMonthModel `tfsdk:"month"`
	AnchorDate      types.String          `tfsdk:"anchor_date"`
	StartTime       types.String          `tfsdk:"start_time"`
	DurationMinutes types.Int64           `tfsdk:"duration_minutes"`
	UntilDate       types.String          `tfsdk:"until_date"`
}

type recurrenceMonthModel struct {
	Mode    types.String `tfsdk:"mode"`
	Day     types.String `tfsdk:"day"`
	Nth     types.String `tfsdk:"nth"`
	Weekday types.Int64  `tfsdk:"weekday"`
}

func NewRecurringMaintenanceResource() resource.Resource { return &recurringMaintenanceResource{} }

func (r *recurringMaintenanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_recurring_maintenance"
}

func (r *recurringMaintenanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	nonEmptyIDs := setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))

	resp.Schema = schema.Schema{
		MarkdownDescription: "A maintenance window that repeats on a schedule, for example every night from 02:00 to 03:00. " +
			"While a window runs, the covered services raise no incidents and send no alerts, and the linked " +
			"status pages show the maintenance. Windows open and close on their own; the computed attributes " +
			"(`state`, `next_start_at`, `next_end_at`, `rule_text`) follow them and never cause a diff.\n\n" +
			"Exceptions (skipping or moving single windows) are not part of this resource. Manage them in the " +
			"console or through the API; they do not cause a diff here.\n\n" +
			"Destroying the resource deletes the recurring maintenance. A window running at that moment ends " +
			"right away and monitoring resumes; past windows stay in the history.\n\n" +
			"With an API token restricted to access areas, the API lists only the services and status pages " +
			"of those areas. A configuration that names others then shows a diff on every plan; use a token " +
			"without area restrictions for maintenances that span areas.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"title": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Single-language title, stored in the organization's default language. " +
					"Exactly one of `title` and `title_translations` must be set. An import reads the title " +
					"as `title_translations`; with `title` in the configuration, the next apply writes it once.",
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.MatchRoot("title"), path.MatchRoot("title_translations")),
					stringvalidator.LengthBetween(1, recurringTitleMaxLength),
				},
			},
			"title_translations": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
				MarkdownDescription: "Title per language as a `{locale = value}` map, e.g. " +
					"`{ de = \"Nächtliche Sicherung\", en = \"Nightly backup\" }`. " +
					"Exactly one of `title` and `title_translations` must be set.",
				Validators: []validator.Map{
					mapvalidator.SizeAtLeast(1),
					mapvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, recurringTitleMaxLength)),
				},
			},
			"description_translations": schema.MapAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Optional description per language. Removing it from the configuration removes it.",
				Validators: []validator.Map{
					mapvalidator.SizeAtLeast(1),
					mapvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, recurringDescMaxLength)),
				},
			},
			"timezone": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "IANA time zone the times of `schedule` are meant in, e.g. `Europe/Berlin`. " +
					"Windows keep their local time across daylight saving changes.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 64)},
			},
			"service_ids": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				MarkdownDescription: "Services the windows cover (public ids), at most 400. Left out, the " +
					"selection is not managed by Terraform and stays as set in the console.",
				Validators:    []validator.Set{setvalidator.SizeAtMost(recurringMaxServices), nonEmptyIDs},
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"tag_ids": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				MarkdownDescription: "Tags (public ids), at most 20. Every service carrying one of them when a " +
					"window starts is covered. Left out, the selection is not managed by Terraform.",
				Validators:    []validator.Set{setvalidator.SizeAtMost(recurringMaxTags), nonEmptyIDs},
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"statuspage_ids": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				MarkdownDescription: "Status pages that show the maintenance (public ids), at most 50. Without any, " +
					"it stays internal. Left out, the selection is not managed by Terraform.",
				Validators:    []validator.Set{setvalidator.SizeAtMost(recurringMaxStatuspages), nonEmptyIDs},
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"notify_start": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Notify the subscribers of the linked status pages when a window starts. Defaults to `false`.",
			},
			"notify_end": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Notify the subscribers of the linked status pages when a window ends. Defaults to `false`.",
			},
			"paused": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Pause by hand: no window starts until it is set back to `false`, and a window " +
					"running at that moment ends. Defaults to `false`. A pause caused by the plan limit is not " +
					"reflected here (see `paused_reason`) and cannot be lifted from Terraform.",
			},
			"paused_reason": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Why it is paused: `manual`, `plan_limit` (more recurring maintenances than the " +
					"plan allows; upgrade, and it resumes on its own) or `org_deleted`. Null when not paused.",
			},
			"state": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "`idle` (waiting for the next window), `running` (a window runs right now), " +
					"`paused` or `ended` (the schedule has no further window).",
			},
			"next_start_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Start of the running or the next window (RFC 3339). Null when paused or ended.",
			},
			"next_end_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "End of the running or the next window (RFC 3339). Null when paused or ended.",
			},
			"rule_text": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The schedule in plain words, for display.",
			},
		},
		Blocks: map[string]schema.Block{
			"schedule": schema.SingleNestedBlock{
				MarkdownDescription: "The rule. Required. Stored exactly as written, so it never drifts.",
				Validators:          []validator.Object{objectvalidator.IsRequired()},
				Attributes: map[string]schema.Attribute{
					"frequency": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "`daily`, `weekly` or `monthly`.",
						Validators:          []validator.String{stringvalidator.OneOf(frequencyDaily, frequencyWeekly, frequencyMonthly)},
					},
					"interval": schema.Int64Attribute{
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(1),
						MarkdownDescription: "Every n days, weeks or months, 1 to 99. Defaults to `1`.",
						Validators:          []validator.Int64{int64validator.Between(1, recurringMaxInterval)},
					},
					"weekdays": schema.SetAttribute{
						ElementType: types.Int64Type,
						Optional:    true,
						MarkdownDescription: "Weekly only, and required there: the days of the week, " +
							"`0` = Sunday, `1` = Monday … `6` = Saturday.",
						Validators: []validator.Set{
							setvalidator.SizeAtLeast(1),
							setvalidator.ValueInt64sAre(int64validator.Between(0, 6)),
						},
					},
					"anchor_date": schema.StringAttribute{
						Required: true,
						MarkdownDescription: "First day the rule applies (`YYYY-MM-DD`). With an interval above 1 it also " +
							"fixes which days, weeks or months count.",
						Validators: []validator.String{stringvalidator.RegexMatches(isoDatePattern, "must be a date as YYYY-MM-DD")},
					},
					"start_time": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "Local start time as `HH:MM` (24 hours) in `timezone`.",
						Validators:          []validator.String{stringvalidator.RegexMatches(clockTimePattern, "must be a time as HH:MM, e.g. 02:00")},
					},
					"duration_minutes": schema.Int64Attribute{
						Required:            true,
						MarkdownDescription: "Length of a window in minutes, 5 to 10080 (seven days), shorter than the gap between two windows.",
						Validators:          []validator.Int64{int64validator.Between(recurringMinDurationMinutes, recurringMaxDurationMinutes)},
					},
					"until_date": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Last day a window may start (`YYYY-MM-DD`, inclusive). Without it the rule repeats forever.",
						Validators:          []validator.String{stringvalidator.RegexMatches(isoDatePattern, "must be a date as YYYY-MM-DD")},
					},
				},
				Blocks: map[string]schema.Block{
					"month": schema.SingleNestedBlock{
						MarkdownDescription: "Monthly only, and required there. Either `mode = \"day\"` with `day`, or " +
							"`mode = \"nth_weekday\"` with `nth` and `weekday`.",
						Attributes: map[string]schema.Attribute{
							"mode": schema.StringAttribute{
								Optional:            true,
								MarkdownDescription: "`day` (a day of the month) or `nth_weekday` (e.g. the last Sunday).",
								Validators:          []validator.String{stringvalidator.OneOf(monthModeDay, monthModeNthWeekday)},
							},
							"day": schema.StringAttribute{
								Optional:            true,
								MarkdownDescription: "For `day`: `1` to `31`, or `last`. A month without that day has no window.",
								Validators:          []validator.String{stringvalidator.RegexMatches(monthDayPattern, "must be 1 to 31 or \"last\"")},
							},
							"nth": schema.StringAttribute{
								Optional:            true,
								MarkdownDescription: "For `nth_weekday`: `1` to `4`, or `last`.",
								Validators:          []validator.String{stringvalidator.RegexMatches(nthWeekdayPattern, "must be 1 to 4 or \"last\"")},
							},
							"weekday": schema.Int64Attribute{
								Optional:            true,
								MarkdownDescription: "For `nth_weekday`: `0` = Sunday … `6` = Saturday.",
								Validators:          []validator.Int64{int64validator.Between(0, 6)},
							},
						},
					},
				},
			},
		},
	}
}

func (r *recurringMaintenanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// ValidateConfig checks that the schedule carries exactly the keys of its
// frequency. The API refuses a key that does not belong (weekdays on a daily
// rule), and a missing one, so both are reported at plan time. Unknown values
// are left to the API.
func (r *recurringMaintenanceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config recurringMaintenanceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	validateRecurrenceSchedule(config.Schedule, &resp.Diagnostics)
}

func validateRecurrenceSchedule(s *recurrenceScheduleModel, diags *diag.Diagnostics) {
	if s == nil || s.Frequency.IsNull() || s.Frequency.IsUnknown() {
		return
	}

	root := path.Root("schedule")
	frequency := s.Frequency.ValueString()

	switch {
	case frequency == frequencyWeekly && s.Weekdays.IsNull():
		diags.AddAttributeError(root.AtName("weekdays"), "weekdays required",
			"A weekly schedule needs the days of the week, e.g. weekdays = [1, 4] for Monday and Thursday (0 = Sunday).")
	case frequency != frequencyWeekly && !s.Weekdays.IsNull():
		diags.AddAttributeError(root.AtName("weekdays"), "weekdays only for weekly",
			"weekdays belongs to a weekly schedule. Remove it, or set frequency = \"weekly\".")
	}

	switch {
	case frequency == frequencyMonthly && s.Month == nil:
		diags.AddAttributeError(root.AtName("month"), "month required",
			"A monthly schedule needs a month block, e.g. month { mode = \"day\"  day = 1 } or "+
				"month { mode = \"nth_weekday\"  nth = \"last\"  weekday = 0 }.")
	case frequency != frequencyMonthly && s.Month != nil:
		diags.AddAttributeError(root.AtName("month"), "month only for monthly",
			"The month block belongs to a monthly schedule. Remove it, or set frequency = \"monthly\".")
	}

	if frequency == frequencyMonthly && s.Month != nil {
		validateRecurrenceMonth(s.Month, root.AtName("month"), diags)
	}
}

func validateRecurrenceMonth(m *recurrenceMonthModel, at path.Path, diags *diag.Diagnostics) {
	if m.Mode.IsUnknown() {
		return
	}
	if m.Mode.IsNull() {
		diags.AddAttributeError(at.AtName("mode"), "mode required", "Set mode = \"day\" or mode = \"nth_weekday\".")
		return
	}

	if m.Mode.ValueString() == monthModeDay {
		if m.Day.IsNull() {
			diags.AddAttributeError(at.AtName("day"), "day required", "mode = \"day\" needs day, 1 to 31 or \"last\".")
		}
		if !m.Nth.IsNull() || !m.Weekday.IsNull() {
			diags.AddAttributeError(at, "nth and weekday only for nth_weekday",
				"mode = \"day\" takes only day. Remove nth and weekday, or set mode = \"nth_weekday\".")
		}
		return
	}

	if m.Nth.IsNull() || m.Weekday.IsNull() {
		diags.AddAttributeError(at, "nth and weekday required",
			"mode = \"nth_weekday\" needs nth (1 to 4 or \"last\") and weekday (0 = Sunday … 6 = Saturday).")
	}
	if !m.Day.IsNull() {
		diags.AddAttributeError(at.AtName("day"), "day only for mode day",
			"mode = \"nth_weekday\" takes nth and weekday. Remove day, or set mode = \"day\".")
	}
}

func (r *recurringMaintenanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan recurringMaintenanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := recurringMaintenanceCreateInput(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateRecurringMaintenance(ctx, in)
	if err != nil {
		addRecurringMaintenanceError(&resp.Diagnostics, "Creating the recurring maintenance failed", err, false)
		return
	}

	state, diags := recurringMaintenanceModelFromAPI(ctx, created, &plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	warnPausedByPlan(&resp.Diagnostics, created)
}

func (r *recurringMaintenanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state recurringMaintenanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote, err := r.client.GetRecurringMaintenance(ctx, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addRecurringMaintenanceError(&resp.Diagnostics, "Reading the recurring maintenance failed", err, false)
		return
	}

	newState, diags := recurringMaintenanceModelFromAPI(ctx, remote, &state)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *recurringMaintenanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state recurringMaintenanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := recurringMaintenanceUpdateInput(ctx, &plan, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateRecurringMaintenance(ctx, state.ID.ValueString(), in)
	if err != nil {
		addRecurringMaintenanceError(&resp.Diagnostics, "Updating the recurring maintenance failed", err, in.Paused != nil && !*in.Paused)
		return
	}

	newState, diags := recurringMaintenanceModelFromAPI(ctx, updated, &plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
	warnPausedByPlan(&resp.Diagnostics, updated)
}

func (r *recurringMaintenanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state recurringMaintenanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteRecurringMaintenance(ctx, state.ID.ValueString()); err != nil && !errors.Is(err, client.ErrNotFound) {
		addRecurringMaintenanceError(&resp.Diagnostics, "Deleting the recurring maintenance failed", err, false)
	}
}

func (r *recurringMaintenanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// recurringMaintenanceCreateInput sends every configured field. A set left out
// of the configuration is not sent and stays empty on the server.
func recurringMaintenanceCreateInput(ctx context.Context, m *recurringMaintenanceModel) (client.RecurringMaintenanceInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	in := client.RecurringMaintenanceInput{
		Title:       translatableInput(ctx, m.Title, m.TitleTranslations, &diags),
		Description: stringMap(ctx, m.DescriptionTranslations, &diags),
		Timezone:    optionalString(m.Timezone),
		Schedule:    scheduleInput(ctx, m.Schedule, &diags),
		NotifyStart: optionalBool(m.NotifyStart),
		NotifyEnd:   optionalBool(m.NotifyEnd),
		Paused:      optionalBool(m.Paused),
	}
	in.ServiceIDs = stringSet(ctx, m.ServiceIDs, &diags)
	in.TagIDs = stringSet(ctx, m.TagIDs, &diags)
	in.StatuspageIDs = stringSet(ctx, m.StatuspageIDs, &diags)

	return in, diags
}

// recurringMaintenanceUpdateInput sends only what differs from the state. The
// API changes only the fields it receives, so an apply that touches `paused`
// alone does not rewrite the rule, and a rule left as it is is not validated
// again against the exceptions saved in the console.
func recurringMaintenanceUpdateInput(ctx context.Context, plan, state *recurringMaintenanceModel) (client.RecurringMaintenanceInput, diag.Diagnostics) {
	var diags diag.Diagnostics
	var in client.RecurringMaintenanceInput

	if !plan.Title.Equal(state.Title) || !plan.TitleTranslations.Equal(state.TitleTranslations) {
		in.Title = translatableInput(ctx, plan.Title, plan.TitleTranslations, &diags)
	}
	if !plan.DescriptionTranslations.Equal(state.DescriptionTranslations) {
		in.Description = stringMap(ctx, plan.DescriptionTranslations, &diags)
		in.ClearDescription = plan.DescriptionTranslations.IsNull()
	}

	// The timezone is part of the rule: send both together, so the server
	// validates the windows of the new combination.
	if !plan.Timezone.Equal(state.Timezone) || !scheduleEqual(plan.Schedule, state.Schedule) {
		in.Timezone = optionalString(plan.Timezone)
		in.Schedule = scheduleInput(ctx, plan.Schedule, &diags)
	}

	if !plan.ServiceIDs.Equal(state.ServiceIDs) {
		in.ServiceIDs = stringSet(ctx, plan.ServiceIDs, &diags)
	}
	if !plan.TagIDs.Equal(state.TagIDs) {
		in.TagIDs = stringSet(ctx, plan.TagIDs, &diags)
	}
	if !plan.StatuspageIDs.Equal(state.StatuspageIDs) {
		in.StatuspageIDs = stringSet(ctx, plan.StatuspageIDs, &diags)
	}
	if !plan.NotifyStart.Equal(state.NotifyStart) {
		in.NotifyStart = optionalBool(plan.NotifyStart)
	}
	if !plan.NotifyEnd.Equal(state.NotifyEnd) {
		in.NotifyEnd = optionalBool(plan.NotifyEnd)
	}
	if !plan.Paused.Equal(state.Paused) {
		in.Paused = optionalBool(plan.Paused)
	}

	return in, diags
}

func scheduleInput(ctx context.Context, s *recurrenceScheduleModel, diags *diag.Diagnostics) *client.RecurrenceSchedule {
	if s == nil {
		return nil
	}

	out := &client.RecurrenceSchedule{
		V:               client.RecurrenceScheduleVersion,
		Frequency:       s.Frequency.ValueString(),
		Interval:        s.Interval.ValueInt64(),
		AnchorDate:      s.AnchorDate.ValueString(),
		StartTime:       s.StartTime.ValueString(),
		DurationMinutes: s.DurationMinutes.ValueInt64(),
		UntilDate:       optionalString(s.UntilDate),
	}

	if !s.Weekdays.IsNull() && !s.Weekdays.IsUnknown() {
		days := []int64{}
		diags.Append(s.Weekdays.ElementsAs(ctx, &days, false)...)
		out.Weekdays = days
	}

	if s.Month != nil {
		month := &client.RecurrenceMonth{Mode: s.Month.Mode.ValueString()}
		if v := optionalString(s.Month.Day); v != nil {
			day := client.NumOrLast(*v)
			month.Day = &day
		}
		if v := optionalString(s.Month.Nth); v != nil {
			nth := client.NumOrLast(*v)
			month.Nth = &nth
		}
		if !s.Month.Weekday.IsNull() && !s.Month.Weekday.IsUnknown() {
			month.Weekday = s.Month.Weekday.ValueInt64Pointer()
		}
		out.Month = month
	}

	return out
}

// scheduleEqual compares two schedule blocks attribute by attribute; the set
// of weekdays compares regardless of order.
func scheduleEqual(a, b *recurrenceScheduleModel) bool {
	if a == nil || b == nil {
		return a == b
	}

	same := a.Frequency.Equal(b.Frequency) &&
		a.Interval.Equal(b.Interval) &&
		a.Weekdays.Equal(b.Weekdays) &&
		a.AnchorDate.Equal(b.AnchorDate) &&
		a.StartTime.Equal(b.StartTime) &&
		a.DurationMinutes.Equal(b.DurationMinutes) &&
		a.UntilDate.Equal(b.UntilDate)
	if !same {
		return false
	}

	if a.Month == nil || b.Month == nil {
		return a.Month == b.Month
	}
	return a.Month.Mode.Equal(b.Month.Mode) &&
		a.Month.Day.Equal(b.Month.Day) &&
		a.Month.Nth.Equal(b.Month.Nth) &&
		a.Month.Weekday.Equal(b.Month.Weekday)
}

// recurringMaintenanceModelFromAPI maps the API echo onto the model. The API
// returns every writable field exactly as stored, so the state is the server's
// view; only the shape of the title (see translatableFromAPI) and `paused`
// (see pausedFromAPI) need the prior value.
func recurringMaintenanceModelFromAPI(ctx context.Context, remote *client.RecurringMaintenance, prior *recurringMaintenanceModel) (*recurringMaintenanceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	m := &recurringMaintenanceModel{
		ID:           types.StringValue(remote.ID),
		Timezone:     types.StringValue(remote.Timezone),
		Schedule:     scheduleFromAPI(ctx, remote.Schedule, &diags),
		NotifyStart:  types.BoolValue(remote.NotifyStart),
		NotifyEnd:    types.BoolValue(remote.NotifyEnd),
		Paused:       pausedFromAPI(remote, prior.Paused),
		PausedReason: types.StringPointerValue(remote.PausedReason),
		State:        types.StringValue(remote.State),
		NextStartAt:  types.StringPointerValue(remote.NextStartAt),
		NextEndAt:    types.StringPointerValue(remote.NextEndAt),
		RuleText:     types.StringPointerValue(remote.RuleText),
	}

	m.Title, m.TitleTranslations = recurringTitleFromAPI(ctx, remote, prior.Title, &diags)

	// No description reads as null (the API answers null, never {}).
	m.DescriptionTranslations = types.MapNull(types.StringType)
	if len(remote.DescriptionTranslations) > 0 {
		desc, d := types.MapValueFrom(ctx, types.StringType, remote.DescriptionTranslations)
		diags.Append(d...)
		m.DescriptionTranslations = desc
	}

	m.ServiceIDs = idSet(ctx, remote.ServiceIDs, &diags)
	m.TagIDs = idSet(ctx, remote.TagIDs, &diags)
	m.StatuspageIDs = idSet(ctx, remote.StatuspageIDs, &diags)

	return m, diags
}

// recurringTitleFromAPI keeps the title in the shape it was written: a
// configured plain `title` reads back as the string, anything else as the full
// map. An import has no prior shape and reads the map, which loses nothing; a
// configuration that uses `title` then rewrites it once on the next apply.
func recurringTitleFromAPI(ctx context.Context, remote *client.RecurringMaintenance, priorTitle types.String, diags *diag.Diagnostics) (types.String, types.Map) {
	if !priorTitle.IsNull() && !priorTitle.IsUnknown() {
		return types.StringValue(remote.Title), types.MapNull(types.StringType)
	}

	m, d := types.MapValueFrom(ctx, types.StringType, nonNilMap(remote.TitleTranslations))
	diags.Append(d...)

	return types.StringNull(), m
}

// pausedFromAPI reads `paused`, which means the pause by hand only. While the
// maintenance is paused for another reason (the plan limit, the organization
// being deleted) it is paused regardless of this flag, and the API ignores a
// change to it, so the prior value is kept: neither `paused = true` nor
// `paused = false` in the configuration shows a diff. Once that pause is lifted
// the flag reads from the server again, and a configured `paused = true` is
// applied as a pause by hand.
func pausedFromAPI(remote *client.RecurringMaintenance, prior types.Bool) types.Bool {
	if remote.Paused {
		return types.BoolValue(true)
	}

	pausedOtherwise := remote.PausedReason != nil && *remote.PausedReason != pausedReasonManual
	if pausedOtherwise && !prior.IsNull() && !prior.IsUnknown() {
		return prior
	}

	return types.BoolValue(false)
}

func scheduleFromAPI(ctx context.Context, s client.RecurrenceSchedule, diags *diag.Diagnostics) *recurrenceScheduleModel {
	m := &recurrenceScheduleModel{
		Frequency:       types.StringValue(s.Frequency),
		Interval:        types.Int64Value(s.Interval),
		Weekdays:        types.SetNull(types.Int64Type),
		AnchorDate:      types.StringValue(s.AnchorDate),
		StartTime:       types.StringValue(s.StartTime),
		DurationMinutes: types.Int64Value(s.DurationMinutes),
		UntilDate:       types.StringPointerValue(s.UntilDate),
	}

	if s.Weekdays != nil {
		days, d := types.SetValueFrom(ctx, types.Int64Type, s.Weekdays)
		diags.Append(d...)
		m.Weekdays = days
	}

	if s.Month != nil {
		m.Month = &recurrenceMonthModel{
			Mode:    types.StringValue(s.Month.Mode),
			Day:     numOrLastValue(s.Month.Day),
			Nth:     numOrLastValue(s.Month.Nth),
			Weekday: types.Int64PointerValue(s.Month.Weekday),
		}
	}

	return m
}

func numOrLastValue(v *client.NumOrLast) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(string(*v))
}

// warnPausedByPlan tells the practitioner after an apply that the maintenance
// is paused for a reason Terraform cannot lift.
func warnPausedByPlan(diags *diag.Diagnostics, remote *client.RecurringMaintenance) {
	if remote.PausedReason == nil || *remote.PausedReason != "plan_limit" {
		return
	}
	diags.AddAttributeWarning(path.Root("paused_reason"), "Paused by the plan limit",
		"This recurring maintenance is paused because the organization has more recurring maintenances "+
			"than its plan allows. No window starts until the plan is upgraded (it then resumes on its own) "+
			"or another recurring maintenance is deleted and this one is resumed in the console.")
}

func stringMap(ctx context.Context, v types.Map, diags *diag.Diagnostics) map[string]string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	m := map[string]string{}
	diags.Append(v.ElementsAs(ctx, &m, false)...)
	return m
}

// stringSet returns a pointer to the set's elements: nil for a null or unknown
// set (key omitted), an empty list for an empty set (sent as [] to clear).
func stringSet(ctx context.Context, v types.Set, diags *diag.Diagnostics) *[]string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	list := []string{}
	diags.Append(v.ElementsAs(ctx, &list, false)...)
	return &list
}

func idSet(ctx context.Context, ids []string, diags *diag.Diagnostics) types.Set {
	elems := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		elems = append(elems, types.StringValue(id))
	}
	set, d := types.SetValue(types.StringType, elems)
	diags.Append(d...)
	return set
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func optionalBool(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueBoolPointer()
}
