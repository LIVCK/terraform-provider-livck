package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

var (
	_ datasource.DataSource              = (*statuspageDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*statuspageDataSource)(nil)
)

type statuspageDataSource struct {
	client *client.Client
}

type statuspageDataModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Slug       types.String `tfsdk:"slug"`
	Published  types.Bool   `tfsdk:"published"`
	AccessType types.String `tfsdk:"access_type"`
}

func NewStatuspageDataSource() datasource.DataSource { return &statuspageDataSource{} }

func (d *statuspageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_statuspage"
}

func (d *statuspageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an existing status page by its public id (e.g. one managed outside of Terraform).",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Required: true},
			"name":        schema.StringAttribute{Computed: true},
			"slug":        schema.StringAttribute{Computed: true},
			"published":   schema.BoolAttribute{Computed: true},
			"access_type": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *statuspageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *statuspageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config statuspageDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote, err := d.client.GetStatuspage(ctx, config.ID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Reading the status page failed", err)
		return
	}

	state := statuspageDataModel{
		ID:         types.StringValue(remote.ID),
		Name:       types.StringValue(remote.Name),
		Slug:       types.StringValue(remote.Slug),
		Published:  types.BoolValue(remote.IsPublished),
		AccessType: types.StringValue(remote.AccessType),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
