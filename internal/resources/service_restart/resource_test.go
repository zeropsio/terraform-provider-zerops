package service_restart

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

type restartAPI struct {
	client.API
	restarts, reads int
}

func (f *restartAPI) GetService(context.Context, string) (client.Service, error) {
	f.reads++
	return client.Service{ID: "s", Status: "ACTIVE"}, nil
}
func (f *restartAPI) ListServiceProcesses(context.Context, string, client.PageQuery) (client.Page[client.Process], error) {
	return client.Page[client.Process]{}, nil
}
func (f *restartAPI) RestartService(context.Context, string) (client.Process, error) {
	f.restarts++
	return client.Process{ID: "restart", Status: "PENDING"}, nil
}
func (f *restartAPI) GetProcess(context.Context, string) (client.Process, error) {
	return client.Process{ID: "restart", ActionName: "stack.restart", Status: "FAILED", Error: &client.ProcessError{Code: "initialProcessStatusError"}}, nil
}
func testModel() model {
	return model{ID: types.StringUnknown(), ServiceID: types.StringValue("s"), Triggers: types.MapValueMust(types.StringType, map[string]attr.Value{"version": types.StringValue("one")}), LastRestart: types.StringUnknown(), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType})}}
}
func TestRestartRetainsIdentityOnProcessFailure(t *testing.T) {
	ctx := context.Background()
	f := &restartAPI{}
	r := Resource{data: &providerdata.ProviderData{API: f, Locks: &locks.Keyed{}}}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	plan := tfsdk.Plan{Schema: schema.Schema}
	if d := plan.Set(ctx, testModel()); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schema.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("failed process succeeded")
	}
	var got model
	if d := resp.State.Get(ctx, &got); d.HasError() || got.ID.ValueString() != "restart" || got.LastRestart.ValueString() != "restart" {
		t.Fatal(d, got)
	}
	if f.restarts != 1 {
		t.Fatal("restart replayed")
	}
	read := resource.ReadResponse{State: resp.State}
	r.Read(ctx, resource.ReadRequest{State: resp.State}, &read)
	if read.Diagnostics.HasError() || f.restarts != 1 {
		t.Fatal("read restarted", read.Diagnostics)
	}
	r.Delete(ctx, resource.DeleteRequest{State: resp.State}, &resource.DeleteResponse{})
	if f.restarts != 1 {
		t.Fatal("delete restarted")
	}
}
func TestRestartSchemaReplacementAndNoImport(t *testing.T) {
	r := NewResource()
	if _, ok := r.(resource.ResourceWithImportState); ok {
		t.Fatal("restart must not import")
	}
	var s resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	service := s.Schema.Attributes["service_id"].GetType()
	if !service.Equal(types.StringType) {
		t.Fatal("wrong service type")
	}
	if len(s.Schema.Blocks) != 1 {
		t.Fatal("unexpected lifecycle blocks")
	}
}

func TestRestartPlanIdentityOnReplacement(t *testing.T) {
	ctx := context.Background()
	r := &Resource{}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	prior := testModel()
	prior.ID = types.StringValue("old")
	prior.LastRestart = prior.ID
	state := tfsdk.State{Schema: schema.Schema}
	if d := state.Set(ctx, prior); d.HasError() {
		t.Fatal(d)
	}
	for _, field := range []string{"unchanged", "triggers", "service"} {
		t.Run(field, func(t *testing.T) {
			m := prior
			switch field {
			case "triggers":
				m.Triggers = types.MapValueMust(types.StringType, map[string]attr.Value{"version": types.StringValue("two")})
			case "service":
				m.ServiceID = types.StringValue("other")
			}
			plan := tfsdk.Plan{Schema: schema.Schema}
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
			if field == "unchanged" {
				if got.ID.ValueString() != "old" || got.LastRestart.ValueString() != "old" {
					t.Fatal("nonreplacement lost identity")
				}
			} else if !got.ID.IsUnknown() || !got.LastRestart.IsUnknown() {
				t.Fatal("replacement retained old process identity")
			}
		})
	}
}
