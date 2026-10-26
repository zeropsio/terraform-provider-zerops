package client

import (
	"context"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
)

// RemoteLoggingWrite replaces both the unreadable configuration and forwarding metadata.
type RemoteLoggingWrite struct {
	SyslogConfig string  `json:"syslogConfig"`
	Certificate  string  `json:"certificate"`
	Type         *string `json:"type"`
}

func (c *Client) SetRemoteLogging(ctx context.Context, id string, v RemoteLoggingWrite) (Process, error) {
	ctx, s := c.operation(ctx, "SetRemoteLogging")
	p, e := input[path.ProjectId](map[string]any{"Id": id})
	if e != nil {
		return Process{}, e
	}
	b, e := input[body.PutProjectRemoteLogging](v)
	if e != nil {
		return Process{}, e
	}
	r, e := c.sdk.PutProjectRemoteLogging(ctx, p, b)
	return finish[Process](s, r, e)
}
