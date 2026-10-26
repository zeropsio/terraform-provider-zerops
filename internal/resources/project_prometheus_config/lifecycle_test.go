package project_prometheus_config

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type fakeAPI struct {
	client.API
	configs []client.PrometheusConfig
	writes  int
	missing bool
	cores   int
}

func (f *fakeAPI) GetProject(context.Context, string) (client.Project, error) {
	if f.missing {
		return client.Project{}, &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}
	}
	return client.Project{ID: "p"}, nil
}
func (f *fakeAPI) ListServices(context.Context, string, client.ServiceQuery) (client.Page[client.Service], error) {
	s := []client.Service{{ID: "core", ServiceStackTypeInfo: client.ServiceTypeInfo{Category: "CORE"}}}
	if f.cores == 2 {
		s = append(s, s[0])
	}
	return client.Page[client.Service]{Items: s, Total: len(s)}, nil
}
func (f *fakeAPI) ListUserData(context.Context, string, client.UserDataQuery) (client.Page[client.Env], error) {
	b, e := json.Marshal(f.configs)
	if e != nil {
		return client.Page[client.Env]{}, e
	}
	return client.Page[client.Env]{Items: []client.Env{{Type: "SYSTEM", Key: "ZEROPS_PROMETHEUS_CUSTOM_SD", Content: string(b)}}, Total: 1}, nil
}
func (f *fakeAPI) SetPrometheusConfig(_ context.Context, _ string, c []client.PrometheusConfig) (*client.Process, error) {
	if f.missing {
		return nil, &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}
	}
	f.configs = c
	f.writes++
	return nil, nil
}
func TestMutationAndClear(t *testing.T) {
	f := &fakeAPI{}
	r := &prometheusResource{data: &providerdata.ProviderData{API: f, Locks: &locks.Keyed{}}}
	m := model{ProjectID: types.StringValue("p"), Configs: []configModel{{Targets: []types.String{types.StringValue("localhost:9100")}, Labels: map[string]types.String{}}}}
	var ds diag.Diagnostics
	accepted := false
	r.mutate(context.Background(), &m, false, func() { accepted = true }, &ds)
	if ds.HasError() || !accepted || m.CoreServiceID.ValueString() != "core" || f.writes != 1 {
		t.Fatalf("mutation: %v %+v", ds, m)
	}
	r.mutate(context.Background(), &m, true, func() {}, &ds)
	if ds.HasError() || len(f.configs) != 0 || f.configs == nil {
		t.Fatalf("clear: %v %+v", ds, f.configs)
	}
	f.missing = true
	r.mutate(context.Background(), &m, true, func() {}, &ds)
	if ds.HasError() {
		t.Fatal(ds)
	}
}
func TestAmbiguousCoreAndSemanticOrdering(t *testing.T) {
	f := &fakeAPI{cores: 2}
	if _, e := client.ReadPrometheusConfig(context.Background(), f, "p"); e == nil {
		t.Fatal("ambiguous core accepted")
	}
	a := []client.PrometheusConfig{{Targets: []string{"a", "b"}, Labels: map[string]string{}}}
	b := []client.PrometheusConfig{{Targets: []string{"b", "a"}, Labels: map[string]string{}}}
	if client.EqualPrometheusConfigs(a, b) {
		t.Fatal("target order ignored")
	}
}
func TestSchema(t *testing.T) {
	var resp resource.SchemaResponse
	NewResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if ds := resp.Schema.ValidateImplementation(context.Background()); ds.HasError() {
		t.Fatal(ds)
	}
}
