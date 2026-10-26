package client

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
)

// PrometheusConfig is one ordered custom service-discovery group.
type PrometheusConfig struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}
type PrometheusState struct {
	CoreServiceID string
	Configs       []PrometheusConfig
}

func (c *Client) SetPrometheusConfig(ctx context.Context, id string, configs []PrometheusConfig) (*Process, error) {
	ctx, s := c.operation(ctx, "SetPrometheusConfig")
	p, e := input[path.ProjectId](map[string]any{"Id": id})
	if e != nil {
		return nil, e
	}
	if configs == nil {
		configs = []PrometheusConfig{}
	}
	b, e := input[body.CoreStackPrometheusConfig](map[string]any{"configs": configs})
	if e != nil {
		return nil, e
	}
	r, e := c.sdk.PutPrometheusConfig(ctx, p, b)
	v, e := finish[struct {
		Process *Process `json:"process"`
	}](s, r, e)
	return normalizeProcess(v.Process), e
}

// PrometheusReader uses public service and user-data collections, including SYSTEM rows.
type PrometheusReader interface {
	GetProject(context.Context, string) (Project, error)
	ListServices(context.Context, string, ServiceQuery) (Page[Service], error)
	ListUserData(context.Context, string, UserDataQuery) (Page[Env], error)
}

// ReadPrometheusConfig refuses ambiguous multi-instance projects: the public project
// response does not identify the primary instance, so choosing one would be unsafe.
func ReadPrometheusConfig(ctx context.Context, api PrometheusReader, id string) (PrometheusState, error) {
	if _, e := api.GetProject(ctx, id); e != nil {
		return PrometheusState{}, e
	}
	var cores []Service
	for offset := 0; ; {
		page, e := api.ListServices(ctx, id, ServiceQuery{PageQuery: PageQuery{Offset: offset, Limit: 100}})
		if e != nil {
			return PrometheusState{}, e
		}
		for _, s := range page.Items {
			if s.ServiceStackTypeInfo.Category == "CORE" {
				cores = append(cores, s)
			}
		}
		next, done, e := prometheusPage(offset, len(page.Items), page.Total)
		if e != nil {
			return PrometheusState{}, e
		}
		if done {
			break
		}
		offset = next
	}
	if len(cores) != 1 || cores[0].ID == "" {
		return PrometheusState{}, fmt.Errorf("expected one unambiguous primary CORE service across project instances; found %d", len(cores))
	}
	var rows []Env
	for offset := 0; ; {
		page, e := api.ListUserData(ctx, cores[0].ID, UserDataQuery{PageQuery: PageQuery{Offset: offset, Limit: 100}})
		if e != nil {
			return PrometheusState{}, e
		}
		for _, row := range page.Items {
			if row.Type == "SYSTEM" && row.Key == "ZEROPS_PROMETHEUS_CUSTOM_SD" {
				rows = append(rows, row)
			}
		}
		next, done, e := prometheusPage(offset, len(page.Items), page.Total)
		if e != nil {
			return PrometheusState{}, e
		}
		if done {
			break
		}
		offset = next
	}
	if len(rows) != 1 {
		return PrometheusState{}, fmt.Errorf("expected exactly one SYSTEM ZEROPS_PROMETHEUS_CUSTOM_SD key; found %d", len(rows))
	}
	var configs []PrometheusConfig
	if json.Unmarshal([]byte(rows[0].Content), &configs) != nil || configs == nil {
		return PrometheusState{}, fmt.Errorf("SYSTEM ZEROPS_PROMETHEUS_CUSTOM_SD content is not an available JSON configuration array")
	}
	for _, c := range configs {
		if c.Targets == nil || c.Labels == nil {
			return PrometheusState{}, fmt.Errorf("SYSTEM ZEROPS_PROMETHEUS_CUSTOM_SD configuration is missing targets or labels")
		}
	}
	return PrometheusState{CoreServiceID: cores[0].ID, Configs: configs}, nil
}
func prometheusPage(offset, count, total int) (int, bool, error) {
	if count == 0 && offset < total {
		return 0, false, fmt.Errorf("Prometheus discovery pagination ended before total")
	}
	next := offset + count
	return next, count == 0 || next >= total, nil
}
func EqualPrometheusConfigs(a, b []PrometheusConfig) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !slices.Equal(a[i].Targets, b[i].Targets) || !maps.Equal(a[i].Labels, b[i].Labels) {
			return false
		}
	}
	return true
}
