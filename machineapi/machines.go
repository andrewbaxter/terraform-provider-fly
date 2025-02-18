package machineapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/andrewbaxter/terraform-provider-fly/providerstate"
	"github.com/andrewbaxter/terraform-provider-fly/utils"
	"github.com/hashicorp/go-hclog"
	hreq "github.com/imroc/req/v3"
	"github.com/superfly/flyctl/api"
)

var NonceHeader = "fly-machine-lease-nonce"

type MachineAPI struct {
	HttpClient *hreq.Client
	baseUrl    string
}

type MachineMount struct {
	Encrypted bool   `json:"encrypted,omitempty"`
	Path      string `json:"path"`
	SizeGb    int    `json:"size_gb,omitempty"`
	Volume    string `json:"volume"`
}

type Port struct {
	Port       *int64   `json:"port"`
	StartPort  *int64   `json:"start_port"`
	EndPort    *int64   `json:"end_port"`
	Handlers   []string `json:"handlers"`
	ForceHttps bool     `json:"force_https"`
}

type Service struct {
	Ports        []Port `json:"ports"`
	Protocol     string `json:"protocol"`
	InternalPort int64  `json:"internal_port"`
}

type InitConfig struct {
	Cmd        []string `json:"cmd,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	Exec       []string `json:"exec,omitempty"`
}

type MachineConfig struct {
	Image       string            `json:"image"`
	Env         map[string]string `json:"env"`
	Init        InitConfig        `json:"init,omitempty"`
	Mounts      []MachineMount    `json:"mounts,omitempty"`
	Services    []Service         `json:"services"`
	Guest       GuestConfig       `json:"guest,omitempty"`
	AutoDestroy bool              `json:"auto_destroy"`
}

type GuestConfig struct {
	Cpus     int    `json:"cpus,omitempty"`
	MemoryMb int    `json:"memory_mb,omitempty"`
	CpuType  string `json:"cpu_kind,omitempty"`
}

type MachineCreateOrUpdateRequest struct {
	Name   string        `json:"name"`
	Region string        `json:"region"`
	Config MachineConfig `json:"config"`
}

type MachineResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	State      string `json:"state"`
	Region     string `json:"region"`
	InstanceID string `json:"instance_id"`
	PrivateIP  string `json:"private_ip"`
	Config     struct {
		Env  map[string]string `json:"env"`
		Init struct {
			Exec       []string `json:"exec"`
			Entrypoint []string `json:"entrypoint"`
			Cmd        []string `json:"cmd"`
			//Tty        bool        `json:"tty"`
		} `json:"init"`
		Image    string      `json:"image"`
		Metadata interface{} `json:"metadata"`
		Restart  struct {
			Policy string `json:"policy"`
		} `json:"restart"`
		Services []Service      `json:"services"`
		Mounts   []MachineMount `json:"mounts"`
		Guest    struct {
			CPUKind  string `json:"cpu_kind"`
			Cpus     int    `json:"cpus"`
			MemoryMb int    `json:"memory_mb"`
		} `json:"guest"`
		AutoDestroy bool `json:"auto_destroy"`
	} `json:"config"`
	ImageRef struct {
		Registry   string `json:"registry"`
		Repository string `json:"repository"`
		Tag        string `json:"tag"`
		Digest     string `json:"digest"`
		Labels     struct {
		} `json:"labels"`
	} `json:"image_ref"`
	CreatedAt time.Time `json:"created_at"`
}

type MachineLease struct {
	Status string `json:"status"`
	Data   struct {
		Nonce     string `json:"nonce"`
		ExpiresAt int64  `json:"expires_at"`
		Owner     string `json:"owner"`
	}
}

func NewMachineApi(ctx context.Context, state *providerstate.State) *MachineAPI {
	endpoint := state.RestBaseUrl
	token := state.Token
	httpClient := hreq.C()
	httpClient.Transport.WrapRoundTrip(func(rt http.RoundTripper) http.RoundTripper {
		return &utils.LoggingHttpTransport{
			Inner: rt,
		}
	})

	httpClient.SetCommonHeader("Authorization", "Bearer "+token)
	httpClient.SetTimeout(2 * time.Minute)

	// Include body in errors
	httpClient.OnAfterResponse(func(client *hreq.Client, resp *hreq.Response) error {
		if resp.Err != nil {
			if dump := resp.Dump(); dump != "" {
				resp.Err = fmt.Errorf("%s\nraw content:\n%s", resp.Err.Error(), resp.Dump())
				resp.Err = fmt.Errorf("got error doing %s %s: %s\nbody:\n%s", resp.Request.Method, resp.Request.RawURL, resp.Err, resp.Dump())
			}
			return nil
		}

		// Return a human-readable error if server api returned an error message.
		// if err, ok := resp.ErrorResult().(*APIError); ok {
		//    resp.Err = err
		//    return nil
		// }

		if !resp.IsSuccessState() {
			resp.Dump()
			resp.Err = fmt.Errorf("got error response from %s %s: %s\nbody:\n%s", resp.Request.Method, resp.Request.RawURL, resp.Status, resp.Dump())
			return nil
		}
		return nil
	})

	out := &MachineAPI{
		HttpClient: httpClient,
		baseUrl:    endpoint,
	}
	if state.EnableTracing {
		out.HttpClient.SetCommonHeader("Fly-Force-Trace", "true")
		out.HttpClient.DevMode()
	}
	return out
}

func reqNoBody[O any](ctx context.Context, a *MachineAPI, url string, method string) (*O, error) {
	return reqFull[any, O](ctx, a, url, method, nil, nil)
}

func req[I any, O any](ctx context.Context, a *MachineAPI, url string, method string, reqBody I) (*O, error) {
	return reqFull[I, O](ctx, a, url, method, nil, &reqBody)
}

func reqFull[I any, O any](ctx context.Context, a *MachineAPI, url string, method string, headers map[string]string, reqBody *I) (*O, error) {
	var res O
	var errBody any
	req := a.HttpClient.R().SetContext(ctx)
	if headers != nil {
		req.
			SetHeaders(headers)
	}
	if reqBody != nil {
		req.SetBody(*reqBody)
	}
	req.SetSuccessResult(&res)
	req.SetErrorResult(&errBody)
	_, err := req.Send(method, url)
	if err != nil {
		errBodyJson, _ := json.MarshalIndent(errBody, "", "   ")
		return nil, fmt.Errorf("API error [%s] to [%s]: %s\nResponse body: %s", url, method, err, string(errBodyJson))
	}
	return &res, nil
}

func (a *MachineAPI) LockMachine(ctx context.Context, app string, id string, timeout int) (*MachineLease, error) {
	return reqNoBody[MachineLease](ctx, a, fmt.Sprintf("%s/v1/apps/%s/machines/%s/lease?ttl=%d", a.baseUrl, app, id, timeout), http.MethodPost)
}

func (a *MachineAPI) ReleaseMachine(ctx context.Context, lease MachineLease, app string, id string) error {
	_, err := reqNoBody[any](ctx, a, fmt.Sprintf("%s/v1/apps/%s/machines/%s/lease", a.baseUrl, app, id), http.MethodDelete)
	return err
}

func (a *MachineAPI) WaitForMachine(ctx context.Context, app string, id string, instanceID string) error {
	_, err := reqNoBody[any](ctx, a, fmt.Sprintf("%s/v1/apps/%s/machines/%s/wait?instance_id=%s", a.baseUrl, app, id, instanceID), http.MethodGet)
	return err
}

// CreateMachine takes a MachineCreateOrUpdateRequest and creates the requested machine in the given app and then writes the response into the `res` param
func (a *MachineAPI) CreateMachine(ctx context.Context, reqBody MachineCreateOrUpdateRequest, app string) (*MachineResponse, error) {
	if reqBody.Config.Guest.CpuType == "" {
		reqBody.Config.Guest.CpuType = "shared"
	}
	if reqBody.Config.Guest.Cpus == 0 {
		reqBody.Config.Guest.Cpus = 1
	}
	if reqBody.Config.Guest.MemoryMb == 0 {
		reqBody.Config.Guest.MemoryMb = 256
	}
	return req[MachineCreateOrUpdateRequest, MachineResponse](ctx, a, fmt.Sprintf("%s/v1/apps/%s/machines", a.baseUrl, app), http.MethodPost, reqBody)
}

func (a *MachineAPI) UpdateMachine(ctx context.Context, reqBody MachineCreateOrUpdateRequest, app string, id string, res *MachineResponse) error {
	if reqBody.Config.Guest.CpuType == "" {
		reqBody.Config.Guest.CpuType = "shared"
	}
	if reqBody.Config.Guest.Cpus == 0 {
		//You can't have a machine with no cpus
		reqBody.Config.Guest.Cpus = 1
	}
	if reqBody.Config.Guest.MemoryMb == 0 {
		//You can't have a machine with no memory
		reqBody.Config.Guest.MemoryMb = 256
	}
	lease, err := a.LockMachine(ctx, app, id, 30)
	if err != nil {
		return err
	}
	defer func() {
		err := a.ReleaseMachine(ctx, *lease, app, id)
		if err != nil {
			hclog.Default().Error("Error releasing lock on app [%s] machine [%s]: %s", app, id, err)
		}
	}()
	_, err = reqFull[MachineCreateOrUpdateRequest, MachineResponse](
		ctx,
		a,
		fmt.Sprintf("%s/v1/apps/%s/machines/%s", a.baseUrl, app, id),
		http.MethodPost,
		map[string]string{
			NonceHeader: lease.Data.Nonce,
		},
		&reqBody,
	)
	if err != nil {
		return err
	}
	return nil
}

func (a *MachineAPI) ReadMachine(ctx context.Context, app string, id string) (*MachineResponse, error) {
	return reqNoBody[MachineResponse](ctx, a, fmt.Sprintf("%s/v1/apps/%s/machines/%s", a.baseUrl, app, id), http.MethodGet)
}

func (a *MachineAPI) DeleteMachine(ctx context.Context, app string, id string, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		machine, err := reqNoBody[MachineResponse](ctx, a, fmt.Sprintf("%s/v1/apps/%s/machines/%s", a.baseUrl, app, id), http.MethodGet)
		if err != nil {
			return err
		}

		switch machine.State {
		case "started", "starting":
			_, err := reqNoBody[MachineResponse](ctx, a, fmt.Sprintf("%s/v1/apps/%s/machines/%s/stop", a.baseUrl, app, id), http.MethodPost)
			if err != nil {
				return err
			}
		case "stopping", "destroying", "replacing":
			time.Sleep(5 * time.Second)
		case "stopped", "replaced":
			_, err := reqNoBody[MachineResponse](ctx, a, fmt.Sprintf("%s/v1/apps/%s/machines/%s", a.baseUrl, app, id), http.MethodDelete)
			if err != nil {
				return err
			}
		case "destroyed":
			return nil
		}
	}
	return fmt.Errorf("reached max attempts while trying to delete app %s machine %s", app, id)
}

func (a *MachineAPI) CreateVolume(ctx context.Context, name, app, region string, size int) (*api.Volume, error) {
	return req[api.CreateVolumeRequest, api.Volume](
		ctx,
		a,
		fmt.Sprintf("%s/v1/apps/%s/volumes", a.baseUrl, app),
		http.MethodPost, api.CreateVolumeRequest{
			Name:   name,
			Region: region,
			SizeGb: &size,
		},
	)
}

func (a *MachineAPI) GetVolume(ctx context.Context, id, app string) (*api.Volume, error) {
	return reqNoBody[api.Volume](ctx, a, fmt.Sprintf("%s/v1/apps/%s/volumes/%s", a.baseUrl, app, id), http.MethodGet)
}

func (a *MachineAPI) ExtendVolume(ctx context.Context, app string, id string, size int) error {
	_, err := req[map[string]any, any](ctx, a, fmt.Sprintf("%s/v1/apps/%s/volumes/%s/extend", a.baseUrl, app, id), http.MethodPut, map[string]any{
		"size_gb": size,
	})
	return err
}

func (a *MachineAPI) DeleteVolume(ctx context.Context, app string, id string) error {
	_, err := reqNoBody[any](ctx, a, fmt.Sprintf("%s/v1/apps/%s/volumes/%s", a.baseUrl, app, id), http.MethodDelete)
	return err
}
