package client

import (
	"context"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
)

// ProjectImport is a newly created project and the work returned for its stacks.
type ProjectImport struct {
	ProjectID     string                 `json:"projectId"`
	ProjectName   string                 `json:"projectName"`
	ServiceStacks []ProjectImportService `json:"serviceStacks"`
}
type ProjectImportService struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Processes []Process `json:"processes"`
}

func (c *Client) ImportProject(ctx context.Context, organizationID, yaml string) (ProjectImport, error) {
	ctx, state := c.operation(ctx, "ImportProject")
	type pDTO path.ClientId
	p, err := input[pDTO](map[string]any{"Id": organizationID})
	if err != nil {
		return ProjectImport{}, err
	}
	type bDTO body.ProjectImport
	b, err := input[bDTO](map[string]any{"yaml": yaml})
	if err != nil {
		return ProjectImport{}, err
	}
	response, outer := c.sdk.PostClientProjectImport(ctx, path.ClientId(p), body.ProjectImport(b))
	value, err := finish[ProjectImport](state, response, outer)
	for i := range value.ServiceStacks {
		for j := range value.ServiceStacks[i].Processes {
			normalizeProcess(&value.ServiceStacks[i].Processes[j])
		}
	}
	return value, err
}
