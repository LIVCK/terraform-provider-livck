package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

var (
	_ datasource.DataSource              = (*probesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*probesDataSource)(nil)
)

type probesDataSource struct {
	client *client.Client
}

type probesModel struct {
	Probes []probeModel `tfsdk:"probes"`
}

type probeModel struct {
	Code        types.String `tfsdk:"code"`
	Name        types.String `tfsdk:"name"`
	Location    types.String `tfsdk:"location"`
	CountryCode types.String `tfsdk:"country_code"`
}

func NewProbesDataSource() datasource.DataSource { return &probesDataSource{} }

func (d *probesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_probes"
}

func (d *probesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The active monitoring locations. Use the `code` values for " +
			"`livck_service.settings.assigned_probes`.",
		Attributes: map[string]schema.Attribute{
			"probes": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"code":         schema.StringAttribute{Computed: true},
						"name":         schema.StringAttribute{Computed: true},
						"location":     schema.StringAttribute{Computed: true},
						"country_code": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *probesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *probesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	probes, err := d.client.ListProbes(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Listing the probes failed", err)
		return
	}

	state := probesModel{}
	for _, p := range probes {
		state.Probes = append(state.Probes, probeModel{
			Code:        types.StringValue(p.Code),
			Name:        types.StringValue(p.Name),
			Location:    types.StringValue(p.Location),
			CountryCode: types.StringValue(p.CountryCode),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
