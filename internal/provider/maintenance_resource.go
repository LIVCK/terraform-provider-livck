package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

var (
	_ resource.Resource                   = (*maintenanceResource)(nil)
	_ resource.ResourceWithConfigure      = (*maintenanceResource)(nil)
	_ resource.ResourceWithImportState    = (*maintenanceResource)(nil)
	_ resource.ResourceWithValidateConfig = (*maintenanceResource)(nil)
)

type maintenanceResource struct {
	client *client.Client
}

type maintenanceModel struct {
	ID                types.String      `tfsdk:"id"`
	Title             types.String      `tfsdk:"title"`
	TitleTranslations types.Map         `tfsdk:"title_translations"`
	Type              types.String      `tfsdk:"type"`
	ScheduledStart    timetypes.RFC3339 `tfsdk:"scheduled_start"`
	ScheduledEnd      timetypes.RFC3339 `tfsdk:"scheduled_end"`
	ServiceIDs        types.Set         `tfsdk:"service_ids"`
	StatuspageIDs     types.Set         `tfsdk:"statuspage_ids"`
	AutoStart         types.Bool        `tfsdk:"auto_start"`
	AutoComplete      types.Bool        `tfsdk:"auto_complete"`
	Notify24h         types.Bool        `tfsdk:"notify_24h"`
	Notify1h          types.Bool        `tfsdk:"notify_1h"`
	NotifyStart       types.Bool        `tfsdk:"notify_start"`
	NotifyComplete    types.Bool        `tfsdk:"notify_complete"`
	Status            types.String      `tfsdk:"status"`
}

func NewMaintenanceResource() resource.Resource { return &maintenanceResource{} }

func (r *maintenanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_maintenance"
}

func (r *maintenanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A scheduled maintenance window, announced on the linked status pages. " +
			"Lifecycle transitions (start/complete/cancel) are runtime events and stay outside " +
			"of Terraform. The window starts and completes automatically (`auto_start`/`auto_complete`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"title": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Single-language title. Exactly one of `title` and `title_translations` must be set.",
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.MatchRoot("title"), path.MatchRoot("title_translations")),
				},
			},
			"title_translations": schema.MapAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Multilingual title as a `{locale = value}` map.",
			},
			"type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`planned` (announced, SLA-excluded by default) or `emergency` (unannounced, counts against the SLA). Defaults to `planned`.",
				Validators:          []validator.String{stringvalidator.OneOf("planned", "emergency")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"scheduled_start": schema.StringAttribute{
				CustomType:          timetypes.RFC3339Type{},
				Required:            true,
				MarkdownDescription: "RFC3339 timestamp. Compared semantically, so timezone formatting differences do not produce diffs.",
			},
			"scheduled_end": schema.StringAttribute{
				CustomType: timetypes.RFC3339Type{},
				Optional:   true,
				MarkdownDescription: "RFC3339 timestamp; must be after `scheduled_start`. Omit it for an " +
					"open-ended window (\"until further notice\"): status pages then show no countdown and " +
					"the window is completed manually, and `auto_complete` cannot be used without an end.",
			},
			"service_ids": schema.SetAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Affected services (public ids).",
			},
			"statuspage_ids": schema.SetAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Status pages announcing this window (public ids).",
			},
			"auto_start": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Start the window automatically at scheduled_start (server default: true).",
			},
			"auto_complete": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Complete the window automatically at scheduled_end (server default: false). " +
					"Requires `scheduled_end`; an open-ended window is completed manually.",
			},
			// The notify flags are write-only on the API (no read echo yet): state
			// equals configuration/default, never the server. Defaults mirror the
			// server defaults so an omitted flag cannot drift.
			"notify_24h": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
			},
			"notify_1h": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
			},
			"notify_start": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
			},
			"notify_complete": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Lifecycle status (scheduled/in_progress/completed/cancelled, read-only).",
			},
		},
	}
}

func (r *maintenanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// ValidateConfig rejects `auto_complete` on an open-ended window at plan time.
// auto_complete fires on `scheduled_end <= now()`, a condition a null end can
// never meet - the flag would be a permanent no-op. The API refuses the combo on
// create; catching it here reports the problem before an apply is attempted and
// covers the update path too.
func (r *maintenanceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config maintenanceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Unknown values (interpolations resolved at apply time) yield false/null
	// here; those cases fall through to the API's own validation.
	if config.AutoComplete.ValueBool() && config.ScheduledEnd.IsNull() {
		resp.Diagnostics.AddAttributeError(
			path.Root("auto_complete"),
			"auto_complete requires scheduled_end",
			"An open-ended maintenance window (scheduled_end omitted) has no end to complete at, so "+
				"auto_complete could never fire. Set scheduled_end, or leave auto_complete unset and "+
				"complete the window manually.",
		)
	}
}

func (r *maintenanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan maintenanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := maintenanceInputFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateMaintenance(ctx, in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating the maintenance failed", err)
		return
	}

	state, diags := maintenanceModelFromAPI(ctx, created, &plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *maintenanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state maintenanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote, err := r.client.GetMaintenance(ctx, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading the maintenance failed", err)
		return
	}

	newState, diags := maintenanceModelFromAPI(ctx, remote, &state)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *maintenanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state maintenanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := maintenanceInputFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateMaintenance(ctx, state.ID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Updating the maintenance failed", err)
		return
	}

	newState, diags := maintenanceModelFromAPI(ctx, updated, &plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *maintenanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state maintenanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteMaintenance(ctx, state.ID.ValueString()); err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Deleting the maintenance failed", err)
	}
}

func (r *maintenanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func maintenanceInputFromModel(ctx context.Context, m *maintenanceModel) (client.MaintenanceInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	in := client.MaintenanceInput{
		Title:          translatableInput(ctx, m.Title, m.TitleTranslations, &diags),
		Type:           optionalString(m.Type),
		ScheduledStart: m.ScheduledStart.ValueString(),
	}

	// An unset end stays nil and is serialised as an explicit null -> open-ended
	// window. Terraform hands us the full desired state on both create and
	// update, so always sending the key also makes "remove scheduled_end from
	// the config" actually clear the end server-side.
	if !m.ScheduledEnd.IsNull() && !m.ScheduledEnd.IsUnknown() {
		in.ScheduledEnd = m.ScheduledEnd.ValueStringPointer()
	}

	// A non-null Set (even empty) is sent as an explicit list so clearing to []
	// reaches the API; a null Set stays unmanaged (key omitted). Bare []string
	// + omitempty could not distinguish these - hence the pointer.
	if !m.ServiceIDs.IsNull() && !m.ServiceIDs.IsUnknown() {
		list := []string{}
		diags.Append(m.ServiceIDs.ElementsAs(ctx, &list, false)...)
		in.ServiceIDs = &list
	}
	if !m.StatuspageIDs.IsNull() && !m.StatuspageIDs.IsUnknown() {
		list := []string{}
		diags.Append(m.StatuspageIDs.ElementsAs(ctx, &list, false)...)
		in.StatuspageIDs = &list
	}
	if !m.AutoStart.IsNull() && !m.AutoStart.IsUnknown() {
		in.AutoStart = m.AutoStart.ValueBoolPointer()
	}
	if !m.AutoComplete.IsNull() && !m.AutoComplete.IsUnknown() {
		in.AutoComplete = m.AutoComplete.ValueBoolPointer()
	}
	in.Notify24h = m.Notify24h.ValueBoolPointer()
	in.Notify1h = m.Notify1h.ValueBoolPointer()
	in.NotifyStart = m.NotifyStart.ValueBoolPointer()
	in.NotifyComplete = m.NotifyComplete.ValueBoolPointer()

	return in, diags
}

// maintenanceModelFromAPI maps the API echo onto the model. The notify flags
// keep the prior (plan/state) values - they have no read echo on the API.
func maintenanceModelFromAPI(ctx context.Context, remote *client.Maintenance, prior *maintenanceModel) (*maintenanceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	start, d := timetypes.NewRFC3339Value(remote.ScheduledStart)
	diags.Append(d...)

	// A null echo means the window is open-ended: keep the attribute null instead
	// of parsing "" as a timestamp. sameInstantOr below then round-trips null ->
	// null, and a server-side end appearing on a window Terraform manages as
	// open-ended still surfaces as real drift.
	end := timetypes.NewRFC3339Null()
	if remote.ScheduledEnd != nil {
		end, d = timetypes.NewRFC3339Value(*remote.ScheduledEnd)
		diags.Append(d...)
	}

	m := &maintenanceModel{
		ID: types.StringValue(remote.ID),
		// Keep the practitioner's timestamp FORMAT when it denotes the same instant
		// as the server echo. The API answers in UTC ("...T22:00:00+00:00") while a
		// config may legitimately use a local offset ("...T23:00:00+01:00"); Terraform
		// Core's post-apply consistency check compares values EXACTLY (it does not
		// know about RFC3339 semantic equality), so echoing the server's format for
		// a semantically identical instant would abort the apply.
		ScheduledStart: sameInstantOr(prior.ScheduledStart, start),
		ScheduledEnd:   sameInstantOr(prior.ScheduledEnd, end),
		AutoStart:      types.BoolValue(remote.AutoStart),
		AutoComplete:   types.BoolValue(remote.AutoComplete),
		Notify24h:      prior.Notify24h,
		Notify1h:       prior.Notify1h,
		NotifyStart:    prior.NotifyStart,
		NotifyComplete: prior.NotifyComplete,
		Status:         types.StringValue(remote.Status),
	}

	m.Title, m.TitleTranslations = translatableFromAPI(ctx, remote.Title, remote.TitleTranslations, prior.TitleTranslations, &diags)
	m.Type = types.StringValue(remote.Type)

	// service/statuspage linkage: only track it when the config manages it -
	// a null set stays null (the server may add auto-detected services).
	if !prior.ServiceIDs.IsNull() {
		ids := make([]string, 0, len(remote.Services))
		for _, s := range remote.Services {
			ids = append(ids, s.ID)
		}
		set, d := types.SetValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		m.ServiceIDs = set
	} else {
		m.ServiceIDs = types.SetNull(types.StringType)
	}

	if !prior.StatuspageIDs.IsNull() {
		ids := make([]string, 0, len(remote.Statuspages))
		for _, sp := range remote.Statuspages {
			ids = append(ids, sp.ID)
		}
		set, d := types.SetValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		m.StatuspageIDs = set
	} else {
		m.StatuspageIDs = types.SetNull(types.StringType)
	}

	return m, diags
}

func optionalString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueStringPointer()
}

// sameInstantOr returns the prior (config-formatted) timestamp when it denotes
// the same instant as the freshly-read one, otherwise the new value. This keeps
// state stable across timezone-offset formatting differences without hiding a
// real reschedule.
func sameInstantOr(prior, fresh timetypes.RFC3339) timetypes.RFC3339 {
	if prior.IsNull() || prior.IsUnknown() || fresh.IsNull() || fresh.IsUnknown() {
		return fresh
	}

	pt, dp := prior.ValueRFC3339Time()
	ft, df := fresh.ValueRFC3339Time()
	if dp.HasError() || df.HasError() {
		return fresh
	}

	if pt.Equal(ft) {
		return prior
	}
	return fresh
}
