package service_deploy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

func TestModifyPlanDetectsDirectoryChanges(t *testing.T) {
	ctx := context.Background()
	r := deployResource{}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	dir := t.TempDir()
	p := filepath.Join(dir, "file")
	if e := os.WriteFile(p, []byte("first"), 0644); e != nil {
		t.Fatal(e)
	}
	hash, e := directoryHash(dir)
	if e != nil {
		t.Fatal(e)
	}
	m := model{ID: types.StringValue("invocation"), ServiceID: types.StringValue("service"), SourceDir: types.StringValue(dir), Setup: types.StringValue("app"), SourceHash: types.StringValue(hash), Git: types.ObjectNull(map[string]attr.Type{"url": types.StringType, "ref": types.StringType, "mode": types.StringType, "auth_token": types.StringType}), Triggers: types.MapNull(types.StringType), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType})}}
	plan := tfsdk.Plan{Schema: sr.Schema}
	if d := plan.Set(ctx, m); d.HasError() {
		t.Fatal(d)
	}
	state := tfsdk.State{Schema: sr.Schema}
	if d := state.Set(ctx, m); d.HasError() {
		t.Fatal(d)
	}
	for _, changed := range []bool{false, true} {
		if changed {
			if e := os.WriteFile(p, []byte("second"), 0644); e != nil {
				t.Fatal(e)
			}
		}
		resp := resource.ModifyPlanResponse{Plan: plan}
		r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: plan, State: state}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		if changed != (len(resp.RequiresReplace) > 0) {
			t.Fatalf("changed=%t replacement paths=%v", changed, resp.RequiresReplace)
		}
		var got model
		if d := resp.Plan.Get(ctx, &got); d.HasError() {
			t.Fatal(d)
		}
		if changed == (got.SourceHash.ValueString() == hash) {
			t.Fatalf("changed=%t source_hash=%s", changed, got.SourceHash.ValueString())
		}
	}
	m.SourceDir = types.StringUnknown()
	m.Setup = types.StringUnknown()
	r.data = &providerdata.ProviderData{API: readAPI{service: client.Service{Name: "runtimehostname"}}}
	if d := plan.Set(ctx, m); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.ModifyPlanResponse{Plan: plan}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: plan, State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got model
	if d := resp.Plan.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if !got.SourceHash.IsUnknown() || len(resp.RequiresReplace) != 0 || got.Setup.ValueString() != "runtimehostname" {
		t.Fatalf("unknown source/default setup: %#v paths=%v", got, resp.RequiresReplace)
	}
}

func TestTimeoutOnlyUpdatePreservesState(t *testing.T) {
	ctx := context.Background()
	r := deployResource{}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	typ := map[string]attr.Type{"create": types.StringType}
	old := model{ID: types.StringValue("invocation"), ServiceID: types.StringValue("service"), Status: types.StringValue("ACTIVE"), Active: types.BoolValue(true), SourceHash: types.StringValue("hash"), Git: types.ObjectNull(map[string]attr.Type{"url": types.StringType, "ref": types.StringType, "mode": types.StringType, "auth_token": types.StringType}), Triggers: types.MapNull(types.StringType), Timeouts: timeouts.Value{Object: types.ObjectNull(typ)}}
	next := old
	next.Timeouts = timeouts.Value{Object: types.ObjectValueMust(typ, map[string]attr.Value{"create": types.StringValue("45m")})}
	next.ID = types.StringUnknown()
	next.Status = types.StringUnknown()
	next.Active = types.BoolUnknown()
	state := tfsdk.State{Schema: sr.Schema}
	plan := tfsdk.Plan{Schema: sr.Schema}
	if d := state.Set(ctx, old); d.HasError() {
		t.Fatal(d)
	}
	if d := plan.Set(ctx, next); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{State: state, Plan: plan}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	old.Timeouts = next.Timeouts
	want := tfsdk.State{Schema: sr.Schema}
	if d := want.Set(ctx, old); d.HasError() {
		t.Fatal(d)
	}
	if !resp.State.Raw.Equal(want.Raw) {
		t.Fatalf("update changed non-timeout state: %v", resp.State.Raw)
	}
}
