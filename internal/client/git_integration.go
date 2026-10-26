package client

import (
	"context"
	"fmt"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
)

// GitIntegration contains repository metadata only; OAuth credentials are server-owned.
type GitIntegration struct {
	RepositoryFullName    string  `json:"repositoryFullName"`
	EventType             string  `json:"eventType"`
	BranchName            *string `json:"branchName"`
	TagRegex              *string `json:"tagRegex"`
	IsActive              bool    `json:"isActive"`
	ZeropsYamlSetup       *string `json:"zeropsYamlSetup"`
	UserID                string  `json:"userId"`
	AuthorizationRequired bool    `json:"authorizationRequired"`
	WebhookID             *int64  `json:"webhookId"`
}
type GitIntegrationStatus struct {
	GithubIntegration *GitIntegration `json:"githubIntegration,omitempty"`
	GitlabIntegration *GitIntegration `json:"gitlabIntegration,omitempty"`
}

// SetGitIntegration replaces the connector, or resets it when both blocks are nil.
// Builds are deliberately never triggered by configuration reconciliation.
func (c *Client) SetGitIntegration(ctx context.Context, id string, v GitIntegrationStatus) (*Process, error) {
	if v.GithubIntegration != nil && v.GitlabIntegration != nil {
		return nil, fmt.Errorf("exactly one git provider must be selected")
	}
	ctx, state := c.operation(ctx, "SetGitIntegration")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return nil, err
	}
	// Project only writable metadata; computed fields and credentials never enter the request.
	payload := map[string]any{}
	for name, g := range map[string]*GitIntegration{"githubIntegration": v.GithubIntegration, "gitlabIntegration": v.GitlabIntegration} {
		if g != nil {
			payload[name] = map[string]any{"repositoryFullName": g.RepositoryFullName, "eventType": g.EventType, "branchName": g.BranchName, "tagRegex": g.TagRegex, "isActive": g.IsActive, "zeropsYamlSetup": g.ZeropsYamlSetup, "triggerBuild": false}
		}
	}
	type bDTO body.ExternalRepositoryIntegration
	b, err := input[bDTO](payload)
	if err != nil {
		return nil, err
	}
	r, outer := c.sdk.PutServiceStackExternalRepositoryIntegration(ctx, path.ServiceStackId(p), body.ExternalRepositoryIntegration(b))
	value, err := finish[processEnvelope](state, r, outer)
	return normalizeProcess(value.Process), err
}
func (c *Client) GetGitIntegration(ctx context.Context, id string) (GitIntegrationStatus, error) {
	ctx, state := c.operation(ctx, "GetGitIntegration")
	type pDTO path.ServiceStackId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return GitIntegrationStatus{}, err
	}
	r, outer := c.sdk.GetServiceStackExternalRepositoryIntegrationStatus(ctx, path.ServiceStackId(p))
	return finish[GitIntegrationStatus](state, r, outer)
}
