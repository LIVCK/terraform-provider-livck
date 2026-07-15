package provider

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
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
	_ resource.Resource                = (*serviceResource)(nil)
	_ resource.ResourceWithConfigure   = (*serviceResource)(nil)
	_ resource.ResourceWithImportState = (*serviceResource)(nil)
)

type serviceResource struct {
	client *client.Client
}

type serviceModel struct {
	ID        types.String   `tfsdk:"id"`
	Name      types.String   `tfsdk:"name"`
	CheckType types.String   `tfsdk:"check_type"`
	Target    types.String   `tfsdk:"target"`
	Paused    types.Bool     `tfsdk:"paused"`
	Status    types.String   `tfsdk:"status"`
	Settings  *settingsModel `tfsdk:"settings"`
}

type settingsModel struct {
	IntervalSeconds types.Int64          `tfsdk:"interval_seconds"`
	TimeoutSeconds  types.Int64          `tfsdk:"timeout_seconds"`
	Retries         types.Int64          `tfsdk:"retries"`
	AssignedProbes  types.Set            `tfsdk:"assigned_probes"`
	Config          jsontypes.Normalized `tfsdk:"config"`
}

func NewServiceResource() resource.Resource { return &serviceResource{} }

func (r *serviceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service"
}

func (r *serviceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A monitored service (HTTP, TCP, DNS, ICMP, SSL or manual). " +
			"Interval bounds, timeout/retry limits and per-check-type config fields are " +
			"validated server-side against your plan and the check type — fetch the live " +
			"catalog via the `livck_check_types` data source.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Stable public identifier (NanoID). Used for `terraform import`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true,
			},
			"check_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "One of `http`, `tcp`, `dns`, `icmp`, `ssl`, `manual`. Immutable — changing it replaces the service.",
				Validators: []validator.String{
					stringvalidator.OneOf("http", "tcp", "dns", "icmp", "ssl", "manual"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"target": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Check target (URL for http, `host:port` for tcp, hostname for dns/icmp/ssl). Required for every check type except `manual`.",
			},
			"paused": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Pause/resume monitoring declaratively. Note: the platform " +
					"may pause services server-side on plan downgrades; the next apply resumes " +
					"them (or fails at the plan limit).",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Live monitoring status (runtime state, read-only).",
			},
			"settings": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Monitoring configuration. Omitted entirely, the service is " +
					"created unconfigured and no checks run.",
				Attributes: map[string]schema.Attribute{
					"interval_seconds": schema.Int64Attribute{
						Optional:            true,
						Computed:            true,
						MarkdownDescription: "Check interval. Floor/ceiling depend on your plan and the check type (e.g. SSL checks run at most hourly).",
					},
					"timeout_seconds": schema.Int64Attribute{
						Optional: true,
						Computed: true,
					},
					"retries": schema.Int64Attribute{
						Optional: true,
						Computed: true,
					},
					"assigned_probes": schema.SetAttribute{
						ElementType:         types.StringType,
						Optional:            true,
						MarkdownDescription: "Probe location codes (see the `livck_probes` data source). Omitted, the organization's default locations apply.",
					},
					"config": schema.StringAttribute{
						CustomType: jsontypes.NormalizedType{},
						Optional:   true,
						Computed:   true,
						Sensitive:  true,
						MarkdownDescription: "Check-type specific config as JSON (fields, conditions, headers, auth). " +
							"Secret values (header values, auth credentials) are write-only: the API returns " +
							"a keep-sentinel which the provider transparently resolves against the state, " +
							"so plans stay clean. After `terraform import`, secrets cannot be recovered and " +
							"must be re-applied.",
					},
				},
			},
		},
	}
}

func (r *serviceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *serviceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serviceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.ServiceInput{
		Name:      plan.Name.ValueString(),
		CheckType: plan.CheckType.ValueString(),
	}
	if !plan.Target.IsNull() {
		in.Target = plan.Target.ValueStringPointer()
	}

	settingsIn, diags := settingsInputFromModel(ctx, plan.Settings)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	in.Settings = settingsIn

	created, err := r.client.CreateService(ctx, in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating the service failed", err)
		return
	}

	if plan.Paused.ValueBool() {
		if created, err = r.client.PauseService(ctx, created.ID); err != nil {
			addAPIError(&resp.Diagnostics, "Pausing the service after creation failed", err)
			return
		}
	}

	state, diags := serviceModelFromAPI(ctx, created, &plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *serviceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serviceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote, err := r.client.GetService(ctx, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading the service failed", err)
		return
	}

	newState, diags := serviceModelFromAPI(ctx, remote, &state)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *serviceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state serviceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.ServiceInput{Name: plan.Name.ValueString()}
	if !plan.Target.IsNull() {
		in.Target = plan.Target.ValueStringPointer()
	}

	settingsIn, diags := settingsInputFromModel(ctx, plan.Settings)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	in.Settings = settingsIn

	updated, err := r.client.UpdateService(ctx, state.ID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Updating the service failed", err)
		return
	}

	if plan.Paused.ValueBool() != updated.IsPaused {
		if plan.Paused.ValueBool() {
			updated, err = r.client.PauseService(ctx, state.ID.ValueString())
		} else {
			updated, err = r.client.ResumeService(ctx, state.ID.ValueString())
		}
		if err != nil {
			addAPIError(&resp.Diagnostics, "Changing the pause state failed", err)
			return
		}
	}

	newState, diags := serviceModelFromAPI(ctx, updated, &plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *serviceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serviceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteService(ctx, state.ID.ValueString()); err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Deleting the service failed", err)
	}
}

func (r *serviceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// settingsInputFromModel converts the nested settings block to the API write shape.
func settingsInputFromModel(ctx context.Context, m *settingsModel) (*client.ServiceSettingsInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	if m == nil {
		return nil, diags
	}

	in := &client.ServiceSettingsInput{}

	if !m.IntervalSeconds.IsNull() && !m.IntervalSeconds.IsUnknown() {
		in.IntervalSeconds = m.IntervalSeconds.ValueInt64Pointer()
	}
	if !m.TimeoutSeconds.IsNull() && !m.TimeoutSeconds.IsUnknown() {
		in.TimeoutSeconds = m.TimeoutSeconds.ValueInt64Pointer()
	}
	if !m.Retries.IsNull() && !m.Retries.IsUnknown() {
		in.Retries = m.Retries.ValueInt64Pointer()
	}
	if !m.AssignedProbes.IsNull() && !m.AssignedProbes.IsUnknown() {
		diags.Append(m.AssignedProbes.ElementsAs(ctx, &in.AssignedProbes, false)...)
	}
	if !m.Config.IsNull() && !m.Config.IsUnknown() {
		in.Config = json.RawMessage(m.Config.ValueString())
	}

	return in, diags
}

// serviceModelFromAPI maps an API service onto the Terraform model. `prior`
// carries the previous state/plan: it decides whether the settings block is
// present at all and supplies the secret values that the API masks with the
// keep-sentinel (see client.MergeSecrets).
func serviceModelFromAPI(ctx context.Context, remote *client.Service, prior *serviceModel) (*serviceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	m := &serviceModel{
		ID:        types.StringValue(remote.ID),
		Name:      types.StringValue(remote.Name),
		CheckType: types.StringValue(remote.CheckType),
		Target:    types.StringPointerValue(remote.Target),
		Paused:    types.BoolValue(remote.IsPaused),
		Status:    types.StringValue(remote.Status),
	}

	if remote.Settings == nil {
		m.Settings = nil
		return m, diags
	}

	s := &settingsModel{
		IntervalSeconds: types.Int64Value(remote.Settings.IntervalSeconds),
		TimeoutSeconds:  types.Int64Value(remote.Settings.TimeoutSeconds),
		Retries:         types.Int64Value(remote.Settings.Retries),
	}

	if remote.Settings.AssignedProbes == nil {
		s.AssignedProbes = types.SetNull(types.StringType)
	} else {
		set, d := types.SetValueFrom(ctx, types.StringType, remote.Settings.AssignedProbes)
		diags.Append(d...)
		s.AssignedProbes = set
	}

	var priorConfig json.RawMessage
	if prior != nil && prior.Settings != nil && !prior.Settings.Config.IsNull() && !prior.Settings.Config.IsUnknown() {
		priorConfig = json.RawMessage(prior.Settings.Config.ValueString())
	}

	if len(remote.Settings.Config) == 0 || string(remote.Settings.Config) == "null" {
		s.Config = jsontypes.NewNormalizedNull()
	} else {
		merged, err := client.MergeSecrets(remote.Settings.Config, priorConfig)
		if err != nil {
			diags.AddError("Decoding the service config failed", err.Error())
			return m, diags
		}
		s.Config = jsontypes.NewNormalizedValue(string(merged))
	}

	m.Settings = s

	return m, diags
}
