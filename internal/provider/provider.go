package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/livck/terraform-provider-livck/internal/client"
)

var _ provider.Provider = (*livckProvider)(nil)

type livckProvider struct {
	version string
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	APIToken types.String `tfsdk:"api_token"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &livckProvider{version: version}
	}
}

func (p *livckProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "livck"
	resp.Version = p.version
}

func (p *livckProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage LIVCK monitoring, status pages and maintenances as code. " +
			"Authentication uses an organization API token (`lvk_…`, plan Team or higher) — " +
			"create one in the console under *Settings → API Tokens* with the abilities the " +
			"managed resources need (services.\\*, statuspages.\\* incl. publish, maintenances.\\*).",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "API endpoint. Defaults to `https://api.livck.cloud`. " +
					"Can also be set via the `LIVCK_ENDPOINT` environment variable.",
			},
			"api_token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Organization API token (`lvk_…`). Prefer the " +
					"`LIVCK_API_TOKEN` environment variable over hardcoding it.",
			},
		},
	}
}

func (p *livckProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := os.Getenv("LIVCK_ENDPOINT")
	if !config.Endpoint.IsNull() {
		endpoint = config.Endpoint.ValueString()
	}
	if endpoint == "" {
		endpoint = "https://api.livck.cloud"
	}

	token := os.Getenv("LIVCK_API_TOKEN")
	if !config.APIToken.IsNull() {
		token = config.APIToken.ValueString()
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_token"),
			"Missing LIVCK API token",
			"Set the api_token provider attribute or the LIVCK_API_TOKEN environment variable. "+
				"Tokens are minted in the LIVCK console (Settings → API Tokens) and require the Team plan or higher.",
		)
		return
	}

	c := client.New(endpoint, token)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *livckProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewServiceResource,
		NewStatuspageResource,
		NewStatuspageComponentResource,
		NewStatuspageMetricResource,
		NewStatuspageMetricSeriesResource,
		NewMaintenanceResource,
		NewTagResource,
		NewCustomDomainResource,
	}
}

func (p *livckProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewProbesDataSource,
		NewCheckTypesDataSource,
		NewStatuspageDataSource,
	}
}
