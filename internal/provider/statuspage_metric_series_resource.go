package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

var (
	_ resource.Resource                = (*statuspageMetricSeriesResource)(nil)
	_ resource.ResourceWithConfigure   = (*statuspageMetricSeriesResource)(nil)
	_ resource.ResourceWithImportState = (*statuspageMetricSeriesResource)(nil)
)

type statuspageMetricSeriesResource struct {
	client *client.Client
}

type seriesModel struct {
	ID           types.String `tfsdk:"id"`
	StatuspageID types.String `tfsdk:"statuspage_id"`
	MetricID     types.String `tfsdk:"metric_id"`
	Name         types.String `tfsdk:"name"`
	ServiceID    types.String `tfsdk:"service_id"`
	MetricType   types.String `tfsdk:"metric_type"`
	Color        types.String `tfsdk:"color"`
	DisplayOrder types.Int64  `tfsdk:"display_order"`
}

func NewStatuspageMetricSeriesResource() resource.Resource { return &statuspageMetricSeriesResource{} }

func (r *statuspageMetricSeriesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_statuspage_metric_series"
}

func (r *statuspageMetricSeriesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One service's metric stream on a chart. The metric type must be " +
			"one the service's check type can produce (see the `livck_check_types` data source) " +
			"— the server rejects incompatible combinations.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"statuspage_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"metric_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true,
			},
			"service_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Public id of the monitored service providing the data.",
			},
			"metric_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Metric key, e.g. `response_time_avg`, `uptime`, `ssl_days_until_expiry`.",
			},
			"color": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Hex color (`#RRGGBB`). Omitted, the theme default applies.",
			},
			"display_order": schema.Int64Attribute{
				Computed: true,
			},
		},
	}
}

func (r *statuspageMetricSeriesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *statuspageMetricSeriesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan seriesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateSeries(ctx, plan.StatuspageID.ValueString(), plan.MetricID.ValueString(), client.SeriesInput{
		Name:       plan.Name.ValueString(),
		ServiceID:  plan.ServiceID.ValueString(),
		MetricType: plan.MetricType.ValueString(),
		Color:      plan.Color.ValueStringPointer(),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating the series failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, seriesModelFromAPI(created, plan.StatuspageID.ValueString(), plan.MetricID.ValueString()))...)
}

func (r *statuspageMetricSeriesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state seriesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote, err := r.client.GetSeries(ctx, state.StatuspageID.ValueString(), state.MetricID.ValueString(), state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading the series failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, seriesModelFromAPI(remote, state.StatuspageID.ValueString(), state.MetricID.ValueString()))...)
}

func (r *statuspageMetricSeriesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state seriesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateSeries(ctx, state.StatuspageID.ValueString(), state.MetricID.ValueString(), state.ID.ValueString(), client.SeriesInput{
		Name:       plan.Name.ValueString(),
		ServiceID:  plan.ServiceID.ValueString(),
		MetricType: plan.MetricType.ValueString(),
		Color:      plan.Color.ValueStringPointer(),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Updating the series failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, seriesModelFromAPI(updated, state.StatuspageID.ValueString(), state.MetricID.ValueString()))...)
}

func (r *statuspageMetricSeriesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state seriesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSeries(ctx, state.StatuspageID.ValueString(), state.MetricID.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Deleting the series failed", err)
	}
}

// ImportState expects the composite id `<statuspage_id>/<metric_id>/<series_id>`.
func (r *statuspageMetricSeriesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import id", fmt.Sprintf("Expected <statuspage_id>/<metric_id>/<series_id>, got %q.", req.ID))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("statuspage_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("metric_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[2])...)
}

func seriesModelFromAPI(remote *client.Series, statuspageID, metricID string) *seriesModel {
	m := &seriesModel{
		ID:           types.StringValue(remote.ID),
		StatuspageID: types.StringValue(statuspageID),
		MetricID:     types.StringValue(metricID),
		Name:         types.StringValue(remote.Name),
		MetricType:   types.StringValue(remote.MetricType),
		Color:        types.StringPointerValue(remote.Color),
		DisplayOrder: types.Int64Value(remote.DisplayOrder),
	}

	if remote.Service != nil {
		m.ServiceID = types.StringValue(remote.Service.ID)
	} else {
		m.ServiceID = types.StringNull()
	}

	return m
}
