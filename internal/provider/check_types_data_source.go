package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/livck/terraform-provider-livck/internal/client"
)

var (
	_ datasource.DataSource              = (*checkTypesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*checkTypesDataSource)(nil)
)

type checkTypesDataSource struct {
	client *client.Client
}

type checkTypesModel struct {
	CatalogJSON jsontypes.Normalized `tfsdk:"catalog_json"`
}

func NewCheckTypesDataSource() datasource.DataSource { return &checkTypesDataSource{} }

func (d *checkTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_check_types"
}

func (d *checkTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The live per-check-type field/condition catalog (config fields, " +
			"allowed condition fields/operators, interval bounds). Exposed as JSON. Decode " +
			"with `jsondecode()`.",
		Attributes: map[string]schema.Attribute{
			"catalog_json": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Computed:   true,
			},
		},
	}
}

func (d *checkTypesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *checkTypesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	raw, err := d.client.CheckTypeCatalog(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Fetching the check-type catalog failed", err)
		return
	}

	state := checkTypesModel{CatalogJSON: jsontypes.NewNormalizedValue(string(raw))}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
