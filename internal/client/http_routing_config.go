package client

import (
	"context"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
)

// SetHTTPLocations replaces the complete ordered location list, including null config children.
func (c *Client) SetHTTPLocations(ctx context.Context, id string, locations []HTTPLocation) (HTTPRoute, error) {
	ctx, state := c.operation(ctx, "SetHTTPLocations")
	type pDTO path.PublicHttpRoutingId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return HTTPRoute{}, err
	}
	type bDTO body.PublicHttpRoutingLocationPut
	b, err := input[bDTO](map[string]any{"locations": locations})
	if err != nil {
		return HTTPRoute{}, err
	}
	r, outer := c.sdk.PutPublicHttpRoutingLocation(ctx, path.PublicHttpRoutingId(p), body.PublicHttpRoutingLocationPut(b))
	return finish[HTTPRoute](state, r, outer)
}

// TriggerHTTPDomainsDNSCheck requests one check; readiness remains a read-only observation.
func (c *Client) TriggerHTTPDomainsDNSCheck(ctx context.Context, id string) error {
	ctx, state := c.operation(ctx, "TriggerHTTPDomainsDNSCheck")
	type pDTO path.PublicHttpRoutingId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return err
	}
	r, outer := c.sdk.PutPublicHttpRoutingTriggerDomainsDnsCheck(ctx, path.PublicHttpRoutingId(p), body.PublicHttpRoutingPutTriggerDomainDnsCheck{})
	_, err = finish[struct {
		Success bool `json:"success"`
	}](state, r, outer)
	return err
}
