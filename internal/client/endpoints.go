package client

import (
	"context"
	"errors"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/dto/input/query"
)

func (c *Client) CreateProject(ctx context.Context, id string, v ProjectCreate) (Project, error) {
	ctx, state := c.operation(ctx, "CreateProject")
	type pDTO path.ClientId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Project{}, err
	}
	type bDTO body.PostProject
	b, err := input[bDTO](v)
	if err != nil {
		return Project{}, err
	}
	r, outer := c.sdk.PostClientProject(ctx, path.ClientId(p), body.PostProject(b))
	value, err := finish[Project](state, r, outer)
	return value, err
}

func (c *Client) GetProject(ctx context.Context, id string) (Project, error) {
	ctx, state := c.operation(ctx, "GetProject")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Project{}, err
	}
	r, outer := c.sdk.GetProject(ctx, path.ProjectId(p))
	value, err := finish[Project](state, r, outer)
	return value, err
}

func (c *Client) ListProjects(ctx context.Context, id string, q ProjectQuery) (Page[Project], error) {
	ctx, state := c.operation(ctx, "ListProjects")
	type pDTO path.ClientId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Page[Project]{}, err
	}
	type qwireDTO query.ListClientProjects
	qwire, err := input[qwireDTO](q)
	if err != nil {
		return Page[Project]{}, err
	}
	r, outer := c.sdk.GetClientProject(ctx, path.ClientId(p), query.ListClientProjects(qwire))
	value, err := finish[wirePage[Project]](state, r, outer)
	return value.page(q.Offset, q.Limit), err
}

func (c *Client) UpdateProject(ctx context.Context, id string, v ProjectUpdate) (Project, error) {
	ctx, state := c.operation(ctx, "UpdateProject")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Project{}, err
	}
	type bDTO body.PutProject
	b, err := input[bDTO](v)
	if err != nil {
		return Project{}, err
	}
	r, outer := c.sdk.PutProject(ctx, path.ProjectId(p), body.PutProject(b))
	value, err := finish[Project](state, r, outer)
	if e, ok := AsAPIError(err); ok && e.HTTPStatus == 500 && e.Code == "500" && v.MaxCreditLimit == nil {
		return value, errors.New("project probably has a credit limit set outside Terraform; set max_credit_limit")
	}
	return value, err
}

func (c *Client) UpgradeProject(ctx context.Context, id string, mode string) (*Process, error) {
	ctx, state := c.operation(ctx, "UpgradeProject")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return nil, err
	}
	type bDTO body.PutProjectModeUpgrade
	b, err := input[bDTO](map[string]any{"mode": mode})
	if err != nil {
		return nil, err
	}
	r, outer := c.sdk.PutProjectModeUpgrade(ctx, path.ProjectId(p), body.PutProjectModeUpgrade(b))
	value, err := finish[processEnvelope](state, r, outer)
	return normalizeProcess(value.Process), err
}

func (c *Client) SetSharedIPv4(ctx context.Context, id string, enabled bool) (Project, error) {
	ctx, state := c.operation(ctx, "SetSharedIPv4")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Project{}, err
	}
	type bDTO body.PutProjectPublicIpV4Shared
	b, err := input[bDTO](map[string]any{"publicIpV4Shared": enabled})
	if err != nil {
		return Project{}, err
	}
	r, outer := c.sdk.PutProjectPublicIpv4Shared(ctx, path.ProjectId(p), body.PutProjectPublicIpV4Shared(b))
	value, err := finish[Project](state, r, outer)
	return value, err
}

func (c *Client) DeleteProject(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "DeleteProject")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.DeleteProject(ctx, path.ProjectId(p))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) ListProjectEnv(ctx context.Context, id string) ([]Env, error) {
	ctx, state := c.operation(ctx, "ListProjectEnv")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return nil, err
	}
	r, outer := c.sdk.GetProjectEnvList(ctx, path.ProjectId(p))
	value, err := finish[itemsEnvelope[Env]](state, r, outer)
	return value.Items, err
}

func (c *Client) GetProjectEnv(ctx context.Context, id string) (Env, error) {
	ctx, state := c.operation(ctx, "GetProjectEnv")
	type pDTO path.ProjectEnvId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Env{}, err
	}
	r, outer := c.sdk.GetProjectEnv(ctx, path.ProjectEnvId(p))
	value, err := finish[Env](state, r, outer)
	return value, err
}

func (c *Client) CreateProjectEnv(ctx context.Context, id string, v EnvWrite) (Process, error) {
	ctx, state := c.operation(ctx, "CreateProjectEnv")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	type bDTO body.ProjectEnvPost
	b, err := input[bDTO](v)
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PostProjectEnv(ctx, path.ProjectId(p), body.ProjectEnvPost(b))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) UpdateProjectEnv(ctx context.Context, id string, v EnvWrite) (Process, error) {
	ctx, state := c.operation(ctx, "UpdateProjectEnv")
	type pDTO path.ProjectEnvId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	type bDTO body.ProjectEnvPut
	b, err := input[bDTO](v)
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PutProjectEnv(ctx, path.ProjectEnvId(p), body.ProjectEnvPut(b))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) DeleteProjectEnv(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "DeleteProjectEnv")
	type pDTO path.ProjectEnvId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.DeleteProjectEnv(ctx, path.ProjectEnvId(p))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) RevealProjectEnv(ctx context.Context, id string) (string, error) {
	ctx, state := c.operation(ctx, "RevealProjectEnv")
	type pDTO path.ProjectEnvId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return "", err
	}
	r, outer := c.sdk.GetProjectEnvReveal(ctx, path.ProjectEnvId(p))
	value, err := finish[textEnvelope](state, r, outer)
	return value.Content, err
}

func (c *Client) GetUserData(ctx context.Context, id string) (Env, error) {
	ctx, state := c.operation(ctx, "GetUserData")
	type pDTO path.UserDataId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Env{}, err
	}
	r, outer := c.sdk.GetUserData(ctx, path.UserDataId(p))
	value, err := finish[Env](state, r, outer)
	return value, err
}

func (c *Client) CreateUserData(ctx context.Context, id string, v EnvWrite) (Process, error) {
	ctx, state := c.operation(ctx, "CreateUserData")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	type bDTO body.UserDataPost
	b, err := input[bDTO](v)
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PostServiceStackUserData(ctx, path.ServiceStackId(p), body.UserDataPost(b))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) UpdateUserData(ctx context.Context, id string, v EnvWrite) (Process, error) {
	ctx, state := c.operation(ctx, "UpdateUserData")
	type pDTO path.UserDataId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	type bDTO body.UserDataPut
	b, err := input[bDTO](v)
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PutUserData(ctx, path.UserDataId(p), body.UserDataPut(b))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) DeleteUserData(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "DeleteUserData")
	type pDTO path.UserDataId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.DeleteUserData(ctx, path.UserDataId(p))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) RevealUserData(ctx context.Context, id string) (string, error) {
	ctx, state := c.operation(ctx, "RevealUserData")
	type pDTO path.UserDataId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return "", err
	}
	r, outer := c.sdk.GetUserDataReveal(ctx, path.UserDataId(p))
	value, err := finish[textEnvelope](state, r, outer)
	return value.Content, err
}

func (c *Client) CreateService(ctx context.Context, id string, v ServiceCreate) (ServiceProcess, error) {
	ctx, state := c.operation(ctx, "CreateService")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return ServiceProcess{}, err
	}
	type bDTO body.PostServiceStack
	b, err := input[bDTO](v)
	if err != nil {
		return ServiceProcess{}, err
	}
	r, outer := c.sdk.PostProjectServiceStack(ctx, path.ProjectId(p), body.PostServiceStack(b))
	value, err := finish[ServiceProcess](state, r, outer)
	normalizeService(&value.Service)
	value.Process = normalizeProcess(value.Process)
	return value, err
}

func (c *Client) GetService(ctx context.Context, id string) (Service, error) {
	ctx, state := c.operation(ctx, "GetService")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Service{}, err
	}
	r, outer := c.sdk.GetServiceStack(ctx, path.ServiceStackId(p))
	value, err := finish[Service](state, r, outer)
	normalizeService(&value)
	return value, err
}

func (c *Client) GetServiceByName(ctx context.Context, id string, name string) (Service, error) {
	ctx, state := c.operation(ctx, "GetServiceByName")
	type pDTO path.GetServiceStackByName
	p, err := input[pDTO](map[string]any{"ProjectId": id, "Name": name})
	if err != nil {
		return Service{}, err
	}
	r, outer := c.sdk.GetServiceStackByName(ctx, path.GetServiceStackByName(p))
	value, err := finish[Service](state, r, outer)
	normalizeService(&value)
	return value, err
}

func (c *Client) ListServices(ctx context.Context, id string, q ServiceQuery) (Page[Service], error) {
	ctx, state := c.operation(ctx, "ListServices")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Page[Service]{}, err
	}
	type qwireDTO query.ListProjectServiceStacks
	qwire, err := input[qwireDTO](q)
	if err != nil {
		return Page[Service]{}, err
	}
	r, outer := c.sdk.GetProjectServiceStack(ctx, path.ProjectId(p), query.ListProjectServiceStacks(qwire))
	value, err := finish[wirePage[Service]](state, r, outer)
	return value.page(q.Offset, q.Limit), err
}

func (c *Client) SetAutoscaling(ctx context.Context, id string, v AutoscalingWrite) (*Process, error) {
	ctx, state := c.operation(ctx, "SetAutoscaling")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return nil, err
	}
	type bDTO body.Autoscaling
	b, err := input[bDTO](v)
	if err != nil {
		return nil, err
	}
	r, outer := c.sdk.PutServiceStackAutoscaling(ctx, path.ServiceStackId(p), body.Autoscaling(b))
	value, err := finish[processEnvelope](state, r, outer)
	return normalizeProcess(value.Process), err
}

func (c *Client) StartService(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "StartService")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PutServiceStackStart(ctx, path.ServiceStackId(p))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) StopService(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "StopService")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PutServiceStackStop(ctx, path.ServiceStackId(p))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) DeleteService(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "DeleteService")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.DeleteServiceStack(ctx, path.ServiceStackId(p))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) ListServiceEnv(ctx context.Context, id string) ([]Env, error) {
	ctx, state := c.operation(ctx, "ListServiceEnv")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return nil, err
	}
	r, outer := c.sdk.GetServiceStackEnv(ctx, path.ServiceStackId(p))
	value, err := finish[itemsEnvelope[Env]](state, r, outer)
	return value.Items, err
}

func (c *Client) ListUserData(ctx context.Context, id string, q UserDataQuery) (Page[Env], error) {
	ctx, state := c.operation(ctx, "ListUserData")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Page[Env]{}, err
	}
	type qwireDTO query.ListServiceStackUserData
	qwire, err := input[qwireDTO](q)
	if err != nil {
		return Page[Env]{}, err
	}
	r, outer := c.sdk.GetServiceStackUserData(ctx, path.ServiceStackId(p), query.ListServiceStackUserData(qwire))
	value, err := finish[wirePage[Env]](state, r, outer)
	return value.page(q.Offset, q.Limit), err
}

func (c *Client) CreateObjectStorage(ctx context.Context, id string, v StorageCreate) (ServiceProcess, error) {
	ctx, state := c.operation(ctx, "CreateObjectStorage")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return ServiceProcess{}, err
	}
	type bDTO body.PostObjectStorageServiceStack
	b, err := input[bDTO](v)
	if err != nil {
		return ServiceProcess{}, err
	}
	r, outer := c.sdk.PostProjectServiceStackObjectStorageV1(ctx, path.ProjectId(p), body.PostObjectStorageServiceStack(b))
	value, err := finish[ServiceProcess](state, r, outer)
	normalizeService(&value.Service)
	value.Process = normalizeProcess(value.Process)
	return value, err
}

func (c *Client) GetObjectStorage(ctx context.Context, id string) (StorageInfo, error) {
	ctx, state := c.operation(ctx, "GetObjectStorage")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return StorageInfo{}, err
	}
	r, outer := c.sdk.GetServiceStackObjectStorageSize(ctx, path.ServiceStackId(p))
	value, err := finish[StorageInfo](state, r, outer)
	return value, err
}

func (c *Client) UpdateObjectStorage(ctx context.Context, id string, v StorageWrite) (Process, error) {
	ctx, state := c.operation(ctx, "UpdateObjectStorage")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	type bDTO body.PutServiceStackObjectStorageSize
	b, err := input[bDTO](v)
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PutServiceStackObjectStorageSize(ctx, path.ServiceStackId(p), body.PutServiceStackObjectStorageSize(b))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) ListHTTPRoutes(ctx context.Context, id string, q PageQuery) (Page[HTTPRoute], error) {
	ctx, state := c.operation(ctx, "ListHTTPRoutes")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Page[HTTPRoute]{}, err
	}
	type qwireDTO query.ListProjectPublicHttpRoutings
	qwire, err := input[qwireDTO](q)
	if err != nil {
		return Page[HTTPRoute]{}, err
	}
	r, outer := c.sdk.GetProjectPublicHttpRouting(ctx, path.ProjectId(p), query.ListProjectPublicHttpRoutings(qwire))
	value, err := finish[wirePage[HTTPRoute]](state, r, outer)
	return value.page(q.Offset, q.Limit), err
}

func (c *Client) GetHTTPRoute(ctx context.Context, id string) (HTTPRoute, error) {
	ctx, state := c.operation(ctx, "GetHTTPRoute")
	type pDTO path.PublicHttpRoutingId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return HTTPRoute{}, err
	}
	r, outer := c.sdk.GetPublicHttpRouting(ctx, path.PublicHttpRoutingId(p))
	value, err := finish[HTTPRoute](state, r, outer)
	return value, err
}

func (c *Client) CreateHTTPRoute(ctx context.Context, id string, v HTTPRouteWrite) (HTTPRoute, error) {
	ctx, state := c.operation(ctx, "CreateHTTPRoute")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return HTTPRoute{}, err
	}
	type bDTO body.PublicHttpRoutingPost
	b, err := input[bDTO](v)
	if err != nil {
		return HTTPRoute{}, err
	}
	r, outer := c.sdk.PostProjectPublicHttpRouting(ctx, path.ProjectId(p), body.PublicHttpRoutingPost(b))
	value, err := finish[HTTPRoute](state, r, outer)
	return value, err
}

func (c *Client) UpdateHTTPRoute(ctx context.Context, id string, v HTTPRouteWrite) (HTTPRoute, error) {
	ctx, state := c.operation(ctx, "UpdateHTTPRoute")
	type pDTO path.PublicHttpRoutingId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return HTTPRoute{}, err
	}
	type bDTO body.PublicHttpRoutingPut
	b, err := input[bDTO](v)
	if err != nil {
		return HTTPRoute{}, err
	}
	r, outer := c.sdk.PutPublicHttpRouting(ctx, path.PublicHttpRoutingId(p), body.PublicHttpRoutingPut(b))
	value, err := finish[HTTPRoute](state, r, outer)
	return value, err
}

func (c *Client) DeleteHTTPRoute(ctx context.Context, id string) error {
	ctx, state := c.operation(ctx, "DeleteHTTPRoute")
	type pDTO path.PublicHttpRoutingId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return err
	}
	r, outer := c.sdk.DeletePublicHttpRouting(ctx, path.PublicHttpRoutingId(p))
	_, err = finish[struct{}](state, r, outer)
	return err
}

func (c *Client) RestoreHTTPRoute(ctx context.Context, id string) (HTTPRoute, error) {
	ctx, state := c.operation(ctx, "RestoreHTTPRoute")
	type pDTO path.PublicHttpRoutingId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return HTTPRoute{}, err
	}
	r, outer := c.sdk.PutPublicHttpRoutingRestore(ctx, path.PublicHttpRoutingId(p))
	value, err := finish[HTTPRoute](state, r, outer)
	return value, err
}

func (c *Client) SyncHTTPRoutes(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "SyncHTTPRoutes")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PutProjectSyncPublicHttpRouting(ctx, path.ProjectId(p))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) ListPortRoutes(ctx context.Context, id string, q PageQuery) (Page[PortRoute], error) {
	ctx, state := c.operation(ctx, "ListPortRoutes")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Page[PortRoute]{}, err
	}
	type qwireDTO query.ListServiceStackPublicPortRoutings
	qwire, err := input[qwireDTO](q)
	if err != nil {
		return Page[PortRoute]{}, err
	}
	r, outer := c.sdk.GetServiceStackPublicPortRouting(ctx, path.ServiceStackId(p), query.ListServiceStackPublicPortRoutings(qwire))
	value, err := finish[wirePage[PortRoute]](state, r, outer)
	return value.page(q.Offset, q.Limit), err
}

func (c *Client) GetPortRoute(ctx context.Context, id string) (PortRoute, error) {
	ctx, state := c.operation(ctx, "GetPortRoute")
	type pDTO path.PublicPortRoutingId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return PortRoute{}, err
	}
	r, outer := c.sdk.GetPublicPortRouting(ctx, path.PublicPortRoutingId(p))
	value, err := finish[PortRoute](state, r, outer)
	return value, err
}

func (c *Client) CreatePortRoute(ctx context.Context, id string, v PortRouteWrite) (PortRoute, error) {
	ctx, state := c.operation(ctx, "CreatePortRoute")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return PortRoute{}, err
	}
	type bDTO body.PublicPortRouting
	b, err := input[bDTO](v)
	if err != nil {
		return PortRoute{}, err
	}
	r, outer := c.sdk.PostServiceStackPublicPortRouting(ctx, path.ServiceStackId(p), body.PublicPortRouting(b))
	value, err := finish[PortRoute](state, r, outer)
	return value, err
}

func (c *Client) UpdatePortRoute(ctx context.Context, id string, v PortRouteWrite) (PortRoute, error) {
	ctx, state := c.operation(ctx, "UpdatePortRoute")
	type pDTO path.PublicPortRoutingId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return PortRoute{}, err
	}
	type bDTO body.PublicPortRouting
	b, err := input[bDTO](v)
	if err != nil {
		return PortRoute{}, err
	}
	r, outer := c.sdk.PutPublicPortRouting(ctx, path.PublicPortRoutingId(p), body.PublicPortRouting(b))
	value, err := finish[PortRoute](state, r, outer)
	return value, err
}

func (c *Client) DeletePortRoute(ctx context.Context, id string) error {
	ctx, state := c.operation(ctx, "DeletePortRoute")
	type pDTO path.PublicPortRoutingId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return err
	}
	r, outer := c.sdk.DeletePublicPortRouting(ctx, path.PublicPortRoutingId(p))
	_, err = finish[struct{}](state, r, outer)
	return err
}

func (c *Client) RestorePortRoute(ctx context.Context, id string) (PortRoute, error) {
	ctx, state := c.operation(ctx, "RestorePortRoute")
	type pDTO path.PublicPortRoutingId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return PortRoute{}, err
	}
	r, outer := c.sdk.PutPublicPortRoutingRestore(ctx, path.PublicPortRoutingId(p))
	value, err := finish[PortRoute](state, r, outer)
	return value, err
}

func (c *Client) SyncPortRoutes(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "SyncPortRoutes")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PutProjectSyncPublicPortRouting(ctx, path.ProjectId(p))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) GetProcess(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "GetProcess")
	type pDTO path.ProcessId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.GetProcess(ctx, path.ProcessId(p))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}

func (c *Client) ListServiceProcesses(ctx context.Context, id string, q PageQuery) (Page[Process], error) {
	ctx, state := c.operation(ctx, "ListServiceProcesses")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Page[Process]{}, err
	}
	type qwireDTO query.ListServiceStackProcesses
	qwire, err := input[qwireDTO](q)
	if err != nil {
		return Page[Process]{}, err
	}
	r, outer := c.sdk.GetServiceStackProcess(ctx, path.ServiceStackId(p), query.ListServiceStackProcesses(qwire))
	value, err := finish[wirePage[Process]](state, r, outer)
	return value.page(q.Offset, q.Limit), err
}

func (c *Client) ListProjectProcesses(ctx context.Context, id string, q PageQuery) (Page[Process], error) {
	ctx, state := c.operation(ctx, "ListProjectProcesses")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Page[Process]{}, err
	}
	type qwireDTO query.ListProjectProcesses
	qwire, err := input[qwireDTO](q)
	if err != nil {
		return Page[Process]{}, err
	}
	r, outer := c.sdk.GetProjectProcess(ctx, path.ProjectId(p), query.ListProjectProcesses(qwire))
	value, err := finish[wirePage[Process]](state, r, outer)
	return value.page(q.Offset, q.Limit), err
}

func (c *Client) CreateAppVersion(ctx context.Context, id string, name string) (UploadVersion, error) {
	ctx, state := c.operation(ctx, "CreateAppVersion")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return UploadVersion{}, err
	}
	type bDTO body.PostAppVersion
	b, err := input[bDTO](map[string]any{"name": name})
	if err != nil {
		return UploadVersion{}, err
	}
	r, outer := c.sdk.PostServiceStackAppVersion(ctx, path.ServiceStackId(p), body.PostAppVersion(b))
	value, err := finish[UploadVersion](state, r, outer)
	return value, err
}

func (c *Client) GetAppVersion(ctx context.Context, id string) (AppVersion, error) {
	ctx, state := c.operation(ctx, "GetAppVersion")
	type pDTO path.AppVersionId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return AppVersion{}, err
	}
	r, outer := c.sdk.GetAppVersion(ctx, path.AppVersionId(p))
	value, err := finish[AppVersion](state, r, outer)
	normalizeAppVersion(&value)
	return value, err
}

func (c *Client) ListAppVersions(ctx context.Context, id string, q PageQuery) (Page[AppVersion], error) {
	ctx, state := c.operation(ctx, "ListAppVersions")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Page[AppVersion]{}, err
	}
	type qwireDTO query.ListServiceStackAppVersions
	qwire, err := input[qwireDTO](q)
	if err != nil {
		return Page[AppVersion]{}, err
	}
	r, outer := c.sdk.GetServiceStackAppVersion(ctx, path.ServiceStackId(p), query.ListServiceStackAppVersions(qwire))
	value, err := finish[wirePage[AppVersion]](state, r, outer)
	return value.page(q.Offset, q.Limit), err
}

func (c *Client) ExportProject(ctx context.Context, id string, reveal bool) (string, error) {
	ctx, state := c.operation(ctx, "ExportProject")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return "", err
	}
	type qwireDTO query.GetProjectExport
	qwire, err := input[qwireDTO](map[string]any{"reveal": reveal})
	if err != nil {
		return "", err
	}
	r, outer := c.sdk.GetProjectExport(ctx, path.ProjectId(p), query.GetProjectExport(qwire))
	value, err := finish[textEnvelope](state, r, outer)
	return value.Yaml, err
}

func (c *Client) Settings(ctx context.Context, id string) (Settings, error) {
	ctx, state := c.operation(ctx, "Settings")
	type pDTO path.ClientId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Settings{}, err
	}
	r, outer := c.sdk.GetClientSettings(ctx, path.ClientId(p))
	value, err := finish[Settings](state, r, outer)
	return value, err
}

func (c *Client) Regions(ctx context.Context) ([]Region, error) {
	ctx, state := c.operation(ctx, "Regions")
	r, outer := c.publicSDK.GetRegion(ctx)
	value, err := finish[itemsEnvelope[Region]](state, r, outer)
	return value.Items, err
}
