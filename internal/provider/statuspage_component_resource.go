package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

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
	ID                      types.String `tfsdk:"id"`
	StatuspageID            types.String `tfsdk:"statuspage_id"`
	Name                    types.String `tfsdk:"name"`
	NameTranslations        types.Map    `tfsdk:"name_translations"`
	Description             types.String `tfsdk:"description"`
	DescriptionTranslations types.Map    `tfsdk:"description_translations"`
	ServiceID               types.String `tfsdk:"service_id"`
	ParentID                types.String `tfsdk:"parent_id"`
	IsGroup                 types.Bool   `tfsdk:"is_group"`
	IsVisible               types.Bool   `tfsdk:"is_visible"`
	DisplayOrder            types.Int64  `tfsdk:"display_order"`
	Status                  types.String `tfsdk:"status"`
	SyncTagID               types.String `tfsdk:"sync_tag_id"`
	SyncNewVisible          types.Bool   `tfsdk:"sync_new_visible"`
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
				Optional:            true,
				MarkdownDescription: "Single-language name. Exactly one of `name` and `name_translations` must be set.",
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.MatchRoot("name"), path.MatchRoot("name_translations")),
				},
			},
			"name_translations": schema.MapAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Multilingual name as a `{locale = value}` map.",
			},
			"description": schema.StringAttribute{
				Optional: true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("description_translations")),
				},
			},
			"description_translations": schema.MapAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Multilingual description as a `{locale = value}` map.",
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
				Optional: true,
				Computed: true,
				MarkdownDescription: "Position within the parent (ascending). Omitted, the server " +
					"appends at the end. Set explicit values (e.g. 10, 20, 30) to manage ordering " +
					"declaratively.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Live component status (runtime state driven by incidents/checks, read-only).",
			},
			"sync_tag_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Tag id (`livck_tag.….id`) that turns this GROUP into a tag-synced " +
					"group: every service carrying the tag materializes as a machine-managed child " +
					"server-side. Do NOT declare those children in Terraform — they are owned by the " +
					"sync (`is_sync_managed` in the API) and follow tagging changes automatically. " +
					"Only valid on groups (`is_group = true`); unsetting it releases the group and its " +
					"children into normal, manually-managed components.",
			},
			"sync_new_visible": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "Whether services that later join the sync tag appear as VISIBLE " +
					"children (default `true`). Only meaningful together with `sync_tag_id`.",
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
		Name:           translatableInput(ctx, plan.Name, plan.NameTranslations, &resp.Diagnostics),
		IsGroup:        plan.IsGroup.ValueBoolPointer(),
		IsVisible:      plan.IsVisible.ValueBoolPointer(),
		SyncTagID:      plan.SyncTagID.ValueStringPointer(),
		SyncNewVisible: plan.SyncNewVisible.ValueBoolPointer(),
	}
	if !plan.DisplayOrder.IsNull() && !plan.DisplayOrder.IsUnknown() {
		in.DisplayOrder = plan.DisplayOrder.ValueInt64Pointer()
	}
	if desc := translatableInput(ctx, plan.Description, plan.DescriptionTranslations, &resp.Diagnostics); desc != nil {
		in.Description = desc
	}
	if resp.Diagnostics.HasError() {
		return
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

	resp.Diagnostics.Append(resp.State.Set(ctx, componentModelFromAPI(ctx, created, plan.StatuspageID.ValueString(), &plan, &resp.Diagnostics))...)
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

	resp.Diagnostics.Append(resp.State.Set(ctx, componentModelFromAPI(ctx, remote, state.StatuspageID.ValueString(), &state, &resp.Diagnostics))...)
}

func (r *statuspageComponentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state componentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.ComponentInput{
		Name:      translatableInput(ctx, plan.Name, plan.NameTranslations, &resp.Diagnostics),
		IsGroup:   plan.IsGroup.ValueBoolPointer(),
		IsVisible: plan.IsVisible.ValueBoolPointer(),
		// null clears the link server-side; omitted keeps it — send explicitly
		// (sync_tag_id: null unsyncs the group the same way).
		Description:    translatableInput(ctx, plan.Description, plan.DescriptionTranslations, &resp.Diagnostics),
		ServiceID:      plan.ServiceID.ValueStringPointer(),
		ParentID:       plan.ParentID.ValueStringPointer(),
		SyncTagID:      plan.SyncTagID.ValueStringPointer(),
		SyncNewVisible: plan.SyncNewVisible.ValueBoolPointer(),
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.DisplayOrder.IsNull() && !plan.DisplayOrder.IsUnknown() {
		in.DisplayOrder = plan.DisplayOrder.ValueInt64Pointer()
	}

	updated, err := r.client.UpdateComponent(ctx, state.StatuspageID.ValueString(), state.ID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Updating the component failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, componentModelFromAPI(ctx, updated, state.StatuspageID.ValueString(), &plan, &resp.Diagnostics))...)
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

func componentModelFromAPI(ctx context.Context, remote *client.Component, statuspageID string, prior *componentModel, diags *diag.Diagnostics) *componentModel {
	m := &componentModel{
		ID:             types.StringValue(remote.ID),
		StatuspageID:   types.StringValue(statuspageID),
		ParentID:       types.StringPointerValue(remote.ParentID),
		IsGroup:        types.BoolValue(remote.IsGroup),
		IsVisible:      types.BoolValue(remote.IsVisible),
		DisplayOrder:   types.Int64Value(remote.DisplayOrder),
		Status:         types.StringValue(remote.Status),
		SyncTagID:      types.StringPointerValue(remote.SyncTagID),
		SyncNewVisible: types.BoolValue(remote.SyncNewVisible),
	}

	if remote.Service != nil {
		m.ServiceID = types.StringValue(remote.Service.ID)
	} else {
		m.ServiceID = types.StringNull()
	}

	priorNames, priorDescs := types.MapNull(types.StringType), types.MapNull(types.StringType)
	if prior != nil {
		priorNames, priorDescs = prior.NameTranslations, prior.DescriptionTranslations
	}
	m.Name, m.NameTranslations = translatableFromAPI(ctx, remote.Name, remote.NameTranslations, priorNames, diags)

	// A fully-unset description must stay null on BOTH representations.
	if remote.Description == nil && len(remote.DescriptionTranslations) == 0 {
		m.Description, m.DescriptionTranslations = types.StringNull(), types.MapNull(types.StringType)
	} else {
		resolved := ""
		if remote.Description != nil {
			resolved = *remote.Description
		}
		m.Description, m.DescriptionTranslations = translatableFromAPI(ctx, resolved, remote.DescriptionTranslations, priorDescs, diags)
	}

	return m
}
