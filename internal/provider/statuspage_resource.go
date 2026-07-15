package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
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
	// Appearance (Optional-only: null = unmanaged, never clobbered)
	PrimaryColor         types.String `tfsdk:"primary_color"`
	SecondaryColor       types.String `tfsdk:"secondary_color"`
	CustomCSS            types.String `tfsdk:"custom_css"`
	ImprintURL           types.String `tfsdk:"imprint_url"`
	PrivacyPolicyURL     types.String `tfsdk:"privacy_policy_url"`
	ShowLogo             types.Bool   `tfsdk:"show_logo"`
	ShowAffectedServices types.Bool   `tfsdk:"show_affected_services"`
	ShowIncidentHistory  types.Bool   `tfsdk:"show_incident_history"`
	// Access
	AccessType         types.String `tfsdk:"access_type"`
	Password           types.String `tfsdk:"password"`
	HasPassword        types.Bool   `tfsdk:"has_password"`
	EmailWhitelist     types.Set    `tfsdk:"email_whitelist"`
	SubscriberChannels types.Set    `tfsdk:"subscriber_channels"`
	// Binary assets: local file paths → uploaded; served URLs computed; the
	// content hashes drive re-upload on change (stamped by fileHashModifier).
	Logo         types.String `tfsdk:"logo"`
	LogoDark     types.String `tfsdk:"logo_dark"`
	Favicon      types.String `tfsdk:"favicon"`
	LogoURL      types.String `tfsdk:"logo_url"`
	LogoDarkURL  types.String `tfsdk:"logo_dark_url"`
	FaviconURL   types.String `tfsdk:"favicon_url"`
	LogoHash     types.String `tfsdk:"logo_hash"`
	LogoDarkHash types.String `tfsdk:"logo_dark_hash"`
	FaviconHash  types.String `tfsdk:"favicon_hash"`
}

// assetSpec pairs a URL slug with the model fields that drive one asset.
type assetSpec struct {
	slug string
	path func(*statuspageModel) types.String
	hash func(*statuspageModel) types.String
}

var statuspageAssets = []assetSpec{
	{"logo", func(m *statuspageModel) types.String { return m.Logo }, func(m *statuspageModel) types.String { return m.LogoHash }},
	{"logo-dark", func(m *statuspageModel) types.String { return m.LogoDark }, func(m *statuspageModel) types.String { return m.LogoDarkHash }},
	{"favicon", func(m *statuspageModel) types.String { return m.Favicon }, func(m *statuspageModel) types.String { return m.FaviconHash }},
}

func NewStatuspageResource() resource.Resource { return &statuspageResource{} }

func (r *statuspageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_statuspage"
}

func (r *statuspageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	optionalHex := []validator.String{stringvalidator.RegexMatches(hexColorPattern, "must be a #RRGGBB hex color")}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A public status page. Manage its structure with `livck_statuspage_component` / " +
			"`livck_statuspage_metric`, and its whole appearance here: colors, custom CSS, legal links, " +
			"visibility flags, access control and binary assets (logo/favicon uploaded from local files).",
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
				MarkdownDescription: "Multilingual name as a `{locale = value}` map.",
			},
			"slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Public subdomain slug (globally unique). Omitted, it is derived from the name.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"published": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Live state. Publishing consumes a plan slot (fails at the limit); unpublishing is always allowed.",
			},

			// ── Appearance ──────────────────────────────────────────────
			"primary_color": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Brand primary color as `#RRGGBB`. Unset stops managing it (does not reset a value set elsewhere).",
				Validators:          optionalHex,
			},
			"secondary_color": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Brand secondary color as `#RRGGBB`.",
				Validators:          optionalHex,
			},
			"custom_css": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Custom CSS injected into the public page (server-sanitized).",
			},
			"imprint_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Footer imprint link (http/https/mailto only).",
			},
			"privacy_policy_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Footer privacy-policy link (http/https/mailto only).",
			},
			"show_logo": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the logo is rendered.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"show_affected_services": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"show_incident_history": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},

			// ── Access control ──────────────────────────────────────────
			"access_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "One of `public`, `password`, `email_whitelist`. Switching to `password` requires `password`; `email_whitelist` requires a non-empty `email_whitelist`.",
				Validators:          []validator.String{stringvalidator.OneOf("public", "password", "email_whitelist")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"password": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Access password (write-only — never read back; the server exposes only `has_password`). Min 8 chars. A console-side change is not detected here.",
				Validators:          []validator.String{stringvalidator.LengthBetween(8, 255)},
			},
			"has_password": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether a password is set (read-only view of the write-only `password`).",
			},
			"email_whitelist": schema.SetAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Allowed viewer emails for `email_whitelist` access. Null stops managing the list.",
			},
			"subscriber_channels": schema.SetAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Subscription channels offered on the page (`email`, `webhook`, `slack`, `teams`, `discord`, `telegram`). Null stops managing them.",
			},

			// ── Binary assets ───────────────────────────────────────────
			"logo": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to a local logo file (jpg/jpeg/png/webp/svg, ≤2 MB) — uploaded on apply. Change the file's content to trigger a re-upload; unset to remove.",
			},
			"logo_dark": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to a local dark-mode logo file (jpg/jpeg/png/webp/svg, ≤2 MB).",
			},
			"favicon": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to a local favicon file (ico/png/svg, ≤1 MB).",
			},
			"logo_url":       computedURL("The served logo URL.", "logo_hash"),
			"logo_dark_url":  computedURL("The served dark-mode logo URL.", "logo_dark_hash"),
			"favicon_url":    computedURL("The served favicon URL.", "favicon_hash"),
			"logo_hash":      computedHash("logo"),
			"logo_dark_hash": computedHash("logo_dark"),
			"favicon_hash":   computedHash("favicon"),
		},
	}
}

func computedURL(desc, hashAttr string) schema.StringAttribute {
	return schema.StringAttribute{
		Computed:            true,
		MarkdownDescription: desc,
		PlanModifiers:       []planmodifier.String{urlFollowsHash(hashAttr)},
	}
}

// computedHash is a Computed attribute re-stamped from the local file's content
// on every plan, so a changed image produces a diff that re-uploads it.
func computedHash(pathAttr string) schema.StringAttribute {
	return schema.StringAttribute{
		Computed:            true,
		MarkdownDescription: "Content hash of the local asset file (managed automatically).",
		PlanModifiers:       []planmodifier.String{fileHashFrom(pathAttr)},
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

	page, err := r.client.CreateStatuspage(ctx, in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Creating the status page failed", err)
		return
	}

	// The create endpoint takes name+slug only; apply appearance/access in a
	// follow-up update (mirrors the console, where branding is edit-only).
	if branding, has := brandingInput(ctx, &plan, &resp.Diagnostics); has {
		page, err = r.client.UpdateStatuspage(ctx, page.ID, branding)
		if err != nil {
			addAPIError(&resp.Diagnostics, "Applying the status page branding failed", err)
			return
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Published.IsNull() && !plan.Published.IsUnknown() && plan.Published.ValueBool() != page.IsPublished {
		page, err = r.client.PublishStatuspage(ctx, page.ID, plan.Published.ValueBool())
		if err != nil {
			addAPIError(&resp.Diagnostics, "Reconciling the published state after creation failed", err)
			return
		}
	}

	page = r.syncAssets(ctx, page, &plan, nil, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, statuspageModelFromAPI(ctx, page, &plan, &resp.Diagnostics))...)
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

	newState := statuspageModelFromAPI(ctx, remote, &state, &resp.Diagnostics)

	// Served asset URLs are regenerated server-side after upload (async image
	// conversion → new conversion path + cache-buster), so a refresh would read
	// a value different from the one captured at upload time and diff forever.
	// The URL only meaningfully changes when WE re-upload (driven by the content
	// hash in Update), so on a plain refresh we keep the captured value.
	newState.LogoURL = state.LogoURL
	newState.LogoDarkURL = state.LogoDarkURL
	newState.FaviconURL = state.FaviconURL

	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *statuspageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state statuspageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, _ := brandingInput(ctx, &plan, &resp.Diagnostics)
	in.Name = translatableInput(ctx, plan.Name, plan.NameTranslations, &resp.Diagnostics)
	if !plan.Slug.IsNull() && !plan.Slug.IsUnknown() {
		in.Slug = plan.Slug.ValueStringPointer()
	}
	if resp.Diagnostics.HasError() {
		return
	}

	page, err := r.client.UpdateStatuspage(ctx, state.ID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Updating the status page failed", err)
		return
	}

	if !plan.Published.IsNull() && !plan.Published.IsUnknown() && plan.Published.ValueBool() != page.IsPublished {
		page, err = r.client.PublishStatuspage(ctx, state.ID.ValueString(), plan.Published.ValueBool())
		if err != nil {
			addAPIError(&resp.Diagnostics, "Changing the published state failed", err)
			return
		}
	}

	page = r.syncAssets(ctx, page, &plan, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, statuspageModelFromAPI(ctx, page, &plan, &resp.Diagnostics))...)
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

// syncAssets uploads changed local files and removes cleared ones. On create
// prior is nil (everything set is uploaded). Returns the latest page echo so
// the caller reads fresh URLs. A changed file is detected via its content hash.
func (r *statuspageResource) syncAssets(ctx context.Context, page *client.Statuspage, plan, prior *statuspageModel, diags *diag.Diagnostics) *client.Statuspage {
	for _, a := range statuspageAssets {
		planPath := a.path(plan)

		if !planPath.IsNull() && !planPath.IsUnknown() {
			changed := prior == nil ||
				a.path(prior).ValueString() != planPath.ValueString() ||
				a.hash(prior).ValueString() != a.hash(plan).ValueString()
			if !changed {
				continue
			}
			content, err := os.ReadFile(planPath.ValueString())
			if err != nil {
				diags.AddError("Cannot read the asset file", err.Error())
				return page
			}
			updated, err := r.client.UploadStatuspageAsset(ctx, page.ID, a.slug, filepath.Base(planPath.ValueString()), content)
			if err != nil {
				addAPIError(diags, "Uploading the "+a.slug+" failed", err)
				return page
			}
			page = updated
			continue
		}

		// Cleared: delete only when the prior state actually had one.
		if prior != nil && !a.path(prior).IsNull() {
			updated, err := r.client.DeleteStatuspageAsset(ctx, page.ID, a.slug)
			if err != nil {
				addAPIError(diags, "Removing the "+a.slug+" failed", err)
				return page
			}
			page = updated
		}
	}
	return page
}

// brandingInput builds the appearance/access portion of the input from the plan.
// Fields the practitioner does not manage (null) are left out (omitempty), so an
// unmanaged field is never clobbered. Returns whether anything was set.
func brandingInput(ctx context.Context, plan *statuspageModel, diags *diag.Diagnostics) (client.StatuspageInput, bool) {
	in := client.StatuspageInput{}
	has := false

	set := func(v types.String, dst **string) {
		if !v.IsNull() && !v.IsUnknown() {
			*dst = v.ValueStringPointer()
			has = true
		}
	}
	setBool := func(v types.Bool, dst **bool) {
		if !v.IsNull() && !v.IsUnknown() {
			*dst = v.ValueBoolPointer()
			has = true
		}
	}

	set(plan.PrimaryColor, &in.PrimaryColor)
	set(plan.SecondaryColor, &in.SecondaryColor)
	set(plan.CustomCSS, &in.CustomCSS)
	set(plan.ImprintURL, &in.ImprintURL)
	set(plan.PrivacyPolicyURL, &in.PrivacyPolicyURL)
	setBool(plan.ShowLogo, &in.ShowLogo)
	setBool(plan.ShowAffectedServices, &in.ShowAffectedServices)
	setBool(plan.ShowIncidentHistory, &in.ShowIncidentHistory)
	set(plan.AccessType, &in.AccessType)
	set(plan.Password, &in.Password)

	if !plan.EmailWhitelist.IsNull() && !plan.EmailWhitelist.IsUnknown() {
		var list []string
		diags.Append(plan.EmailWhitelist.ElementsAs(ctx, &list, false)...)
		in.EmailWhitelist = &list
		has = true
	}
	if !plan.SubscriberChannels.IsNull() && !plan.SubscriberChannels.IsUnknown() {
		var list []string
		diags.Append(plan.SubscriberChannels.ElementsAs(ctx, &list, false)...)
		in.SubscriberChannels = &list
		has = true
	}

	return in, has
}

func statuspageModelFromAPI(ctx context.Context, remote *client.Statuspage, prior *statuspageModel, diags *diag.Diagnostics) *statuspageModel {
	m := &statuspageModel{
		ID:          types.StringValue(remote.ID),
		Slug:        types.StringValue(remote.Slug),
		Published:   types.BoolValue(remote.IsPublished),
		AccessType:  types.StringValue(remote.AccessType),
		HasPassword: types.BoolValue(remote.HasPassword),
		// Server-driven flags always reflect the effective value.
		ShowLogo:             types.BoolValue(remote.ShowLogo),
		ShowAffectedServices: types.BoolValue(remote.ShowAffectedServices),
		ShowIncidentHistory:  types.BoolValue(remote.ShowIncidentHistory),
		// Served URLs.
		LogoURL:     types.StringPointerValue(remote.LogoURL),
		LogoDarkURL: types.StringPointerValue(remote.LogoDarkURL),
		FaviconURL:  types.StringPointerValue(remote.FaviconURL),
	}

	priorNames := types.MapNull(types.StringType)
	if prior != nil {
		priorNames = prior.NameTranslations
	}
	m.Name, m.NameTranslations = translatableFromAPI(ctx, remote.Name, remote.NameTranslations, priorNames, diags)

	// Appearance strings: echo the server value only when the field is managed
	// (prior set), else keep null — a null Optional attribute is "unmanaged" and
	// must not drift against the server's stored value.
	m.PrimaryColor = keepOrEcho(prior, func(p *statuspageModel) types.String { return p.PrimaryColor }, remote.PrimaryColor)
	m.SecondaryColor = keepOrEcho(prior, func(p *statuspageModel) types.String { return p.SecondaryColor }, remote.SecondaryColor)
	m.CustomCSS = keepOrEcho(prior, func(p *statuspageModel) types.String { return p.CustomCSS }, remote.CustomCSS)
	m.ImprintURL = keepOrEcho(prior, func(p *statuspageModel) types.String { return p.ImprintURL }, remote.ImprintURL)
	m.PrivacyPolicyURL = keepOrEcho(prior, func(p *statuspageModel) types.String { return p.PrivacyPolicyURL }, remote.PrivacyPolicyURL)

	// email_whitelist / subscriber_channels: same null-unmanaged pivot.
	m.EmailWhitelist = keepOrEchoSet(ctx, prior, func(p *statuspageModel) types.Set { return p.EmailWhitelist }, remote.EmailWhitelist, diags)
	m.SubscriberChannels = keepOrEchoSet(ctx, prior, func(p *statuspageModel) types.Set { return p.SubscriberChannels }, remote.SubscriberChannels, diags)

	// Write-only + local-only fields are never echoed by the server: carry them
	// from the prior (plan on write, state on read).
	if prior != nil {
		m.Password = prior.Password
		m.Logo, m.LogoDark, m.Favicon = prior.Logo, prior.LogoDark, prior.Favicon
		m.LogoHash, m.LogoDarkHash, m.FaviconHash = prior.LogoHash, prior.LogoDarkHash, prior.FaviconHash
	} else {
		m.Password = types.StringNull()
		m.Logo, m.LogoDark, m.Favicon = types.StringNull(), types.StringNull(), types.StringNull()
		m.LogoHash, m.LogoDarkHash, m.FaviconHash = types.StringNull(), types.StringNull(), types.StringNull()
	}

	return m
}

// keepOrEcho returns null when the field is unmanaged (prior null), otherwise
// the server's current value.
func keepOrEcho(prior *statuspageModel, get func(*statuspageModel) types.String, remote *string) types.String {
	if prior == nil || get(prior).IsNull() {
		return types.StringNull()
	}
	return types.StringPointerValue(remote)
}

func keepOrEchoSet(ctx context.Context, prior *statuspageModel, get func(*statuspageModel) types.Set, remote []string, diags *diag.Diagnostics) types.Set {
	if prior == nil || get(prior).IsNull() {
		return types.SetNull(types.StringType)
	}
	set, d := types.SetValueFrom(ctx, types.StringType, remote)
	diags.Append(d...)
	return set
}
