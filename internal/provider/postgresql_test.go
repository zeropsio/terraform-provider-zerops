package provider

import (
	"context"
	"math/big"
	"strings"
	"testing"

	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

func TestConfigurePostgresqlDiskFloor(t *testing.T) {
	for _, tc := range []struct {
		name string
		attr any
		env  string
		want float64
		bad  bool
	}{
		{"default", nil, "", 2, false},
		{"attribute", 4, "invalid", 4, false},
		{"environment", nil, "3.5", 3.5, false},
		{"attribute-precision", 2.0001, "", 0, true},
		{"environment-precision", nil, "2.0001", 0, true},
		{"attribute-fractional", 2.125, "", 2.125, false},
		{"environment-fractional", nil, "2.125", 2.125, false},
		{"below-one", 0.5, "", 0, true},
		{"invalid-environment", nil, "invalid", 0, true},
		{"environment-below-one", nil, "0", 0, true},
		{"infinite", nil, "+Inf", 0, true},
		{"nan", nil, "NaN", 0, true},
		{"unknown", tftypes.UnknownValue, "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			if tc.env != "" {
				t.Setenv("ZEROPS_POSTGRESQL_MIN_DISK_GB", tc.env)
			}
			p := New("test", Registry{})()
			var resp framework.ConfigureResponse
			p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, map[string]any{"endpoint": "https://api.example.com", "token": "test", "organization_id": "org", "routing_force_sync": false, "postgresql_min_disk_gb": tc.attr})}, &resp)
			if resp.Diagnostics.HasError() != tc.bad {
				t.Fatal(resp.Diagnostics)
			}
			if !tc.bad && resp.ResourceData.(*providerdata.ProviderData).PostgresqlMinDiskGB != tc.want {
				t.Fatal("wrong resolved disk floor")
			}
			if tc.name == "invalid-environment" && !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "ZEROPS_POSTGRESQL_MIN_DISK_GB") {
				t.Fatal(resp.Diagnostics)
			}
		})
	}
}

func TestPostgresqlDiskFloorValidation(t *testing.T) {
	for _, value := range []float64{0, 0.5, 1, 2.125, 2.0001} {
		var resp validator.NumberResponse
		postgresqlDiskFloorValidator{}.ValidateNumber(context.Background(), validator.NumberRequest{ConfigValue: types.NumberValue(big.NewFloat(value))}, &resp)
		if resp.Diagnostics.HasError() != (value < 1 || value == 2.0001) {
			t.Fatal(resp.Diagnostics)
		}
	}
}

func TestPostgresqlDiskFloorUnknownDefers(t *testing.T) {
	p := New("test", Registry{})()
	var resp framework.ConfigureResponse
	p.Configure(context.Background(), framework.ConfigureRequest{
		Config:             providerConfig(t, p, map[string]any{"postgresql_min_disk_gb": tftypes.UnknownValue}),
		ClientCapabilities: framework.ConfigureProviderClientCapabilities{DeferralAllowed: true},
	}, &resp)
	if resp.Diagnostics.HasError() || resp.Deferred == nil || resp.ResourceData != nil {
		t.Fatalf("unknown floor did not defer: %v", resp.Diagnostics)
	}
}
