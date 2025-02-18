package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/andrewbaxter/terraform-provider-fly/machineapi"
	"github.com/andrewbaxter/terraform-provider-fly/providerstate"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &flyMachineResource{}
var _ resource.ResourceWithConfigure = &flyMachineResource{}
var _ resource.ResourceWithImportState = &flyMachineResource{}

type flyMachineResource struct {
	state *providerstate.State
}

func NewMachineResource() resource.Resource {
	return &flyMachineResource{}
}

func (r *flyMachineResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "fly_machine"
}

func (r *flyMachineResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.state = req.ProviderData.(*providerstate.State)
}

type TfPort struct {
	Port       types.Int64    `tfsdk:"port"`
	StartPort  types.Int64    `tfsdk:"start_port"`
	EndPort    types.Int64    `tfsdk:"end_port"`
	Handlers   []types.String `tfsdk:"handlers"`
	ForceHttps types.Bool     `tfsdk:"force_https"`
}

type TfService struct {
	Ports        []TfPort     `tfsdk:"ports"`
	Protocol     types.String `tfsdk:"protocol"`
	InternalPort types.Int64  `tfsdk:"internal_port"`
}

type flyMachineResourceData struct {
	Name        types.String `tfsdk:"name"`
	Region      types.String `tfsdk:"region"`
	Id          types.String `tfsdk:"id"`
	PrivateIP   types.String `tfsdk:"private_ip"`
	App         types.String `tfsdk:"app"`
	Image       types.String `tfsdk:"image"`
	Cpus        types.Int64  `tfsdk:"cpus"`
	MemoryMb    types.Int64  `tfsdk:"memory"`
	CpuType     types.String `tfsdk:"cpu_type"`
	Env         types.Map    `tfsdk:"env"`
	Cmd         []string     `tfsdk:"cmd"`
	Entrypoint  []string     `tfsdk:"entrypoint"`
	Exec        []string     `tfsdk:"exec"`
	AutoDestroy types.Bool   `tfsdk:"auto_destroy"`

	Mounts   []TfMachineMount `tfsdk:"mounts"`
	Services []TfService      `tfsdk:"services"`
}

type TfMachineMount struct {
	Path   types.String `tfsdk:"path"`
	Volume types.String `tfsdk:"volume"`
}

func (r *flyMachineResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: NAME_DESC,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: REGION_DESC,
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: ID_DESC,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"app": schema.StringAttribute{
				MarkdownDescription: APP_DESC,
				Required:            true,
			},
			"private_ip": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"cmd": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
			},
			"entrypoint": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
			},
			"exec": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
			},
			"image": schema.StringAttribute{
				MarkdownDescription: "Protocol-less docker image, ex: `registry.fly.io/myapp:mytag`",
				Required:            true,
			},
			"cpu_type": schema.StringAttribute{
				MarkdownDescription: "Which machine flavor, ex: `shared`",
				Computed:            true,
				Optional:            true,
			},
			"cpus": schema.Int64Attribute{
				Computed: true,
				Optional: true,
			},
			"memory": schema.Int64Attribute{
				MarkdownDescription: "Amount of memory in MB. `256`, `512`, `1024`, ...",
				Computed:            true,
				Optional:            true,
			},
			"env": schema.MapAttribute{
				MarkdownDescription: "Keys and values must be strings",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
			},
			"auto_destroy": schema.BoolAttribute{
				MarkdownDescription: "Optional boolean telling the Machine to destroy itself once it's complete",
				Computed:            true,
				Optional:            true,
				Default:             booldefault.StaticBool(false),
			},
			"mounts": schema.ListNestedAttribute{
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"path": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Path for volume to be mounted on vm, ex: `/data`",
						},
						"volume": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "ID of volume",
							PlanModifiers: []planmodifier.String{
								stringplanmodifier.RequiresReplace(),
							},
						},
					},
				},
			},
			"services": schema.ListNestedAttribute{
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"ports": schema.ListNestedAttribute{
							MarkdownDescription: "How the port is exposed",
							Required:            true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"port": schema.Int64Attribute{
										MarkdownDescription: "Mapped external port number, either `port` or `start_port` and `end_port` must be set.",
										Optional:            true,
										Validators: []validator.Int64{
											int64validator.Between(1, 65535),
											int64validator.ConflictsWith(path.Expressions{path.MatchRelative().AtParent().AtName("start_port")}...),
											int64validator.ConflictsWith(path.Expressions{path.MatchRelative().AtParent().AtName("end_port")}...),
										},
									},
									"start_port": schema.Int64Attribute{
										MarkdownDescription: "For a port range, the first port to listen on.",
										Optional:            true,
										Validators: []validator.Int64{
											int64validator.Between(1, 65535),
											int64validator.AlsoRequires(path.Expressions{path.MatchRelative().AtParent().AtName("end_port")}...),
											int64validator.ConflictsWith(path.Expressions{path.MatchRelative().AtParent().AtName("port")}...),
										},
									},
									"end_port": schema.Int64Attribute{
										MarkdownDescription: "For a port range, the last port to listen on",
										Optional:            true,
										Validators: []validator.Int64{
											int64validator.Between(1, 65535),
											int64validator.AlsoRequires(path.Expressions{path.MatchRelative().AtParent().AtName("start_port")}...),
											int64validator.ConflictsWith(path.Expressions{path.MatchRelative().AtParent().AtName("port")}...),
										},
									},
									"handlers": schema.ListAttribute{
										MarkdownDescription: "How the edge should process requests; ex empty, or `tls` to attach app's certificate",
										Optional:            true,
										ElementType:         types.StringType,
									},
									"force_https": schema.BoolAttribute{
										MarkdownDescription: "Automatically redirect to HTTPS on \"http\" handler",
										Computed:            true,
										Optional:            true,
										Default:             booldefault.StaticBool(false),
									},
								},
							},
						},
						"protocol": schema.StringAttribute{
							MarkdownDescription: "`udp` or `tcp`",
							Required:            true,
						},
						"internal_port": schema.Int64Attribute{
							MarkdownDescription: "Port the machine listens on",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

func TfServicesToServices(input []TfService) []machineapi.Service {
	services := make([]machineapi.Service, 0)
	for _, s := range input {
		var ports []machineapi.Port
		for _, j := range s.Ports {
			var handlers []string
			for _, k := range j.Handlers {
				handlers = append(handlers, k.ValueString())
			}
			port := machineapi.Port{
				Handlers:   handlers,
				ForceHttps: j.ForceHttps.ValueBool(),
			}
			portValue := j.Port.ValueInt64Pointer()
			if portValue != nil {
				port.Port = portValue
			} else {
				port.StartPort = j.StartPort.ValueInt64Pointer()
				port.EndPort = j.EndPort.ValueInt64Pointer()
			}
			ports = append(ports, port)
		}
		services = append(services, machineapi.Service{
			Ports:        ports,
			Protocol:     s.Protocol.ValueString(),
			InternalPort: s.InternalPort.ValueInt64(),
		})
	}
	return services
}

func ServicesToTfServices(input []machineapi.Service) []TfService {
	tfservices := make([]TfService, 0)
	for _, s := range input {
		tfports := []TfPort{}
		for _, j := range s.Ports {
			var handlers []types.String
			for _, k := range j.Handlers {
				handlers = append(handlers, types.StringValue(k))
			}
			tfports = append(tfports, TfPort{
				Port:       types.Int64PointerValue(j.Port),
				Handlers:   handlers,
				ForceHttps: types.BoolValue(j.ForceHttps),
				StartPort:  types.Int64PointerValue(j.StartPort),
				EndPort:    types.Int64PointerValue(j.EndPort),
			})
		}
		tfservices = append(tfservices, TfService{
			Ports:        tfports,
			Protocol:     types.StringValue(s.Protocol),
			InternalPort: types.Int64Value(s.InternalPort),
		})
	}
	return tfservices
}

func (r *flyMachineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data flyMachineResourceData

	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	services := TfServicesToServices(data.Services)
	createReq := machineapi.MachineCreateOrUpdateRequest{
		Name:   data.Name.ValueString(),
		Region: data.Region.ValueString(),
		Config: machineapi.MachineConfig{
			Image:    data.Image.ValueString(),
			Services: services,
			Init: machineapi.InitConfig{
				Cmd:        data.Cmd,
				Entrypoint: data.Entrypoint,
				Exec:       data.Exec,
			},
			AutoDestroy: data.AutoDestroy.ValueBool(),
		},
	}

	if !data.Cpus.IsUnknown() {
		createReq.Config.Guest.Cpus = int(data.Cpus.ValueInt64())
	}
	if !data.CpuType.IsUnknown() {
		createReq.Config.Guest.CpuType = data.CpuType.ValueString()
	}
	if !data.MemoryMb.IsUnknown() {
		createReq.Config.Guest.MemoryMb = int(data.MemoryMb.ValueInt64())
	}

	if !data.Env.IsUnknown() {
		var env map[string]string
		data.Env.ElementsAs(ctx, &env, false)
		createReq.Config.Env = env
	}
	if len(data.Mounts) > 0 {
		var mounts []machineapi.MachineMount
		for _, m := range data.Mounts {
			mounts = append(mounts, machineapi.MachineMount{
				Path:   m.Path.ValueString(),
				Volume: m.Volume.ValueString(),
			})
		}
		createReq.Config.Mounts = mounts
	}

	machineApi := machineapi.NewMachineApi(ctx, r.state)

	newMachine, err := machineApi.CreateMachine(ctx, createReq, data.App.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create machine", err.Error())
		return
	}

	tflog.Info(ctx, fmt.Sprintf("%+v", newMachine))

	// env := utils.KVToTfMap(newMachine.Config.Env, types.StringType)
	env, diags := types.MapValueFrom(ctx, types.StringType, newMachine.Config.Env)
	resp.Diagnostics.Append(diags...)

	tfservices := ServicesToTfServices(newMachine.Config.Services)

	if data.Services == nil && len(tfservices) == 0 {
		tfservices = nil
	}

	data = flyMachineResourceData{
		Name:        types.StringValue(newMachine.Name),
		Region:      types.StringValue(newMachine.Region),
		Id:          types.StringValue(newMachine.ID),
		App:         data.App,
		PrivateIP:   types.StringValue(newMachine.PrivateIP),
		Image:       types.StringValue(newMachine.Config.Image),
		Cpus:        types.Int64Value(int64(newMachine.Config.Guest.Cpus)),
		MemoryMb:    types.Int64Value(int64(newMachine.Config.Guest.MemoryMb)),
		CpuType:     types.StringValue(newMachine.Config.Guest.CPUKind),
		Cmd:         newMachine.Config.Init.Cmd,
		Entrypoint:  newMachine.Config.Init.Entrypoint,
		Exec:        newMachine.Config.Init.Exec,
		Env:         env,
		Services:    tfservices,
		AutoDestroy: types.BoolValue(newMachine.Config.AutoDestroy),
	}

	if len(newMachine.Config.Mounts) > 0 {
		var tfmounts []TfMachineMount
		for _, m := range newMachine.Config.Mounts {
			tfmounts = append(tfmounts, TfMachineMount{
				Path:   types.StringValue(m.Path),
				Volume: types.StringValue(m.Volume),
			})
		}
		data.Mounts = tfmounts
	}

	err = machineApi.WaitForMachine(ctx, data.App.ValueString(), data.Id.ValueString(), newMachine.InstanceID)
	if err != nil {
		//FIXME(?): For now we just assume that the orchestrator is in fact going to faithfully execute our request
		tflog.Info(ctx, "Waiting errored")
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *flyMachineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data flyMachineResourceData

	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)

	machineApi := machineapi.NewMachineApi(ctx, r.state)

	machine, err := machineApi.ReadMachine(ctx, data.App.ValueString(), data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create machine", err.Error())
		return
	}

	// env := utils.KVToTfMap(machine.Config.Env, types.StringType)
	env, diags := types.MapValueFrom(ctx, types.StringType, machine.Config.Env)
	resp.Diagnostics.Append(diags...)

	tfservices := ServicesToTfServices(machine.Config.Services)

	if data.Services == nil && len(tfservices) == 0 {
		tfservices = nil
	}

	data = flyMachineResourceData{
		Name:        types.StringValue(machine.Name),
		Id:          types.StringValue(machine.ID),
		Region:      types.StringValue(machine.Region),
		App:         data.App,
		PrivateIP:   types.StringValue(machine.PrivateIP),
		Image:       types.StringValue(machine.Config.Image),
		Cpus:        types.Int64Value(int64(machine.Config.Guest.Cpus)),
		MemoryMb:    types.Int64Value(int64(machine.Config.Guest.MemoryMb)),
		CpuType:     types.StringValue(machine.Config.Guest.CPUKind),
		Cmd:         machine.Config.Init.Cmd,
		Entrypoint:  machine.Config.Init.Entrypoint,
		Exec:        machine.Config.Init.Exec,
		Env:         env,
		Services:    tfservices,
		AutoDestroy: types.BoolValue(machine.Config.AutoDestroy),
	}

	if len(machine.Config.Mounts) > 0 {
		var tfmounts []TfMachineMount
		for _, m := range machine.Config.Mounts {
			tfmounts = append(tfmounts, TfMachineMount{
				Path:   types.StringValue(m.Path),
				Volume: types.StringValue(m.Volume),
			})
		}
		data.Mounts = tfmounts
	}

	diags = resp.State.Set(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *flyMachineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan flyMachineResourceData

	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state flyMachineResourceData
	diags = resp.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Name.IsUnknown() && plan.Name.ValueString() != state.Name.ValueString() {
		resp.Diagnostics.AddError("Can't mutate name of existing machine", "Can't switch name "+state.Name.ValueString()+" to "+plan.Name.ValueString())
	}
	if !state.Region.IsUnknown() && plan.Region.ValueString() != state.Region.ValueString() {
		resp.Diagnostics.AddError("Can't mutate region of existing machine", "Can't switch region "+state.Name.ValueString()+" to "+plan.Name.ValueString())
	}

	services := TfServicesToServices(plan.Services)

	updateReq := machineapi.MachineCreateOrUpdateRequest{
		Name:   plan.Name.ValueString(),
		Region: state.Region.ValueString(),
		Config: machineapi.MachineConfig{
			Image:    plan.Image.ValueString(),
			Services: services,
			Init: machineapi.InitConfig{
				Cmd:        plan.Cmd,
				Entrypoint: plan.Entrypoint,
				Exec:       plan.Exec,
			},
			AutoDestroy: state.AutoDestroy.ValueBool(),
		},
	}

	if !plan.Cpus.IsUnknown() {
		updateReq.Config.Guest.Cpus = int(plan.Cpus.ValueInt64())
	}
	if !plan.CpuType.IsUnknown() {
		updateReq.Config.Guest.CpuType = plan.CpuType.ValueString()
	}
	if !plan.MemoryMb.IsUnknown() {
		updateReq.Config.Guest.MemoryMb = int(plan.MemoryMb.ValueInt64())
	}
	if plan.Env.IsNull() {
		env := map[string]string{}
		updateReq.Config.Env = env
	} else if !plan.Env.IsUnknown() {
		var env map[string]string
		plan.Env.ElementsAs(ctx, &env, false)
		updateReq.Config.Env = env
	} else if !state.Env.IsUnknown() {
		updateReq.Config.Env = map[string]string{}
	}

	if len(plan.Mounts) > 0 {
		var mounts []machineapi.MachineMount
		for _, m := range plan.Mounts {
			mounts = append(mounts, machineapi.MachineMount{
				Path:   m.Path.ValueString(),
				Volume: m.Volume.ValueString(),
			})
		}
		updateReq.Config.Mounts = mounts
	}

	machineApi := machineapi.NewMachineApi(ctx, r.state)

	var updatedMachine machineapi.MachineResponse

	err := machineApi.UpdateMachine(ctx, updateReq, state.App.ValueString(), state.Id.ValueString(), &updatedMachine)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update machine", err.Error())
		return
	}

	// env := utils.KVToTfMap(updatedMachine.Config.Env, types.StringType)
	env, diags := types.MapValueFrom(ctx, types.StringType, updatedMachine.Config.Env)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tfservices := ServicesToTfServices(updatedMachine.Config.Services)

	if len(tfservices) == 0 {
		tfservices = nil
	}

	state = flyMachineResourceData{
		Name:        types.StringValue(updatedMachine.Name),
		Region:      types.StringValue(updatedMachine.Region),
		Id:          types.StringValue(updatedMachine.ID),
		App:         state.App,
		PrivateIP:   types.StringValue(updatedMachine.PrivateIP),
		Image:       types.StringValue(updatedMachine.Config.Image),
		Cpus:        types.Int64Value(int64(updatedMachine.Config.Guest.Cpus)),
		MemoryMb:    types.Int64Value(int64(updatedMachine.Config.Guest.MemoryMb)),
		CpuType:     types.StringValue(updatedMachine.Config.Guest.CPUKind),
		Cmd:         updatedMachine.Config.Init.Cmd,
		Entrypoint:  updatedMachine.Config.Init.Entrypoint,
		Exec:        updatedMachine.Config.Init.Exec,
		Env:         env,
		Services:    tfservices,
		AutoDestroy: types.BoolValue(updatedMachine.Config.AutoDestroy),
	}

	if len(updatedMachine.Config.Mounts) > 0 {
		var tfmounts []TfMachineMount
		for _, m := range updatedMachine.Config.Mounts {
			tfmounts = append(tfmounts, TfMachineMount{
				Path:   types.StringValue(m.Path),
				Volume: types.StringValue(m.Volume),
			})
		}
		state.Mounts = tfmounts
	}

	err = machineApi.WaitForMachine(ctx, state.App.ValueString(), state.Id.ValueString(), updatedMachine.InstanceID)
	if err != nil {
		tflog.Info(ctx, "Waiting errored")
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *flyMachineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data flyMachineResourceData
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	machineApi := machineapi.NewMachineApi(ctx, r.state)

	err := machineApi.DeleteMachine(ctx, data.App.ValueString(), data.Id.ValueString(), 50)
	if err != nil {
		resp.Diagnostics.AddError("Machine delete failed", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (mr flyMachineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: app_id,machine_id. Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("app"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
}
