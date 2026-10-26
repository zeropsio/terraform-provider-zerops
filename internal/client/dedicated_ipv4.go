package client

import (
	"context"
	"github.com/zeropsio/zerops-go/dto/input/path"
)

// RequestDedicatedIPv4 requests a billed allocation; there is no release endpoint.
func (c *Client) RequestDedicatedIPv4(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "RequestDedicatedIPv4")
	type pDTO path.ProjectId
	p, err := input[pDTO](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PutProjectRequestIpv4(ctx, path.ProjectId(p))
	value, err := finish[Process](state, r, outer)
	normalizeProcess(&value)
	return value, err
}
