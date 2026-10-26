package integration_token

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestModifyPlanReDerivesAndRotatesInPlace(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		role            string
		configured      types.Bool
		rotate, unknown bool
		want            types.Bool
	}{{"READ_ONLY", types.BoolNull(), false, false, types.BoolValue(false)}, {"ADMIN", types.BoolNull(), false, false, types.BoolValue(true)}, {"READ_ONLY", types.BoolValue(true), true, false, types.BoolValue(true)}, {"READ_ONLY", types.BoolUnknown(), false, true, types.BoolUnknown()}} {
		r, _, m, state := fixture(t)
		m.RoleCode = types.StringValue(tc.role)
		configModel := m
		configModel.CanCreateProjects = tc.configured
		configModel.OrganizationID = types.StringNull()
		configState := tfsdk.State{Schema: state.Schema}
		if d := configState.Set(ctx, &configModel); d.HasError() {
			t.Fatal(d)
		}
		m.CanCreateProjects = types.BoolValue(true)
		m.Token = types.StringUnknown()
		if tc.rotate {
			m.RotationTrigger = types.MapValueMust(types.StringType, map[string]attr.Value{"generation": types.StringValue("2")})
		}
		if tc.unknown {
			m.RotationTrigger = types.MapUnknown(types.StringType)
		}
		plan := tfsdk.Plan{Schema: state.Schema}
		if d := plan.Set(ctx, &m); d.HasError() {
			t.Fatal(d)
		}
		p := resource.ModifyPlanResponse{Plan: plan}
		r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: plan, State: state, Config: tfsdk.Config{Schema: state.Schema, Raw: configState.Raw}}, &p)
		if p.Diagnostics.HasError() {
			t.Fatal(p.Diagnostics)
		}
		var got model
		p.Plan.Get(ctx, &got)
		if !got.CanCreateProjects.Equal(tc.want) {
			t.Fatalf("derived %v want %v", got.CanCreateProjects, tc.want)
		}
		if got.Token.IsUnknown() != (tc.rotate || tc.unknown) {
			t.Fatal("incorrect secret plan")
		}
		if len(p.RequiresReplace) != 0 {
			t.Fatal("rotation must not replace")
		}
	}
}

func TestPermissionAndDuplicateValidation(t *testing.T) {
	ctx := context.Background()
	for _, duplicate := range []bool{false, true} {
		r, _, m, state := fixture(t)
		if duplicate {
			ps := []projectModel{{types.StringValue("p"), types.StringValue("READ_ONLY")}, {types.StringValue("p"), types.StringValue("NO_ACCESS")}}
			m.Projects, _ = types.SetValueFrom(ctx, projectType, ps)
		} else {
			m.RoleCode = types.StringValue("ADMIN")
			m.CanCreateProjects = types.BoolValue(false)
		}
		s := tfsdk.State{Schema: state.Schema}
		if d := s.Set(ctx, &m); d.HasError() {
			t.Fatal(d)
		}
		p := resource.ValidateConfigResponse{}
		r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: s.Raw}}, &p)
		if !p.Diagnostics.HasError() {
			t.Fatal("invalid permission/scope accepted")
		}
	}
}

func TestPayloadIsCompleteAndScopeOrderStable(t *testing.T) {
	ctx := context.Background()
	_, _, m, _ := fixture(t)
	ps := []projectModel{{types.StringValue("z"), types.StringValue("READ_ONLY")}, {types.StringValue("a"), types.StringValue("BASIC_USER")}}
	m.Projects, _ = types.SetValueFrom(ctx, projectType, ps)
	m.Sudo = types.BoolValue(true)
	m.CanEditFinances = types.BoolValue(true)
	v, d := m.payload(ctx)
	if d.HasError() {
		t.Fatal(d)
	}
	if v.Name != "name" || v.RoleCode != "NO_ACCESS" || v.CanCreateProjects || v.CanViewFinances || !v.CanEditFinances || !v.Sudo || len(v.Projects) != 2 || v.Projects[0].ProjectID != "a" || v.Projects[1].RoleCode != "READ_ONLY" {
		t.Fatal("incorrect complete payload")
	}
}
