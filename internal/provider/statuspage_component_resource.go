package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

var (
	_ resource.Resource                = (*statuspageComponentResource)(nil)
	_ resource.ResourceWithConfigure   = (*statuspageComponentResource)(nil)
	_ resource.ResourceWithImportState = (*statuspageComponentResource)(nil)
)

type statuspageComponentResource struct {
	client *client.Client
}

type componentModel struct {
	ID           types.String `tfsdk:"id"`
	StatuspageID types.String `tfsdk:"statuspage_id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	ServiceID    types.String `tfsdk:"service_id"`
	ParentID     types.String `tfsdk:"parent_id"`
	IsGroup      types.Bool   `tfsdk:"is_group"`
	IsVisible    types.Bool   `tfsdk:"is_visible"`
	DisplayOrder types.Int64  `tfsdk:"display_order"`
	Status       types.String `tfsdk:"status"`
}

func NewStatuspageComponentResource() resource.Resource { return &statuspageComponentResource{} }

func (r *statuspageComponentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_statuspage_component"
}

func (r *statuspageComponentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A component (or component group) on a status page. Link it to a " +
			"monitored service via `service_id` so its status follows the checks. Display " +
			"order is server-assigned by creation order (reordering stays console-managed in v0).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"statuspage_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The owning status page. Immutable — changing it replaces the component.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true,
			},
			"description": schema.StringAttribute{
				Optional: true,
			},
			"service_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Public id of a monitored service whose status drives this component.",
			},
			"parent_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Public id of a group component on the same page.",
			},
			"is_group": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"is_visible": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"display_order": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Server-assigned position within the parent (read-only in v0).",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Live component status (runtime state driven by incidents/checks, read-only).",
			},
		},
	}
}

func (r *statuspageComponentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *statuspageComponentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan componentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.ComponentInput{
		Name:      plan.Name.ValueString(),
		IsGroup:   plan.IsGroup.ValueBoolPointer(),
		IsVisible: plan.IsVisible.ValueBoolPointer(),
	}
	if !plan.Description.IsNull() {
		in.Description = plan.Description.ValueStringPointer()
	}
	if !plan.ServiceID.IsNull() {
		in.ServiceID = plan.ServiceID.ValueStringPointer()
	}
	if !plan.ParentID.IsNull() {
		in.ParentID = plan.ParentID.ValueStringPointer()
	}

	created, err := r.client.CreateComponent(ctx, plan.StatuspageID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating the component failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, componentModelFromAPI(created, plan.StatuspageID.ValueString()))...)
}

func (r *statuspageComponentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state componentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote, err := r.client.GetComponent(ctx, state.StatuspageID.ValueString(), state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading the component failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, componentModelFromAPI(remote, state.StatuspageID.ValueString()))...)
}

func (r *statuspageComponentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state componentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.ComponentInput{
		Name:      plan.Name.ValueString(),
		IsGroup:   plan.IsGroup.ValueBoolPointer(),
		IsVisible: plan.IsVisible.ValueBoolPointer(),
		// null clears the link server-side; omitted keeps it — send explicitly.
		Description: plan.Description.ValueStringPointer(),
		ServiceID:   plan.ServiceID.ValueStringPointer(),
		ParentID:    plan.ParentID.ValueStringPointer(),
	}

	updated, err := r.client.UpdateComponent(ctx, state.StatuspageID.ValueString(), state.ID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Updating the component failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, componentModelFromAPI(updated, state.StatuspageID.ValueString()))...)
}

func (r *statuspageComponentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state componentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteComponent(ctx, state.StatuspageID.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Deleting the component failed", err)
	}
}

// ImportState expects the composite id `<statuspage_id>/<component_id>`.
func (r *statuspageComponentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid import id",
			fmt.Sprintf("Expected <statuspage_id>/<component_id>, got %q.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("statuspage_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

func componentModelFromAPI(remote *client.Component, statuspageID string) *componentModel {
	m := &componentModel{
		ID:           types.StringValue(remote.ID),
		StatuspageID: types.StringValue(statuspageID),
		Name:         types.StringValue(remote.Name),
		Description:  types.StringPointerValue(remote.Description),
		ParentID:     types.StringPointerValue(remote.ParentID),
		IsGroup:      types.BoolValue(remote.IsGroup),
		IsVisible:    types.BoolValue(remote.IsVisible),
		DisplayOrder: types.Int64Value(remote.DisplayOrder),
		Status:       types.StringValue(remote.Status),
	}

	if remote.Service != nil {
		m.ServiceID = types.StringValue(remote.Service.ID)
	} else {
		m.ServiceID = types.StringNull()
	}

	return m
}
