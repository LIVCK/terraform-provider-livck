package provider

import (
	"context"
	"errors"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = (*tagResource)(nil)
	_ resource.ResourceWithConfigure   = (*tagResource)(nil)
	_ resource.ResourceWithImportState = (*tagResource)(nil)
)

type tagResource struct {
	client *client.Client
}

// Mirrors the server-side key rule (TagInput::KEY_PATTERN): lowercase, starts
// and ends alphanumeric, inner `_ . -` only — NO `/`, no trailing separator.
// Keep in exact sync with the Laravel constant or configs pass here and 422 late.
var tagKeyPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9_.-]{0,48}[a-z0-9])?$`)

type tagModel struct {
	ID    types.String `tfsdk:"id"`
	Key   types.String `tfsdk:"key"`
	Value types.String `tfsdk:"value"`
	Color types.String `tfsdk:"color"`
	Label types.String `tfsdk:"label"`
}

func NewTagResource() resource.Resource { return &tagResource{} }

func (r *tagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (r *tagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An organization-wide label for services — either a bare label (`critical`) " +
			"or a key/value pair (`env` / `prod`). Attach tags to services via the `tags` argument on " +
			"`livck_service`, and turn a statuspage group into a tag-synced group via `sync_tag_id` on " +
			"`livck_statuspage_component`. Renaming keeps the id stable, so every tagged service and " +
			"synced group follows. The (key, value) pair is unique per organization; a duplicate is " +
			"rejected — import the existing tag instead.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"key": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Lowercase key, e.g. `env`, `team`, `critical`. Allowed: `a-z`, `0-9`, " +
					"`_`, `.`, `/`, `-` (must start alphanumeric). `:` and `=` are reserved separators.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(tagKeyPattern, "must be lowercase and may contain a-z, 0-9, _, ., /, - (starting alphanumeric)"),
					stringvalidator.LengthAtMost(50),
				},
			},
			"value": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional value (keeps its casing), e.g. `prod`. Omitted, the tag is a bare label.",
				Validators:          []validator.String{stringvalidator.LengthAtMost(100)},
			},
			"color": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Hex color (`#RRGGBB`). Omitted, the server derives a deterministic color from key/value.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"label": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Canonical display form — `key:value`, or just `key` for bare labels.",
			},
		},
	}
}

func (r *tagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *tagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateTag(ctx, client.TagInput{
		Key:   plan.Key.ValueString(),
		Value: plan.Value.ValueStringPointer(),
		Color: colorPtr(plan.Color),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating the tag failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, tagModelFromAPI(created))...)
}

func (r *tagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state tagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote, err := r.client.GetTag(ctx, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading the tag failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, tagModelFromAPI(remote))...)
}

func (r *tagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state tagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateTag(ctx, state.ID.ValueString(), client.TagInput{
		Key:   plan.Key.ValueString(),
		Value: plan.Value.ValueStringPointer(),
		Color: colorPtr(plan.Color),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Updating the tag failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, tagModelFromAPI(updated))...)
}

// Delete surfaces the server's reference protection as-is: a tag still used by
// an SLA objective or a tag-synced statuspage group is a 422, not a force-delete.
func (r *tagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state tagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteTag(ctx, state.ID.ValueString())
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Deleting the tag failed", err)
	}
}

func (r *tagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func tagModelFromAPI(remote *client.Tag) *tagModel {
	return &tagModel{
		ID:    types.StringValue(remote.ID),
		Key:   types.StringValue(remote.Key),
		Value: types.StringPointerValue(remote.Value),
		Color: types.StringValue(remote.Color),
		Label: types.StringValue(remote.Label),
	}
}

func colorPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueStringPointer()
}
