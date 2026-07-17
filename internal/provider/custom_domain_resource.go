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
	_ resource.Resource                = (*customDomainResource)(nil)
	_ resource.ResourceWithConfigure   = (*customDomainResource)(nil)
	_ resource.ResourceWithImportState = (*customDomainResource)(nil)
)

type customDomainResource struct {
	client *client.Client
}

type customDomainModel struct {
	ID             types.String `tfsdk:"id"`
	StatuspageID   types.String `tfsdk:"statuspage_id"`
	Hostname       types.String `tfsdk:"hostname"`
	Status         types.String `tfsdk:"status"`
	CnameTarget    types.String `tfsdk:"cname_target"`
	TxtRecordName  types.String `tfsdk:"txt_record_name"`
	TxtRecordValue types.String `tfsdk:"txt_record_value"`
	Verified       types.Bool   `tfsdk:"verified"`
}

func NewCustomDomainResource() resource.Resource { return &customDomainResource{} }

func (r *customDomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_domain"
}

func (r *customDomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A custom domain on a status page. Creating it returns the COMPLETE DNS " +
			"record set as computed attributes. Wire `cname_target`, `txt_record_name` and " +
			"`txt_record_value` straight into your DNS provider's Terraform resources (Cloudflare, " +
			"Route53, ...) and the platform's background verification takes the domain live on its " +
			"own; no further apply needed. TLS is issued automatically once verified.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"statuspage_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The status page served under this domain. Immutable: changing it replaces the domain.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"hostname": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Fully-qualified hostname, e.g. `status.example.com`. Immutable: " +
					"changing it replaces the domain (new identity, new TXT token).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Verification lifecycle: `pending_verification`, then `active` (or `failed` " +
					"after the retry budget). Server-managed, refreshed on read.",
			},
			"cname_target": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Create a CNAME record pointing `hostname` at this target.",
			},
			"txt_record_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the ownership-proof TXT record (`_livck-verify.<hostname>`).",
			},
			"txt_record_value": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Value of the ownership-proof TXT record.",
			},
			"verified": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether DNS verification has completed (read-only).",
			},
		},
	}
}

func (r *customDomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *customDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan customDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateCustomDomain(ctx, plan.StatuspageID.ValueString(), client.CustomDomainInput{
		Hostname: plan.Hostname.ValueString(),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "Attaching the custom domain failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, customDomainModelFromAPI(created, plan.StatuspageID.ValueString()))...)
}

func (r *customDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state customDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote, err := r.client.GetCustomDomain(ctx, state.StatuspageID.ValueString(), state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading the custom domain failed", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, customDomainModelFromAPI(remote, state.StatuspageID.ValueString()))...)
}

// Update never runs: both configurable attributes are RequiresReplace.
func (r *customDomainResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Custom domains cannot be updated in place",
		"Both statuspage_id and hostname force a replacement, so this is a provider bug.",
	)
}

func (r *customDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state customDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteCustomDomain(ctx, state.StatuspageID.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		addAPIError(&resp.Diagnostics, "Detaching the custom domain failed", err)
	}
}

// ImportState expects the composite id `<statuspage_id>/<domain_id>`.
func (r *customDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import id", fmt.Sprintf("Expected <statuspage_id>/<domain_id>, got %q.", req.ID))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("statuspage_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

func customDomainModelFromAPI(remote *client.CustomDomain, statuspageID string) *customDomainModel {
	return &customDomainModel{
		ID:             types.StringValue(remote.ID),
		StatuspageID:   types.StringValue(statuspageID),
		Hostname:       types.StringValue(remote.Hostname),
		Status:         types.StringValue(remote.Status),
		CnameTarget:    types.StringValue(remote.CnameTarget),
		TxtRecordName:  types.StringValue(remote.TxtRecordName),
		TxtRecordValue: types.StringValue(remote.TxtRecordValue),
		Verified:       types.BoolValue(remote.Verified),
	}
}
