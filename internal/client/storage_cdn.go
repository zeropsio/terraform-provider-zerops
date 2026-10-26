package client

import (
	"context"
	"errors"
	"github.com/zeropsio/zerops-go/dto/input/path"
)

func (c *Client) SetStorageCDN(ctx context.Context, id string, enabled bool) (Process, error) {
	ctx, state := c.operation(ctx, "SetStorageCDN")
	p, err := input[path.ServiceStackId](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	if enabled {
		r, outer := c.sdk.PutServiceStackCdn(ctx, p)
		v, err := finish[Process](state, r, outer)
		normalizeProcess(&v)
		if err == nil && v.ID == "" {
			err = errors.New("CDN response missing process ID")
		}
		return v, err
	}
	r, outer := c.sdk.DeleteServiceStackCdn(ctx, p)
	v, err := finish[Process](state, r, outer)
	normalizeProcess(&v)
	if err == nil && v.ID == "" {
		err = errors.New("CDN response missing process ID")
	}
	return v, err
}
