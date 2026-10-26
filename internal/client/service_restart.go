package client

import (
	"context"
	"errors"
	"github.com/zeropsio/zerops-go/dto/input/path"
)

func (c *Client) RestartService(ctx context.Context, id string) (Process, error) {
	ctx, state := c.operation(ctx, "RestartService")
	p, err := input[path.ServiceStackId](map[string]any{"Id": id})
	if err != nil {
		return Process{}, err
	}
	r, outer := c.sdk.PutServiceStackRestart(ctx, p)
	v, err := finish[Process](state, r, outer)
	normalizeProcess(&v)
	if err == nil && v.ID == "" {
		err = errors.New("restart response missing process ID")
	}
	return v, err
}
