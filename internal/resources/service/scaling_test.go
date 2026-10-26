package service

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"math/big"
	"testing"
)

func TestScalingRoundTrip(t *testing.T) {
	ram := 0.10000000149
	cpu := 2
	swap := false
	max := 3
	a := &client.Autoscaling{Vertical: &client.VerticalAutoscaling{MaxResource: &client.ScalingResource{CPUCoreCount: &cpu, MemoryGBytes: &ram}, SwapEnabled: &swap}, Horizontal: &client.HorizontalAutoscaling{MaxContainerCount: &max}}
	v, h := ScalingValues(a, true)
	decimal, _, _ := big.ParseFloat("0.1", 10, 512, big.ToNearestEven)
	if !v.Attributes()["max_ram_gb"].Equal(types.NumberValue(decimal)) {
		t.Fatal("float32 artifact not rounded")
	}
	out, e := scalingInput(v, h)
	if e != nil {
		t.Fatal(e)
	}
	if out.Vertical.SwapEnabled == nil || *out.Vertical.SwapEnabled || *out.Horizontal.MaxContainerCount != 3 || *out.Vertical.MaxResource.CPUCoreCount != 2 {
		t.Fatalf("lost nullable input: %#v", out)
	}
	current := CurrentValue(a)
	if current.Attributes()["vertical_autoscaling"].(types.Object).Attributes()["max_ram_gb"].Equal(v.Attributes()["max_ram_gb"]) {
		t.Fatal("effective default incorrectly rounded")
	}
}
func TestNullScaling(t *testing.T) {
	v, h := ScalingValues(&client.Autoscaling{Vertical: &client.VerticalAutoscaling{}, Horizontal: &client.HorizontalAutoscaling{}}, true)
	if !v.IsNull() || !h.IsNull() {
		t.Fatal("all-null backend objects not normalized")
	}
	out, e := scalingInput(v, h)
	if e != nil || out.Vertical != nil || out.Horizontal != nil {
		t.Fatal("full replacement must send null blocks")
	}
}
func TestManagedLifecycleRefresh(t *testing.T) {
	for _, tt := range []struct {
		desired      types.String
		status, want string
	}{{types.StringNull(), "ACTIVE", ""}, {types.StringValue("RUNNING"), "STOPPED", "STOPPED"}, {types.StringValue("STOPPED"), "UPGRADING", "STOPPED"}, {types.StringValue("STOPPED"), "ACTIVE", "RUNNING"}} {
		m := Model{Base: types.StringNull(), Desired: tt.desired}
		var d diag.Diagnostics
		m.refresh(client.Service{Status: tt.status}, &d)
		if m.Desired.ValueString() != tt.want || m.Desired.IsNull() != tt.desired.IsNull() {
			t.Fatalf("refresh lifecycle %s => %s", tt.status, m.Desired)
		}
	}
}

// Deployments change the run base (zerops.yml run.base); refresh must follow it without error.
func TestDeployChangesBase(t *testing.T) {
	m := Model{Base: types.StringValue("ubuntu/nodejs@22"), Type: types.StringValue("nodejs@22")}
	var d diag.Diagnostics
	m.refresh(client.Service{Base: "alpine/nodejs@22"}, &d)
	if d.HasError() || m.Base.ValueString() != "alpine/nodejs@22" || m.Type.ValueString() != "nodejs@22" {
		t.Fatalf("base refresh: diags=%v base=%s type=%s", d, m.Base, m.Type)
	}
}
func TestNumberBounds(t *testing.T) {
	for _, tt := range []struct {
		n   float64
		bad bool
	}{{-1, true}, {0, true}, {0.001, false}, {100, false}, {100.001, true}} {
		var p validator.NumberResponse
		numberBounds{min: 0, max: 100, strict: true}.ValidateNumber(context.Background(), validator.NumberRequest{ConfigValue: types.NumberValue(big.NewFloat(tt.n))}, &p)
		if p.Diagnostics.HasError() != tt.bad {
			t.Fatalf("%v: %v", tt.n, p.Diagnostics)
		}
	}
}
func TestScalingUsesCustomNotCurrent(t *testing.T) {
	v := types.ObjectValueMust(ScalingTypes(true), func() map[string]attr.Value {
		m := map[string]attr.Value{}
		for k, t := range ScalingTypes(true) {
			switch t {
			case types.NumberType:
				m[k] = types.NumberNull()
			case types.Int64Type:
				m[k] = types.Int64Null()
			case types.StringType:
				m[k] = types.StringNull()
			case types.BoolType:
				m[k] = types.BoolNull()
			}
		}
		m["swap_enabled"] = types.BoolValue(false)
		return m
	}())
	a, e := scalingInput(v, nullScaling(false))
	if e != nil {
		t.Fatal(e)
	}
	d := scalingMismatch(context.Background(), v, nullScaling(false), a)
	if d.HasError() {
		t.Fatal(d)
	}
}

func TestReadyToDeployLifecycleRejectedWithoutMutation(t *testing.T) {
	r := &Resource{}
	for _, desired := range []string{"RUNNING", "STOPPED"} {
		if _, err := r.lifecycle(context.Background(), Model{Desired: types.StringValue(desired)}, client.Service{ID: "undeployed", Status: "READY_TO_DEPLOY"}); err == nil {
			t.Fatalf("%s accepted on undeployed service", desired)
		}
	}
	if _, err := r.lifecycle(context.Background(), Model{Desired: types.StringNull()}, client.Service{Status: "READY_TO_DEPLOY"}); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshDoesNotAdoptEffectiveDefaultsAsOverrides(t *testing.T) {
	cpu := 3
	m := Model{Base: types.StringNull(), Desired: types.StringNull()}
	var d diag.Diagnostics
	m.refresh(client.Service{CurrentAutoscaling: &client.Autoscaling{Vertical: &client.VerticalAutoscaling{MaxResource: &client.ScalingResource{CPUCoreCount: &cpu}}}}, &d)
	if !m.Vertical.IsNull() || !m.Horizontal.IsNull() || m.Current.IsNull() {
		t.Fatal("effective defaults leaked into custom inputs")
	}
}
