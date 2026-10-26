package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/dto/input/query"
)

type wirePage[T any] struct {
	List  []T `json:"list"`
	Total int `json:"total"`
}

func (w wirePage[T]) page(offset, limit int) Page[T] {
	if limit == 0 {
		limit = 100
	}
	for i := range w.List {
		switch v := any(&w.List[i]).(type) {
		case *Service:
			normalizeService(v)
		case *Process:
			normalizeProcess(v)
		case *AppVersion:
			normalizeAppVersion(v)
		}
	}
	return Page[T]{Items: w.List, Total: w.Total, Offset: offset, Limit: limit}
}

type itemsEnvelope[T any] struct {
	Items []T `json:"items"`
}
type processEnvelope struct {
	Process *Process `json:"process"`
}
type textEnvelope struct {
	Content string `json:"content"`
	Yaml    string `json:"yaml"`
}

func normalizeProcess(p *Process) *Process {
	if p != nil && p.AppVersion != nil {
		p.AppVersionID = p.AppVersion.ID
		normalizeAppVersion(p.AppVersion)
	}
	return p
}
func normalizeService(s *Service) {
	if s.ActiveAppVersion != nil {
		s.ActiveAppVersionID = s.ActiveAppVersion.ID
	}
	s.ServiceTypeCategory = s.ServiceStackTypeInfo.Category
}

func normalizeAppVersion(v *AppVersion) {
	for _, source := range []map[string]any{v.PublicGitSource, v.GithubIntegration, v.GitlabIntegration} {
		if branch, ok := source["branchName"].(string); ok && branch != "" {
			v.GitBranch = branch
			return
		}
	}
}

// CollectPages advances by actual item count, not the requested page size.
func CollectPages[T any](ctx context.Context, fetch func(context.Context, PageQuery) (Page[T], error)) ([]T, error) {
	var items []T
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, err := fetch(ctx, PageQuery{Offset: offset, Limit: 100})
		if err != nil {
			return nil, err
		}
		if p.Offset != offset {
			return nil, errors.New("inconsistent pagination offset")
		}
		n := len(p.Items)
		if n == 0 {
			if p.Total > offset {
				return nil, errors.New("pagination stopped before reported total")
			}
			return items, nil
		}
		items = append(items, p.Items...)
		next := offset + n
		if next <= offset {
			return nil, errors.New("pagination did not advance")
		}
		offset = next
		if p.Total > 0 && offset >= p.Total {
			return items, nil
		}
	}
}
func (c *Client) AllServiceProcesses(ctx context.Context, id string) ([]Process, error) {
	return CollectPages(ctx, func(ctx context.Context, q PageQuery) (Page[Process], error) {
		return c.ListServiceProcesses(ctx, id, q)
	})
}
func (c *Client) AllProjectProcesses(ctx context.Context, id string) ([]Process, error) {
	return CollectPages(ctx, func(ctx context.Context, q PageQuery) (Page[Process], error) {
		return c.ListProjectProcesses(ctx, id, q)
	})
}
func (c *Client) GetProjectByName(ctx context.Context, org, name string, statuses []string) (Project, error) {
	projects, err := CollectPages(ctx, func(ctx context.Context, q PageQuery) (Page[Project], error) {
		return c.ListProjects(ctx, org, ProjectQuery{PageQuery: q, NameContains: name})
	})
	if err != nil {
		return Project{}, err
	}
	var matches []Project
	for _, p := range projects {
		if p.Name != name {
			continue
		}
		allowed := len(statuses) == 0
		for _, s := range statuses {
			if p.Status == s {
				allowed = true
			}
		}
		if allowed {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return Project{}, &APIError{HTTPStatus: 400, Code: "projectNotFound"}
	}
	if len(matches) != 1 {
		return Project{}, errors.New("multiple projects have the exact requested name")
	}
	return matches[0], nil
}
func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	return CollectPages(ctx, func(ctx context.Context, q PageQuery) (Page[Organization], error) {
		ctx, state := c.operation(ctx, "ListOrganizations")
		qw, err := input[query.ListUserClients](q)
		if err != nil {
			return Page[Organization]{}, err
		}
		r, outer := c.sdk.GetUserClientList(ctx, qw)
		type membership struct {
			ID       string `json:"id"`
			ClientID string `json:"clientId"`
			RoleCode string `json:"roleCode"`
			Client   struct {
				ID          string `json:"id"`
				AccountName string `json:"accountName"`
			} `json:"client"`
		}
		wire, err := finish[wirePage[membership]](state, r, outer)
		if err != nil {
			return Page[Organization]{}, err
		}
		p := Page[Organization]{Total: wire.Total, Offset: q.Offset, Limit: q.Limit}
		for _, m := range wire.List {
			if m.ClientID == "" || m.Client.ID != m.ClientID {
				return p, errors.New("organization membership clientId does not match client.id")
			}
			p.Items = append(p.Items, Organization{ID: m.ClientID, Name: m.Client.AccountName, MembershipID: m.ID, RoleCode: m.RoleCode})
		}
		return p, nil
	})
}
func (c *Client) SetSubdomainAccess(ctx context.Context, id string, enabled bool) (Process, error) {
	ctx, state := c.operation(ctx, "SetSubdomainAccess")
	p, err := input[path.ServiceStackId](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	if enabled {
		r, e := c.sdk.PutServiceStackEnableSubdomainAccess(ctx, p)
		return finish[Process](state, r, e)
	}
	r, e := c.sdk.PutServiceStackDisableSubdomainAccess(ctx, p)
	return finish[Process](state, r, e)
}
func (c *Client) DeployAppVersion(ctx context.Context, id string, v DeployRequest, build bool) (Process, error) {
	ctx, state := c.operation(ctx, "DeployAppVersion")
	p, err := input[path.AppVersionId](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	if build {
		b, err := input[body.PutAppVersionBuildAndDeploy](v)
		if err != nil {
			return Process{}, err
		}
		r, e := c.sdk.PutAppVersionBuildAndDeploy(ctx, p, b)
		return finish[Process](state, r, e)
	}
	b, err := input[body.PutAppVersionDeploy](v)
	if err != nil {
		return Process{}, err
	}
	r, e := c.sdk.PutAppVersionDeploy(ctx, p, b)
	return finish[Process](state, r, e)
}

// TriggerGitPipeline unwraps process.appVersion rather than treating the
// nullable process wrapper as a direct process response.
func (c *Client) TriggerGitPipeline(ctx context.Context, id string, v GitPipelineRequest) (PipelineProcess, error) {
	type dto body.PutStandardServiceStackTriggerPipeline
	b, err := input[dto](v)
	if err != nil {
		return PipelineProcess{}, err
	}
	p, err := input[path.ServiceStackId](map[string]any{"Id": id})
	if err != nil {
		return PipelineProcess{}, err
	}
	ctx, state := c.operation(ctx, "TriggerGitPipeline")
	r, outer := c.sdk.PutServiceStackTriggerPipeline(ctx, p, body.PutStandardServiceStackTriggerPipeline(b))
	out, err := finish[PipelineProcess](state, r, outer)
	if out.Process != nil {
		normalizeProcess(out.Process)
		out.AppVersion = out.Process.AppVersion
		out.AppVersionID = out.Process.AppVersionID
	}
	return out, err
}

// UploadArchive is raw HTTP: SDK JSON methods buffer bodies and cannot provide
// the streaming signed-URL PUT contract. Never add the API bearer token here.
func (c *Client) UploadArchive(ctx context.Context, signedURL string, archive io.Reader, size int64) error {
	u, err := url.Parse(signedURL)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(c.testHTTP && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return errors.New("archive upload requires a valid HTTPS URL")
	}
	ctx, state := c.operation(ctx, "UploadArchive")
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, signedURL, archive)
	if err != nil {
		return errors.New("invalid archive upload request")
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	// Redirects are deliberately not followed: archive streams cannot safely be
	// replayed and redirect targets must never inherit authentication.
	resp, err := c.http.Do(req)
	if err != nil {
		return &TransportError{cause: err}
	}
	defer resp.Body.Close()
	if state.transport != nil {
		return &TransportError{cause: state.transport}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{HTTPStatus: resp.StatusCode, Code: "archiveUploadFailed"}
	}
	return nil
}
