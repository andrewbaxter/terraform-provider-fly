package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/andrewbaxter/terraform-provider-fly/graphql"
	"github.com/andrewbaxter/terraform-provider-fly/providerstate"
	"github.com/andrewbaxter/terraform-provider-fly/utils"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &flyIpResource{}
var _ resource.ResourceWithConfigure = &flyIpResource{}
var _ resource.ResourceWithImportState = &flyIpResource{}

type flyIpResource struct {
	state *providerstate.State
}

func NewIpResource() resource.Resource {
	return &flyIpResource{}
}

func (r *flyIpResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "fly_ip"
}

func (r *flyIpResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.state = req.ProviderData.(*providerstate.State)
}

type flyIpResourceData struct {
	Id      types.String `tfsdk:"id"`
	App     types.String `tfsdk:"app"`
	Region  types.String `tfsdk:"region"`
	Address types.String `tfsdk:"address"`
	Type    types.String `tfsdk:"type"`
}

func (r *flyIpResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"address": schema.StringAttribute{
				MarkdownDescription: "Empty if using `shared_v4`",
				Computed:            true,
			},
			"app": schema.StringAttribute{
				MarkdownDescription: APP_DESC,
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: ID_DESC,
				Computed:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: ADDRESS_TYPE_DESC,
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(ADDRESS_TYPE_REGEX, ADDRESS_TYPE_DESC),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: REGION_DESC,
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("global"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *flyIpResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data flyIpResourceData
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	app := data.App.ValueString()
	region := data.Region.ValueString()
	type_ := graphql.IPAddressType(data.Type.ValueString())
	q, err := graphql.AllocateIpAddress(ctx, r.state.GraphqlClient, app, region, type_)
	if err != nil {
		utils.HandleGraphqlErrors(&resp.Diagnostics, err, "Error creating ip address (app [%s], region [%s], type [%s])", app, region, type_)
		return
	}

	data.Id = types.StringValue(q.AllocateIpAddress.IpAddress.Id)
	data.App = types.StringValue(data.App.ValueString())
	data.Address = types.StringValue(q.AllocateIpAddress.IpAddress.Address)
	if q.AllocateIpAddress.IpAddress.Region != "" {
		data.Region = types.StringValue(q.AllocateIpAddress.IpAddress.Region)
	}
	if q.AllocateIpAddress.IpAddress.Type != "" {
		data.Type = types.StringValue(string(q.AllocateIpAddress.IpAddress.Type))
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r flyIpResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data flyIpResourceData
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	addr := data.Address.ValueString()
	app := data.App.ValueString()
	query, err := graphql.IpAddressQuery(ctx, r.state.GraphqlClient, app, addr)
	if err != nil {
		utils.HandleGraphqlErrors(&resp.Diagnostics, err, "Error looking up ip address (app [%s], addr [%s])", app, addr)
		return
	}

	data.Id = types.StringValue(query.App.IpAddress.Id)
	data.Address = types.StringValue(query.App.IpAddress.Address)
	if query.App.IpAddress.Region != "" {
		data.Region = types.StringValue(query.App.IpAddress.Region)
	}
	if query.App.IpAddress.Type != "" {
		data.Type = types.StringValue(string(query.App.IpAddress.Type))
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *flyIpResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"The fly api does not allow updating ips once created",
		"Try deleting and then recreating the ip with new options",
	)
}

func (r *flyIpResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data flyIpResourceData

	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !data.Id.IsUnknown() && !data.Id.IsNull() && data.Id.ValueString() != "" {
		app := data.App.ValueString()
		ip := data.Address.ValueString()
		_, err := graphql.ReleaseIpAddress(ctx, r.state.GraphqlClient, app, ip)
		if err != nil {
			utils.HandleGraphqlErrors(&resp.Diagnostics, err, "Error deleting ip address (app [%s], ip [%s])", app, ip)
			return
		}
	}

	resp.State.RemoveResource(ctx)
}

func (r *flyIpResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected import identifier when trying to import ip address",
			fmt.Sprintf("Expected import identifier with format: app_id,ip_address. Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("address"), idParts[1])...)
}
