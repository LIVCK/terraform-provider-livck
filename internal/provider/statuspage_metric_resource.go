package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = (*statuspageMetricResource)(nil)
	_ resource.ResourceWithConfigure   = (*statuspageMetricResource)(nil)
	_ resource.ResourceWithImportState = (*statuspageMetricResource)(nil)
)

type statuspageMetricResource struct {
	client *client.Client
}

type metricModel struct {
	ID           types.String `tfsdk:"id"`
	StatuspageID types.String `tfsdk:"statuspage_id"`
	Name         types.String `tfsdk:"name"`
	DisplayType  types.String `tfsdk:"display_type"`
	Suffix       types.String `tfsdk:"suffix"`
	ComponentID  types.String `tfsdk:"component_id"`
	IsVisible    types.Bool   `tfsdk:"is_visible"`
	DisplayOrder types.Int64  `tfsdk:"display_order"`
}

func NewStatuspageMetricResource() resource.Resource { return &statuspageMetricResource{} }

func (r *statuspageMetricResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_statuspage_metric"
}

func (r *statuspageMetricResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A metric chart on a status page — standalone or placed under a " +
			"component (`component_id`). Its data comes from one or more " +
			"`livck_statuspage_metric_series`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"statuspage_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true,
			},
			"display_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "`line_chart` or `uptime_bars`.",
				Validators: []validator.String{
					stringvalidator.OneOf("line_chart", "uptime_bars"),
				},
			},
			"suffix": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Unit suffix rendered after values (e.g. `ms`). The server stores an empty string when omitted.",
			},
			"component_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Public id of a component on the same page. Omitted, the chart renders standalone.",
			},
			"is_visible": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"display_order": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Server-assigned position (read-only in v0).",
			},
		},
	}
}

func (r *statuspageMetricResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *statuspageMetricResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan metricModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.MetricInput{
		Name:        plan.Name.ValueString(),
		DisplayType: plan.DisplayType.ValueString(),
		IsVisible:   plan.IsVisible.ValueBoolPointer(),
		ComponentID: plan.ComponentID.ValueStringPointer(),
	}
	if !plan.Suffix.IsNull() && !plan.Suffix.IsUnknown() {
		in.Suffix = plan.Suffix.ValueStringPointer()
	}

	created, err := r.client.CreateMetric(ctx, plan.StatuspageID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating the metric failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, metricModelFromAPI(created, plan.StatuspageID.ValueString()))...)
}

func (r *statuspageMetricResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state metricModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote, err := r.client.GetMetric(ctx, state.StatuspageID.ValueString(), state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading the metric failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, metricModelFromAPI(remote, state.StatuspageID.ValueString()))...)
}

func (r *statuspageMetricResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state metricModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.MetricInput{
		Name:        plan.Name.ValueString(),
		DisplayType: plan.DisplayType.ValueString(),
		IsVisible:   plan.IsVisible.ValueBoolPointer(),
		ComponentID: plan.ComponentID.ValueStringPointer(),
	}
	if !plan.Suffix.IsNull() && !plan.Suffix.IsUnknown() {
		in.Suffix = plan.Suffix.ValueStringPointer()
	}

	updated, err := r.client.UpdateMetric(ctx, state.StatuspageID.ValueString(), state.ID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Updating the metric failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, metricModelFromAPI(updated, state.StatuspageID.ValueString()))...)
}

func (r *statuspageMetricResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state metricModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteMetric(ctx, state.StatuspageID.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Deleting the metric failed", err)
	}
}

// ImportState expects the composite id `<statuspage_id>/<metric_id>`.
func (r *statuspageMetricResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import id", fmt.Sprintf("Expected <statuspage_id>/<metric_id>, got %q.", req.ID))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("statuspage_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

func metricModelFromAPI(remote *client.Metric, statuspageID string) *metricModel {
	return &metricModel{
		ID:           types.StringValue(remote.ID),
		StatuspageID: types.StringValue(statuspageID),
		Name:         types.StringValue(remote.Name),
		DisplayType:  types.StringValue(remote.DisplayType),
		Suffix:       types.StringValue(remote.Suffix),
		ComponentID:  types.StringPointerValue(remote.ComponentID),
		IsVisible:    types.BoolValue(remote.IsVisible),
		DisplayOrder: types.Int64Value(remote.DisplayOrder),
	}
}
