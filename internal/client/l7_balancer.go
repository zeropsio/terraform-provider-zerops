package client

import (
	"context"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"sort"
)

type L7BalancerValue struct {
	Name    string  `json:"name"`
	Current *string `json:"current"`
	Default string  `json:"default"`
}
type L7BalancerConfig struct {
	Values []L7BalancerValue `json:"values"`
}
type L7BalancerWrite struct {
	Name  string  `json:"name"`
	Value *string `json:"value"`
}

// SortedL7Values makes full replacement requests deterministic, including null resets.
func SortedL7Values(values map[string]*string) []L7BalancerWrite {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]L7BalancerWrite, 0, len(keys))
	for _, k := range keys {
		out = append(out, L7BalancerWrite{Name: k, Value: values[k]})
	}
	return out
}
func (c *Client) GetL7BalancerConfig(ctx context.Context, id string) (L7BalancerConfig, error) {
	ctx, s := c.operation(ctx, "GetL7BalancerConfig")
	p, e := input[path.ProjectId](map[string]any{"Id": id})
	if e != nil {
		return L7BalancerConfig{}, e
	}
	r, e := c.sdk.GetProjectL7httpbalancerConfig(ctx, p)
	return finish[L7BalancerConfig](s, r, e)
}
func (c *Client) SetL7BalancerConfig(ctx context.Context, id string, values map[string]*string) (Process, error) {
	ctx, s := c.operation(ctx, "SetL7BalancerConfig")
	p, e := input[path.ProjectId](map[string]any{"Id": id})
	if e != nil {
		return Process{}, e
	}
	b, e := input[body.PutProjectL7httpbalancerConfig](map[string]any{"values": SortedL7Values(values)})
	if e != nil {
		return Process{}, e
	}
	r, e := c.sdk.PutProjectL7httpbalancerConfig(ctx, p, b)
	return finish[Process](s, r, e)
}
