package object_storage

import (
	"context"
	"errors"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

func TestPolicyRemovalUsesConfig(t *testing.T) {
	ctx := context.Background()
	r := Resource{}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	for _, tc := range []struct {
		raw  jsontypes.Normalized
		want types.String
	}{
		{jsontypes.NewNormalizedNull(), types.StringValue("private")},
		{jsontypes.NewNormalizedValue(`{}`), types.StringValue("custom")},
		{jsontypes.NewNormalizedUnknown(), types.StringUnknown()},
	} {
		m := model{Policy: types.StringValue("public-read"), Raw: tc.raw, Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
		plan := tfsdk.Plan{Schema: sr.Schema}
		if d := plan.Set(ctx, m); d.HasError() {
			t.Fatal(d)
		}
		m.Policy = types.StringNull()
		config := tfsdk.Config{Schema: sr.Schema}
		state := tfsdk.State{Schema: sr.Schema}
		if d := state.Set(ctx, m); d.HasError() {
			t.Fatal(d)
		}
		config.Raw = state.Raw
		resp := resource.ModifyPlanResponse{Plan: plan}
		r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: plan, Config: config}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		var got model
		resp.Plan.Get(ctx, &got)
		if !got.Policy.Equal(tc.want) {
			t.Fatalf("policy=%v want=%v", got.Policy, tc.want)
		}
	}
}

type revealStorageAPI struct {
	storageAPI
	err error
}

func (a revealStorageAPI) RevealUserData(context.Context, string) (string, error) { return "", a.err }
func TestStorageRevealErrors(t *testing.T) {
	for _, e := range []error{&client.APIError{HTTPStatus: 403, Code: "insufficientPermissions"}, &client.APIError{HTTPStatus: 500, Code: "notInSudoMode"}, errors.New("transport")} {
		r := Resource{data: &providerdata.ProviderData{API: &revealStorageAPI{err: e}}}
		m := model{ID: types.StringValue("s"), Policy: types.StringValue("private")}
		var d diag.Diagnostics
		if _, err := r.refresh(context.Background(), &m, &d, false); !errors.Is(err, e) {
			t.Fatalf("lost reveal error: %v", err)
		}
	}
}
