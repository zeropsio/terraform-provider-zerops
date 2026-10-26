package project

import (
	"context"
	"errors"
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

type partialAPI struct {
	client.API
	wrote bool
}

func (a *partialAPI) UpdateProject(context.Context, string, client.ProjectUpdate) (client.Project, error) {
	a.wrote = true
	return client.Project{}, nil
}
func (a *partialAPI) UpgradeProject(context.Context, string, string) (*client.Process, error) {
	return nil, errors.New("upgrade failed")
}
func (a *partialAPI) GetProject(context.Context, string) (client.Project, error) {
	return client.Project{}, errors.New("read failed")
}
func (a *partialAPI) ListProjectEnv(context.Context, string) ([]client.Env, error) {
	return nil, errors.New("isolation failed")
}
func TestUpdatePersistsCommittedPUT(t *testing.T) {
	for _, failure := range []string{"upgrade", "isolation", "read"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			api := &partialAPI{}
			r := Resource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}}}
			var sr resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &sr)
			old := Model{ID: types.StringValue("p"), Name: types.StringValue("old"), Mode: types.StringValue("LIGHT"), Tags: types.SetValueMust(types.StringType, nil), PublicIPv4Shared: types.BoolValue(false), MaxCreditLimit: types.StringValue("10"), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
			next := old
			next.Name = types.StringValue("new")
			next.Description = types.StringValue("description")
			next.MaxCreditLimit = types.StringValue("25")
			next.PublicIPv4Shared = types.BoolValue(true)
			next.Tags = types.SetValueMust(types.StringType, []attr.Value{types.StringValue("tag")})
			if failure == "upgrade" {
				next.Mode = types.StringValue("SERIOUS")
			}
			if failure == "isolation" {
				next.EnvIsolation = types.StringValue("project")
			}
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
			if !api.wrote || !resp.Diagnostics.HasError() {
				t.Fatalf("missing committed PUT or later failure: %v", resp.Diagnostics)
			}
			var got Model
			if d := resp.State.Get(ctx, &got); d.HasError() {
				t.Fatal(d)
			}
			if !got.Name.Equal(next.Name) || !got.Description.Equal(next.Description) || !got.Tags.Equal(next.Tags) || !got.PublicIPv4Shared.Equal(next.PublicIPv4Shared) || !got.MaxCreditLimit.Equal(next.MaxCreditLimit) || !got.Mode.Equal(old.Mode) || !got.EnvIsolation.Equal(old.EnvIsolation) {
				t.Fatalf("incorrect partial state: %+v", got)
			}
		})
	}
}
