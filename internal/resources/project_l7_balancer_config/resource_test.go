package project_l7_balancer_config

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

type fakeAPI struct {
	client.API
	config  client.L7BalancerConfig
	written map[string]*string
	calls   int
}

func (f *fakeAPI) GetL7BalancerConfig(context.Context, string) (client.L7BalancerConfig, error) {
	return f.config, nil
}
func (f *fakeAPI) SetL7BalancerConfig(_ context.Context, _ string, v map[string]*string) (client.Process, error) {
	f.calls++
	f.written = v
	return client.Process{ID: "proc"}, nil
}
func TestL7AuthoritativeReplacementAndReset(t *testing.T) {
	a, b := "64m", "35"
	f := &fakeAPI{config: client.L7BalancerConfig{Values: []client.L7BalancerValue{{Name: "body", Current: &a, Default: "512m"}, {Name: "keepalive", Current: &b, Default: "30"}}}}
	r := &configResource{data: &providerdata.ProviderData{API: f}}
	ctx := context.Background()
	values, _ := types.MapValueFrom(ctx, types.StringType, map[string]string{"keepalive": "35"})
	m := model{ProjectID: types.StringValue("p"), Values: values}
	if _, e := r.write(ctx, &m, false); e != nil {
		t.Fatal(e)
	}
	if len(f.written) != 1 || f.written["keepalive"] == nil {
		t.Fatal("not a full desired replacement")
	}
	if matches, e := r.matches(ctx, &m); e != nil || matches {
		t.Fatal("unwanted override not drift")
	}
	if _, e := r.write(ctx, &m, true); e != nil {
		t.Fatal(e)
	}
	if len(f.written) != 2 || f.written["body"] != nil || f.written["keepalive"] != nil {
		t.Fatal("reset did not null every current key")
	}
	if _, e := r.load(ctx, &m); e != nil {
		t.Fatal(e)
	}
	if len(m.Values.Elements()) != 2 {
		t.Fatal("lost override")
	}
	f.config.Values[0].Default = "1g"
	if _, e := r.load(ctx, &m); e != nil {
		t.Fatal(e)
	}
	if m.Defaults.Elements()["body"].(types.String).ValueString() != "1g" {
		t.Fatal("cached defaults")
	}
	bad, _ := types.MapValueFrom(ctx, types.StringType, map[string]string{"unknown": "x"})
	m.Values = bad
	before := f.calls
	if _, e := r.write(ctx, &m, false); e == nil || f.calls != before {
		t.Fatal("unknown key written")
	}
}
func TestL7MalformedAndNullReadback(t *testing.T) {
	if _, _, e := l7Maps(client.L7BalancerConfig{}); e == nil {
		t.Fatal("missing values accepted")
	}
	empty := ""
	current, defaults, e := l7Maps(client.L7BalancerConfig{Values: []client.L7BalancerValue{{Name: "null", Default: "d"}, {Name: "empty", Current: &empty, Default: "e"}}})
	if e != nil || len(current) != 1 || current["empty"] != "" || len(defaults) != 2 {
		t.Fatal("null/empty conflated")
	}
}
