package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

var (
	_ resource.Resource                = (*statuspageResource)(nil)
	_ resource.ResourceWithConfigure   = (*statuspageResource)(nil)
	_ resource.ResourceWithImportState = (*statuspageResource)(nil)
)

type statuspageResource struct {
	client *client.Client
}

type statuspageModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	NameTranslations types.Map    `tfsdk:"name_translations"`
	Slug             types.String `tfsdk:"slug"`
	Published        types.Bool   `tfsdk:"published"`
	AccessType       types.String `tfsdk:"access_type"`
}

func NewStatuspageResource() resource.Resource { return &statuspageResource{} }

func (r *statuspageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_statuspage"
}

func (r *statuspageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A public status page, served at `{slug}.statuspage.de`. " +
			"Components are managed via `livck_statuspage_component`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
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
				MarkdownDescription: "Multilingual name as a `{locale = value}` map, e.g. `{ de = \"Systemstatus\", en = \"System Status\" }`.",
			},
			"slug": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Public subdomain slug (lowercase letters, numbers, hyphens; " +
					"globally unique). Omitted, a unique slug is derived from the name. Changing " +
					"it frees the old subdomain (reserved against takeover for a cooldown; your " +
					"own organization can re-claim it anytime).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"published": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Live state. Publishing consumes a plan slot and fails at the " +
					"limit; unpublishing is always allowed (emergency takedown). Omitted, the page " +
					"goes live automatically when a slot is free (draft otherwise).",
			},
			"access_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Access control mode (v0 of the API creates public pages; password/whitelist stay console-managed).",
			},
		},
	}
}

func (r *statuspageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *statuspageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan statuspageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.StatuspageInput{Name: translatableInput(ctx, plan.Name, plan.NameTranslations, &resp.Diagnostics)}
	if !plan.Slug.IsNull() && !plan.Slug.IsUnknown() {
		in.Slug = plan.Slug.ValueStringPointer()
	}
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateStatuspage(ctx, in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating the status page failed", err)
		return
	}

	// The server decides the initial published state (live when a plan slot is
	// free, draft otherwise). An explicit `published` in the config that differs
	// is reconciled through the dedicated publish/unpublish actions.
	if !plan.Published.IsNull() && !plan.Published.IsUnknown() && plan.Published.ValueBool() != created.IsPublished {
		created, err = r.client.PublishStatuspage(ctx, created.ID, plan.Published.ValueBool())
		if err != nil {
			addAPIError(&resp.Diagnostics, "Reconciling the published state after creation failed", err)
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, statuspageModelFromAPI(ctx, created, &plan, &resp.Diagnostics))...)
}

func (r *statuspageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state statuspageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote, err := r.client.GetStatuspage(ctx, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading the status page failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, statuspageModelFromAPI(ctx, remote, &state, &resp.Diagnostics))...)
}

func (r *statuspageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state statuspageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.StatuspageInput{Name: translatableInput(ctx, plan.Name, plan.NameTranslations, &resp.Diagnostics)}
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Slug.IsNull() && !plan.Slug.IsUnknown() {
		in.Slug = plan.Slug.ValueStringPointer()
	}

	updated, err := r.client.UpdateStatuspage(ctx, state.ID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Updating the status page failed", err)
		return
	}

	if !plan.Published.IsNull() && !plan.Published.IsUnknown() && plan.Published.ValueBool() != updated.IsPublished {
		updated, err = r.client.PublishStatuspage(ctx, state.ID.ValueString(), plan.Published.ValueBool())
		if err != nil {
			addAPIError(&resp.Diagnostics, "Changing the published state failed", err)
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, statuspageModelFromAPI(ctx, updated, &plan, &resp.Diagnostics))...)
}

func (r *statuspageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state statuspageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteStatuspage(ctx, state.ID.ValueString()); err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Deleting the status page failed", err)
	}
}

func (r *statuspageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func statuspageModelFromAPI(ctx context.Context, remote *client.Statuspage, prior *statuspageModel, diags *diag.Diagnostics) *statuspageModel {
	m := &statuspageModel{
		ID:         types.StringValue(remote.ID),
		Slug:       types.StringValue(remote.Slug),
		Published:  types.BoolValue(remote.IsPublished),
		AccessType: types.StringValue(remote.AccessType),
	}

	priorNames := types.MapNull(types.StringType)
	if prior != nil {
		priorNames = prior.NameTranslations
	}
	m.Name, m.NameTranslations = translatableFromAPI(ctx, remote.Name, remote.NameTranslations, priorNames, diags)

	return m
}
