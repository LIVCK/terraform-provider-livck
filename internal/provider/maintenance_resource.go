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
	_ resource.Resource                = (*maintenanceResource)(nil)
	_ resource.ResourceWithConfigure   = (*maintenanceResource)(nil)
	_ resource.ResourceWithImportState = (*maintenanceResource)(nil)
)

type maintenanceResource struct {
	client *client.Client
}

type maintenanceModel struct {
	ID                types.String      `tfsdk:"id"`
	Title             types.String      `tfsdk:"title"`
	TitleTranslations types.Map         `tfsdk:"title_translations"`
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
			"of Terraform — the window starts/completes automatically (`auto_start`/`auto_complete`).",
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
			"scheduled_start": schema.StringAttribute{
				CustomType:          timetypes.RFC3339Type{},
				Required:            true,
				MarkdownDescription: "RFC3339 timestamp. Compared semantically — timezone formatting differences do not produce diffs.",
			},
			"scheduled_end": schema.StringAttribute{
				CustomType:          timetypes.RFC3339Type{},
				Required:            true,
				MarkdownDescription: "RFC3339 timestamp; must be after `scheduled_start`.",
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
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Complete the window automatically at scheduled_end (server default: false).",
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
		ScheduledStart: m.ScheduledStart.ValueString(),
		ScheduledEnd:   m.ScheduledEnd.ValueString(),
	}

	if !m.ServiceIDs.IsNull() && !m.ServiceIDs.IsUnknown() {
		diags.Append(m.ServiceIDs.ElementsAs(ctx, &in.ServiceIDs, false)...)
	}
	if !m.StatuspageIDs.IsNull() && !m.StatuspageIDs.IsUnknown() {
		diags.Append(m.StatuspageIDs.ElementsAs(ctx, &in.StatuspageIDs, false)...)
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
// keep the prior (plan/state) values — they have no read echo on the API.
func maintenanceModelFromAPI(ctx context.Context, remote *client.Maintenance, prior *maintenanceModel) (*maintenanceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	start, d := timetypes.NewRFC3339Value(remote.ScheduledStart)
	diags.Append(d...)
	end, d := timetypes.NewRFC3339Value(remote.ScheduledEnd)
	diags.Append(d...)

	m := &maintenanceModel{
		ID:             types.StringValue(remote.ID),
		ScheduledStart: start,
		ScheduledEnd:   end,
		AutoStart:      types.BoolValue(remote.AutoStart),
		AutoComplete:   types.BoolValue(remote.AutoComplete),
		Notify24h:      prior.Notify24h,
		Notify1h:       prior.Notify1h,
		NotifyStart:    prior.NotifyStart,
		NotifyComplete: prior.NotifyComplete,
		Status:         types.StringValue(remote.Status),
	}

	m.Title, m.TitleTranslations = translatableFromAPI(ctx, remote.Title, remote.TitleTranslations, prior.TitleTranslations, &diags)

	// service/statuspage linkage: only track it when the config manages it —
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
