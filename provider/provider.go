package provider

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/andrewbaxter/terraform-provider-fly/providerstate"
	"github.com/andrewbaxter/terraform-provider-fly/utils"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*flyProvider)(nil)

type flyProvider struct{}

type flyProviderData struct {
	FlyToken        types.String `tfsdk:"fly_api_token"`
	FlyHttpEndpoint types.String `tfsdk:"fly_http_endpoint"`
}

func (p *flyProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "fly"
}

func (p *flyProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data flyProviderData
	diags := req.Config.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	var token string
	if data.FlyToken.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unable to create client",
			"Cannot use unknown value as token",
		)
		return
	}
	if !data.FlyToken.IsNull() {
		token = data.FlyToken.ValueString()
	}
	if token == "" {
		token = os.Getenv("FLY_API_TOKEN")
	}
	if token == "" {
		resp.Diagnostics.AddError(
			"Unable to find token",
			"token cannot be an empty string",
		)
		return
	}

	restBaseUrl := ""
	if !data.FlyHttpEndpoint.IsNull() && !data.FlyHttpEndpoint.IsUnknown() {
		restBaseUrl = data.FlyHttpEndpoint.ValueString()
	}
	if restBaseUrl == "" {
		restBaseUrl = os.Getenv("FLY_HTTP_ENDPOINT")
	}
	if restBaseUrl == "" {
		restBaseUrl = "https://api.machines.dev"
	}

	enableTracing := false
	_, ok := os.LookupEnv("DEBUG")
	if ok {
		enableTracing = true
		resp.Diagnostics.AddWarning("Debug mode enabled", "Debug mode enabled, this will add the Fly-Force-Trace header to all graphql requests")
	}

	state := &providerstate.State{
		EnableTracing: enableTracing,
		Token:         token,
		RestBaseUrl:   restBaseUrl,
		GraphqlClient: graphql.NewClient("https://api.fly.io/graphql", &http.Client{
			Timeout: 60 * time.Second,
			Transport: &utils.GraphqlTransport{
				Inner: &utils.LoggingHttpTransport{
					Inner: http.DefaultTransport,
				},
				Token:            token,
				EnableDebugTrace: enableTracing,
			},
		}),
	}

	resp.DataSourceData = state
	resp.ResourceData = state
}

func (p *flyProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAppResource,     // fly_app
		NewVolumeResource,  // fly_volume
		NewIpResource,      // fly_ip
		NewCertResource,    // fly_cert
		NewMachineResource, // fly_machine
	}
}

func (p *flyProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAppDataSource,    // fly_app
		NewCertDataSource,   // fly_cert
		NewIpDataSource,     // fly_ip
		NewVolumeDataSource, // fly_volume
	}
}

func (p *flyProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"fly_api_token": schema.StringAttribute{
				MarkdownDescription: "fly.io api token. If not set checks env for FLY_API_TOKEN",
				Optional:            true,
			},
			"fly_http_endpoint": schema.StringAttribute{
				MarkdownDescription: "Where the provider should look to find the fly http endpoint",
				Optional:            true,
			},
		},
	}
}

func New() func() provider.Provider {
	return func() provider.Provider {
		return &flyProvider{}
	}
}
