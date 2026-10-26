package project_vpn_peer

import (
	"context"
	"errors"
	"reflect"
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

type deleteAPI struct {
	client.API
	getErr, deleteErr, finalErr error
	calls                       []string
}

func (a *deleteAPI) GetProject(_ context.Context, id string) (client.Project, error) {
	a.calls = append(a.calls, "get:"+id)
	return client.Project{ID: id}, a.getErr
}
func (a *deleteAPI) DeleteVPNPeer(_ context.Context, id, key string) error {
	a.calls = append(a.calls, "delete:"+id+"/"+key)
	return a.deleteErr
}
func (a *deleteAPI) GetVPNPeer(_ context.Context, id, key string) (client.VPNPeer, error) {
	a.calls = append(a.calls, "peer:"+id+"/"+key)
	if a.finalErr != nil {
		return client.VPNPeer{}, a.finalErr
	}
	return client.VPNPeer{}, &client.APIError{HTTPStatus: 400, Code: "vpnPublicKeyNotFound"}
}

func TestDeleteParentPreflight(t *testing.T) {
	absent := &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}
	peerAbsent := &client.APIError{HTTPStatus: 400, Code: "vpnPublicKeyNotFound"}
	denied := &client.APIError{HTTPStatus: 403, Code: "forbidden"}
	for _, tc := range []struct {
		name                        string
		getErr, deleteErr, finalErr error
		bad                         bool
		calls                       []string
	}{
		{"absent parent", absent, nil, nil, false, []string{"get:p"}},
		{"unexpected read error", errors.New("read failed"), nil, nil, true, []string{"get:p"}},
		{"transport error", &client.TransportError{}, nil, nil, true, []string{"get:p"}},
		{"permission error", denied, nil, nil, true, []string{"get:p"}},
		{"wrong status", &client.APIError{HTTPStatus: 403, Code: "projectNotFound"}, nil, nil, true, []string{"get:p"}},
		{"wrong absence code", peerAbsent, nil, nil, true, []string{"get:p"}},
		{"generic not found", &client.APIError{HTTPStatus: 404, Code: "notFound"}, nil, nil, true, []string{"get:p"}},
		{"normal removal", nil, nil, nil, false, []string{"get:p", "delete:p/key", "peer:p/key"}},
		{"parent absence race", nil, absent, nil, false, []string{"get:p", "delete:p/key"}},
		{"peer already absent", nil, peerAbsent, nil, false, []string{"get:p", "delete:p/key"}},
		{"delete error", nil, denied, nil, true, []string{"get:p", "delete:p/key"}},
		{"wait parent absence", nil, nil, absent, false, []string{"get:p", "delete:p/key", "peer:p/key"}},
		{"wait read error", nil, nil, denied, true, []string{"get:p", "delete:p/key", "peer:p/key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			api := &deleteAPI{getErr: tc.getErr, deleteErr: tc.deleteErr, finalErr: tc.finalErr}
			r := &configResource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}}}
			var schema resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schema)
			m := model{ID: types.StringValue("p/key"), ProjectID: types.StringValue("p"), PublicKey: types.StringValue("key"), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
			state := tfsdk.State{Schema: schema.Schema}
			if d := state.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.DeleteResponse{State: state}
			r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != tc.bad || !reflect.DeepEqual(api.calls, tc.calls) {
				t.Fatalf("diagnostics=%v calls=%v, want error=%v calls=%v", resp.Diagnostics, api.calls, tc.bad, tc.calls)
			}
		})
	}
}
