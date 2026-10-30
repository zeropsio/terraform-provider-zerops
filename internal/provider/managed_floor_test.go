package provider

import (
	"context"
	"strings"
	"testing"

	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

func TestConfigureManagedRAMFloors(t *testing.T) {
	for _, family := range []struct {
		env      string
		fallback float64
		value    func(*providerdata.ProviderData) float64
	}{
		{"ZEROPS_CLICKHOUSE_MIN_RAM_GB", 0.375, func(d *providerdata.ProviderData) float64 { return d.ClickhouseMinRAMGB }},
		{"ZEROPS_ELASTICSEARCH_MIN_RAM_GB", 2, func(d *providerdata.ProviderData) float64 { return d.ElasticsearchMinRAMGB }},
	} {
		for _, tc := range []struct {
			name, env string
			want      float64
			bad       bool
		}{
			{name: "default", want: family.fallback},
			{name: "override", env: "3.5", want: 3.5},
			{name: "small-positive", env: "0.125", want: 0.125},
			{name: "three-decimals", env: "0.375", want: 0.375},
			{name: "clickhouse-excess-precision", env: "0.3755", bad: true},
			{name: "elasticsearch-excess-precision", env: "2.0001", bad: true},
			{name: "zero", env: "0", bad: true},
			{name: "negative", env: "-1", bad: true},
			{name: "infinite", env: "+Inf", bad: true},
			{name: "nan", env: "NaN", bad: true},
			{name: "invalid", env: "invalid", bad: true},
			{name: "empty", bad: true},
		} {
			t.Run(family.env+"/"+tc.name, func(t *testing.T) {
				cleanEnv(t)
				for _, env := range []string{"ZEROPS_CLICKHOUSE_MIN_RAM_GB", "ZEROPS_ELASTICSEARCH_MIN_RAM_GB"} {
					t.Setenv(env, "")
					unsetEnvironment(t, env)
				}
				if tc.name != "default" {
					t.Setenv(family.env, tc.env)
				}
				p := New("test", Registry{})()
				var resp framework.ConfigureResponse
				p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, map[string]any{"endpoint": "https://api.example.com", "token": "test", "organization_id": "org", "routing_force_sync": false})}, &resp)
				if resp.Diagnostics.HasError() != tc.bad {
					t.Fatal(resp.Diagnostics)
				}
				if tc.bad {
					if resp.ResourceData != nil || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), family.env+" must be a finite positive number with at most three decimal places.") {
						t.Fatal(resp.Diagnostics)
					}
				} else if got := family.value(resp.ResourceData.(*providerdata.ProviderData)); got != tc.want {
					t.Fatalf("floor=%g want=%g", got, tc.want)
				}
			})
		}
	}
}
