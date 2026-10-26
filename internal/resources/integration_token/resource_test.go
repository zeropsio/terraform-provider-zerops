package integration_token

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

func TestRoleDerivation(t *testing.T) {
	for _, tc := range []struct {
		role   types.String
		config types.Bool
		want   types.Bool
	}{
		{types.StringValue("ADMIN"), types.BoolNull(), types.BoolValue(true)},
		{types.StringValue("OWNER"), types.BoolNull(), types.BoolValue(true)},
		{types.StringValue("READ_ONLY"), types.BoolNull(), types.BoolValue(false)},
		{types.StringValue("NO_ACCESS"), types.BoolValue(true), types.BoolValue(true)},
		{types.StringUnknown(), types.BoolNull(), types.BoolUnknown()},
		{types.StringValue("ADMIN"), types.BoolUnknown(), types.BoolUnknown()},
	} {
		if got := deriveCreate(tc.role, tc.config); !got.Equal(tc.want) {
			t.Fatalf("role %v: got %v want %v", tc.role, got, tc.want)
		}
	}
}

type fakeAPI struct {
	client.API
	metadata                 client.IntegrationToken
	gets, updates, rotations int
	getErr, rotateErr        error
	creates                  int
	createErr                error
	deletes                  int
	deleteErr                error
}

func (f *fakeAPI) CreateIntegrationToken(_ context.Context, _ string, w client.IntegrationTokenWrite) (client.IntegrationTokenRaw, error) {
	f.creates++
	f.metadata.IntegrationTokenWrite = w
	return client.IntegrationTokenRaw{IntegrationToken: f.metadata, Token: "created-secret"}, f.createErr
}
func (f *fakeAPI) GetIntegrationToken(context.Context, string, string) (client.IntegrationToken, error) {
	f.gets++
	return f.metadata, f.getErr
}
func (f *fakeAPI) UpdateIntegrationToken(_ context.Context, _, _ string, w client.IntegrationTokenWrite) (client.IntegrationToken, error) {
	f.updates++
	f.metadata.IntegrationTokenWrite = w
	return f.metadata, nil
}
func (f *fakeAPI) RegenerateIntegrationToken(context.Context, string, string) (client.IntegrationTokenRaw, error) {
	f.rotations++
	return client.IntegrationTokenRaw{IntegrationToken: f.metadata, Token: "rotated-secret"}, f.rotateErr
}
func (f *fakeAPI) DeleteIntegrationToken(context.Context, string, string) error {
	f.deletes++
	if f.gets == 0 {
		return errors.New("delete requires live token metadata")
	}
	if f.deleteErr == nil {
		f.getErr = &client.APIError{HTTPStatus: 400, Code: "clientUserConnectionNotFound"}
	}
	return f.deleteErr
}
func fixture(t *testing.T) (*Resource, *fakeAPI, model, tfsdk.State) {
	t.Helper()
	ctx := context.Background()
	a := &fakeAPI{metadata: client.IntegrationToken{ID: "t", IntegrationTokenWrite: client.IntegrationTokenWrite{Name: "name", RoleCode: "NO_ACCESS", Projects: []client.IntegrationTokenProject{}}}}
	r := &Resource{data: &providerdata.ProviderData{API: a, Locks: &locks.Keyed{}}}
	var s resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &s)
	m := model{ID: types.StringValue("o/t"), OrganizationID: types.StringValue("o"), Name: types.StringValue("name"), RoleCode: types.StringValue("NO_ACCESS"), Projects: types.SetValueMust(projectType, []attr.Value{}), CanCreateProjects: types.BoolValue(false), CanViewFinances: types.BoolValue(false), CanEditFinances: types.BoolValue(false), Sudo: types.BoolValue(false), RotationTrigger: types.MapValueMust(types.StringType, map[string]attr.Value{}), Token: types.StringValue("original-secret")}
	m.Timeouts = timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}
	state := tfsdk.State{Schema: s.Schema}
	if d := state.Set(ctx, &m); d.HasError() {
		t.Fatal(d)
	}
	return r, a, m, state
}
func TestReadRetainsSecretAndExactAbsence(t *testing.T) {
	for _, tc := range []struct {
		err          error
		absent, fail bool
	}{{nil, false, false}, {&client.APIError{HTTPStatus: 400, Code: "clientUserConnectionNotFound"}, true, false}, {&client.APIError{HTTPStatus: 404, Code: "clientUserConnectionNotFound"}, false, true}, {&client.APIError{HTTPStatus: 400, Code: "insufficientPermissions"}, false, true}} {
		r, a, _, state := fixture(t)
		a.getErr = tc.err
		p := resource.ReadResponse{State: state}
		r.Read(context.Background(), resource.ReadRequest{State: state}, &p)
		if p.Diagnostics.HasError() != tc.fail {
			t.Fatal(p.Diagnostics)
		}
		if p.State.Raw.IsNull() != tc.absent {
			t.Fatal("wrong absence classification")
		}
		if !tc.absent && !tc.fail {
			var got model
			p.State.Get(context.Background(), &got)
			if got.Token.ValueString() != "original-secret" {
				t.Fatal("lost secret")
			}
		}
	}
}
func TestUpdateRotationAndPartialPersistence(t *testing.T) {
	for _, tc := range []struct{ rotate, rename, failRead bool }{{false, true, false}, {true, false, false}, {true, true, true}, {false, false, false}} {
		r, a, m, state := fixture(t)
		if tc.rename {
			m.Name = types.StringValue("renamed")
		}
		if tc.rotate {
			m.RotationTrigger = types.MapValueMust(types.StringType, map[string]attr.Value{"generation": types.StringValue("2")})
			m.Token = types.StringUnknown()
		}
		plan := tfsdk.Plan{Schema: state.Schema}
		if d := plan.Set(context.Background(), &m); d.HasError() {
			t.Fatal(d)
		}
		if tc.failRead {
			a.getErr = errors.New("read unavailable")
		}
		p := resource.UpdateResponse{State: state}
		r.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: state}, &p)
		if p.Diagnostics.HasError() != tc.failRead {
			t.Fatal(p.Diagnostics)
		}
		if a.rotations != boolInt(tc.rotate) || a.updates != boolInt(tc.rename) {
			t.Fatalf("updates %d rotations %d", a.updates, a.rotations)
		}
		var got model
		p.State.Get(context.Background(), &got)
		want := "original-secret"
		if tc.rotate {
			want = "rotated-secret"
		}
		if got.Token.ValueString() != want {
			t.Fatal("secret not persisted")
		}
		if !got.RotationTrigger.Equal(m.RotationTrigger) {
			t.Fatal("rotation intent not persisted")
		}
	}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func TestImportSecretWarningAndValidation(t *testing.T) {
	r, a, _, state := fixture(t)
	p := resource.ImportStateResponse{State: state}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "o/t"}, &p)
	if p.Diagnostics.HasError() || len(p.Diagnostics) != 1 {
		t.Fatal(p.Diagnostics)
	}
	var got model
	p.State.Get(context.Background(), &got)
	if !got.Token.IsNull() || got.ID.ValueString() != "o/t" || len(got.RotationTrigger.Elements()) != 0 {
		t.Fatal("bad imported state")
	}
	if a.rotations != 0 {
		t.Fatal("import rotated")
	}
	for _, id := range []string{"t", "/t", "o/", "o/t/extra"} {
		p := resource.ImportStateResponse{State: state}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &p)
		if !p.Diagnostics.HasError() {
			t.Fatal("accepted malformed import")
		}
	}
}
func TestSchemaPermissionAndToken(t *testing.T) {
	r := NewResource()
	var p resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &p)
	a := p.Schema.Attributes["can_create_projects"]
	if !a.IsOptional() || !a.IsComputed() {
		t.Fatal("permission must be O+C")
	}
	if !p.Schema.Attributes["token"].IsSensitive() {
		t.Fatal("secret not sensitive")
	}
}

func TestCreatePersistsIdentityAndSecretOnPartialSuccess(t *testing.T) {
	ctx := context.Background()
	for _, fail := range []bool{false, true} {
		r, a, m, state := fixture(t)
		m.ID = types.StringUnknown()
		m.Token = types.StringUnknown()
		if fail {
			a.createErr = errors.New("transport interrupted after response")
		}
		plan := tfsdk.Plan{Schema: state.Schema}
		if d := plan.Set(ctx, &m); d.HasError() {
			t.Fatal(d)
		}
		p := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
		r.Create(ctx, resource.CreateRequest{Plan: plan}, &p)
		if p.Diagnostics.HasError() != fail {
			t.Fatal(p.Diagnostics)
		}
		var got model
		if d := p.State.Get(ctx, &got); d.HasError() {
			t.Fatal(d)
		}
		if got.ID.ValueString() != "o/t" || got.Token.ValueString() != "created-secret" || a.creates != 1 {
			t.Fatal("lost identity/secret or replayed create")
		}
	}
}

func TestRotationPersistsReturnedSecretBeforeError(t *testing.T) {
	ctx := context.Background()
	r, a, m, state := fixture(t)
	a.rotateErr = errors.New("transport interrupted after response")
	m.RotationTrigger = types.MapValueMust(types.StringType, map[string]attr.Value{"generation": types.StringValue("2")})
	m.Token = types.StringUnknown()
	plan := tfsdk.Plan{Schema: state.Schema}
	if d := plan.Set(ctx, &m); d.HasError() {
		t.Fatal(d)
	}
	p := resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, &p)
	if !p.Diagnostics.HasError() {
		t.Fatal("missing rotation error")
	}
	var got model
	if d := p.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if got.Token.ValueString() != "rotated-secret" || a.rotations != 1 || a.gets != 0 {
		t.Fatal("lost new secret or replayed rotation")
	}
}

func TestDeleteExactPresence(t *testing.T) {
	for _, tc := range []struct {
		name      string
		getErr    error
		deleteErr error
		fail      bool
		deletes   int
		gets      int
	}{
		{name: "present", deletes: 1, gets: 2},
		{name: "absent", getErr: &client.APIError{HTTPStatus: 400, Code: "clientUserConnectionNotFound"}, gets: 1},
		{name: "wrong status", getErr: &client.APIError{HTTPStatus: 404, Code: "clientUserConnectionNotFound"}, fail: true, gets: 1},
		{name: "permission denied", getErr: &client.APIError{HTTPStatus: 400, Code: "insufficientPermissions"}, fail: true, gets: 1},
		{name: "transport failed", getErr: errors.New("transport failed"), fail: true, gets: 1},
		{name: "delete disappearance race", deleteErr: &client.APIError{HTTPStatus: 400, Code: "clientUserConnectionNotFound"}, deletes: 1, gets: 1},
		{name: "delete failed", deleteErr: errors.New("delete failed"), fail: true, deletes: 1, gets: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, a, _, state := fixture(t)
			a.getErr, a.deleteErr = tc.getErr, tc.deleteErr
			resp := resource.DeleteResponse{State: state}
			r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != tc.fail {
				t.Fatal(resp.Diagnostics)
			}
			if a.deletes != tc.deletes || a.gets != tc.gets {
				t.Fatalf("deletes = %d, gets = %d; want %d, %d", a.deletes, a.gets, tc.deletes, tc.gets)
			}
		})
	}
}
