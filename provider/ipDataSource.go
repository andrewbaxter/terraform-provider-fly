package provider

import (
	"context"
	"fmt"

	"github.com/andrewbaxter/terraform-provider-fly/graphql"
	"github.com/andrewbaxter/terraform-provider-fly/providerstate"
	"github.com/andrewbaxter/terraform-provider-fly/utils"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces
var _ datasource.DataSource = &ipDataSourceType{}
var _ datasource.DataSourceWithConfigure = &ipDataSourceType{}

func NewIpDataSource() datasource.DataSource {
	return &ipDataSourceType{}
}

type ipDataSourceType struct {
	state *providerstate.State
}

func (d *ipDataSourceType) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "fly_ip"
}

func (d *ipDataSourceType) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.state = req.ProviderData.(*providerstate.State)
}

// Matches Schema
type ipDataSourceOutput struct {
	Id      types.String `tfsdk:"id"`
	App     types.String `tfsdk:"app"`
	Region  types.String `tfsdk:"region"`
	Address types.String `tfsdk:"address"`
	Type    types.String `tfsdk:"type"`
}

func (d *ipDataSourceType) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"address": schema.StringAttribute{
				MarkdownDescription: "Empty if using `shared_v4`",
				Computed:            true,
			},
			"app": schema.StringAttribute{
				MarkdownDescription: APP_DESC,
				Required:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: ID_DESC,
				Required:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "`v4`, `v6`, or `private_v6`",
				Computed:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: REGION_DESC,
				Computed:            true,
			},
		},
	}
}

func (d *ipDataSourceType) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ipDataSourceOutput
	diags := req.Config.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.Id.ValueString()
	app := data.App.ValueString()
	query, err := graphql.IpAddressByIdQuery(ctx, d.state.GraphqlClient, id)
	tflog.Info(ctx, fmt.Sprintf("Query res: for app %s id %s %+v", app, id, query))
	if err != nil {
		utils.HandleGraphqlErrors(&resp.Diagnostics, err, "Error looking up ip address (app [%s], id [%s])", app, id)
		return
	}

	region := query.IpAddress.Region
	if region == "" {
		region = "global"
	}
	data.Region = types.StringValue(region)
	data.Address = types.StringValue(query.IpAddress.Address)
	data.Type = types.StringValue(string(query.IpAddress.Type))

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
