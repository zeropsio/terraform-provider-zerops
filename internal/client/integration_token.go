package client

import (
	"context"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
)

// IntegrationTokenProject scopes an organization's integration token.
type IntegrationTokenProject struct {
	ProjectID string `json:"projectId"`
	RoleCode  string `json:"roleCode"`
}

type IntegrationTokenWrite struct {
	Name              string                    `json:"name"`
	RoleCode          string                    `json:"roleCode"`
	Projects          []IntegrationTokenProject `json:"projects"`
	CanCreateProjects bool                      `json:"canCreateProjects"`
	CanViewFinances   bool                      `json:"canViewFinances"`
	CanEditFinances   bool                      `json:"canEditFinances"`
	Sudo              bool                      `json:"sudo"`
}

// IntegrationToken is readable metadata; the bearer is never returned by GET.
type IntegrationToken struct {
	ID string `json:"id"`
	IntegrationTokenWrite
}

// IntegrationTokenRaw is only returned by create and explicit regeneration.
type IntegrationTokenRaw struct {
	IntegrationToken
	Token string `json:"token"`
}

func (c *Client) CreateIntegrationToken(ctx context.Context, org string, v IntegrationTokenWrite) (IntegrationTokenRaw, error) {
	ctx, state := c.operation(ctx, "CreateIntegrationToken")
	p, err := input[path.ClientId](map[string]any{"Id": org})
	if err != nil {
		return IntegrationTokenRaw{}, err
	}
	b, err := input[body.ClientIntegrationToken](v)
	if err != nil {
		return IntegrationTokenRaw{}, err
	}
	r, outer := c.sdk.PostClientIntegrationToken(ctx, p, b)
	return finish[IntegrationTokenRaw](state, r, outer)
}
func (c *Client) GetIntegrationToken(ctx context.Context, org, id string) (IntegrationToken, error) {
	ctx, state := c.operation(ctx, "GetIntegrationToken")
	p, err := input[path.IntegrationTokenId](map[string]any{"Id": org, "TokenId": id})
	if err != nil {
		return IntegrationToken{}, err
	}
	r, outer := c.sdk.GetClientIntegrationToken(ctx, p)
	return finish[IntegrationToken](state, r, outer)
}
func (c *Client) UpdateIntegrationToken(ctx context.Context, org, id string, v IntegrationTokenWrite) (IntegrationToken, error) {
	ctx, state := c.operation(ctx, "UpdateIntegrationToken")
	p, err := input[path.IntegrationTokenId](map[string]any{"Id": org, "TokenId": id})
	if err != nil {
		return IntegrationToken{}, err
	}
	b, err := input[body.ClientIntegrationToken](v)
	if err != nil {
		return IntegrationToken{}, err
	}
	r, outer := c.sdk.PutClientIntegrationToken(ctx, p, b)
	return finish[IntegrationToken](state, r, outer)
}
func (c *Client) DeleteIntegrationToken(ctx context.Context, org, id string) error {
	ctx, state := c.operation(ctx, "DeleteIntegrationToken")
	p, err := input[path.IntegrationTokenId](map[string]any{"Id": org, "TokenId": id})
	if err != nil {
		return err
	}
	r, outer := c.sdk.DeleteClientIntegrationToken(ctx, p)
	_, err = finish[struct{}](state, r, outer)
	return err
}
func (c *Client) RegenerateIntegrationToken(ctx context.Context, org, id string) (IntegrationTokenRaw, error) {
	ctx, state := c.operation(ctx, "RegenerateIntegrationToken")
	p, err := input[path.IntegrationTokenId](map[string]any{"Id": org, "TokenId": id})
	if err != nil {
		return IntegrationTokenRaw{}, err
	}
	r, outer := c.sdk.PutClientIntegrationTokenRegenerate(ctx, p)
	return finish[IntegrationTokenRaw](state, r, outer)
}
