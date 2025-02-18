package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/andrewbaxter/terraform-provider-fly/graphql"
	"github.com/andrewbaxter/terraform-provider-fly/providerstate"
	"github.com/andrewbaxter/terraform-provider-fly/utils"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &flyCertResource{}
var _ resource.ResourceWithConfigure = &flyCertResource{}
var _ resource.ResourceWithImportState = &flyCertResource{}

type flyCertResource struct {
	state *providerstate.State
}

func NewCertResource() resource.Resource {
	return &flyCertResource{}
}

func (r *flyCertResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "fly_cert"
}

func (r *flyCertResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.state = req.ProviderData.(*providerstate.State)
}

type flyCertResourceData struct {
	App                       types.String `tfsdk:"app"`
	Id                        types.String `tfsdk:"id"`
	DnsValidationInstructions types.String `tfsdk:"dns_validation_instructions"`
	DnsValidationHostname     types.String `tfsdk:"dns_validation_hostname"`
	DnsValidationTarget       types.String `tfsdk:"dns_validation_target"`
	Hostname                  types.String `tfsdk:"hostname"`
	Check                     types.Bool   `tfsdk:"check"`
}

func (r *flyCertResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Fly certificate resource",
		Attributes: map[string]schema.Attribute{
			// Key
			"app": schema.StringAttribute{
				MarkdownDescription: APP_DESC,
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"hostname": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			// RO
			"id": schema.StringAttribute{
				MarkdownDescription: ID_DESC,
				Computed:            true,
			},
			"dns_validation_instructions": schema.StringAttribute{
				Computed: true,
			},
			"dns_validation_target": schema.StringAttribute{
				Computed: true,
			},
			"dns_validation_hostname": schema.StringAttribute{
				Computed: true,
			},
			"check": schema.BoolAttribute{
				Computed: true,
			},
		},
	}
}

func (r *flyCertResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data flyCertResourceData
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	app := data.App.ValueString()
	hostname := data.Hostname.ValueString()
	q, err := graphql.AddCertificate(ctx, r.state.GraphqlClient, app, hostname)
	if err != nil {
		utils.HandleGraphqlErrors(&resp.Diagnostics, err, "Error creating cert (app [%s], hostname [%s])", app, hostname)
		return
	}

	data.Id = types.StringValue(q.AddCertificate.Certificate.Id)
	data.DnsValidationInstructions = types.StringValue(q.AddCertificate.Certificate.DnsValidationInstructions)
	data.DnsValidationHostname = types.StringValue(q.AddCertificate.Certificate.DnsValidationHostname)
	data.DnsValidationTarget = types.StringValue(q.AddCertificate.Certificate.DnsValidationTarget)
	data.Check = types.BoolValue(q.AddCertificate.Certificate.Check)

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *flyCertResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data flyCertResourceData
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	hostname := data.Hostname.ValueString()
	app := data.App.ValueString()
	query, err := graphql.GetCertificate(ctx, r.state.GraphqlClient, app, hostname)
	if err != nil {
		utils.HandleGraphqlErrors(&resp.Diagnostics, err, "Error looking up cert (app [%s], hostname [%s])", app, hostname)
		return
	}

	data.DnsValidationInstructions = types.StringValue(query.App.Certificate.DnsValidationInstructions)
	data.DnsValidationHostname = types.StringValue(query.App.Certificate.DnsValidationHostname)
	data.DnsValidationTarget = types.StringValue(query.App.Certificate.DnsValidationTarget)
	data.Check = types.BoolValue(query.App.Certificate.Check)

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (cr *flyCertResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("The fly api does not allow updating certs once created", "Try deleting and then recreating the cert with new options")
	// We could maybe instead flag every attribute with RequiresReplace?
}

func (r *flyCertResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data flyCertResourceData

	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	app := data.App.ValueString()
	hostname := data.Hostname.ValueString()
	_, err := graphql.DeleteCertificate(ctx, r.state.GraphqlClient, app, hostname)
	if err != nil {
		utils.HandleGraphqlErrors(&resp.Diagnostics, err, "Error deleting cert (app [%s], hostname [%s])", app, hostname)
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *flyCertResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: app_id,hostname. Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("hostname"), idParts[1])...)
}
