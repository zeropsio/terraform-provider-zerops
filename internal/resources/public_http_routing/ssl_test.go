package public_http_routing

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"strings"
	"testing"
)

type sslResourceAPI struct{ recoveryAPI }

func (f *sslResourceAPI) TriggerHTTPDomainsDNSCheck(context.Context, string) error {
	f.calls = append(f.calls, "trigger")
	return nil
}
func TestSSLWaitOnlyUpdates(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after bool
		trigger       bool
	}{{"enable", false, true, true}, {"disable", true, false, false}, {"timeout only", true, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			api := &sslResourceAPI{recoveryAPI: recoveryAPI{foreign: true, v: client.HTTPRoute{ID: "route", ProjectID: "project", IsEditable: true, IsSynced: true, SSLEnabled: true, Domains: []client.HTTPDomain{{DomainName: "a.example.com", SSLStatus: "ACTIVE"}}, Locations: []client.HTTPLocation{{Path: "/", Port: 3000, ServiceStackID: "service"}}}}}
			r := &Resource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}}}
			var sr resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &sr)
			var m Model
			if err := m.read(ctx, api.v); err != nil {
				t.Fatal(err)
			}
			m.WaitForSSL = types.BoolValue(tc.before)
			m.SSLTimeout = types.StringValue("1m")
			m.Timeouts = timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}
			state := tfsdk.State{Schema: sr.Schema}
			if d := state.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			m.WaitForSSL = types.BoolValue(tc.after)
			m.SSLTimeout = types.StringValue("2m")
			plan := tfsdk.Plan{Schema: sr.Schema}
			if d := plan.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.UpdateResponse{State: state}
			r.Update(ctx, resource.UpdateRequest{State: state, Plan: plan}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			calls := strings.Join(api.calls, ",")
			if strings.Contains(calls, "update") || strings.Contains(calls, "sync") || strings.Contains(calls, "restore") {
				t.Fatalf("state-only update staged routing: %s", calls)
			}
			if strings.Contains(calls, "trigger") != tc.trigger {
				t.Fatalf("unexpected trigger calls: %s", calls)
			}
		})
	}
}

func TestSSLToggleValidation(t *testing.T) {
	ctx := context.Background()
	r := &Resource{}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	for _, enabled := range []types.Bool{types.BoolValue(false), types.BoolValue(true), types.BoolUnknown(), types.BoolNull()} {
		m := Model{WaitForSSL: types.BoolValue(true), SSLEnabled: enabled, Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
		m.Domains = types.SetNull(types.StringType)
		m.DomainStatuses = types.ListNull(statusType)
		state := tfsdk.State{Schema: sr.Schema}
		if d := state.Set(ctx, &m); d.HasError() {
			t.Fatal(d)
		}
		resp := resource.ValidateConfigResponse{}
		r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: state.Raw}}, &resp)
		want := !enabled.IsNull() && !enabled.IsUnknown() && !enabled.ValueBool()
		if resp.Diagnostics.HasError() != want {
			t.Fatalf("enabled=%v diagnostics=%v", enabled, resp.Diagnostics)
		}
	}
}

func TestLocationValidationDefersUnknowns(t *testing.T) {
	ctx := context.Background()
	r := &Resource{}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	locationType := sr.Schema.Attributes["location"].GetType().(types.ListType).ElemType.(types.ObjectType)
	for name, locations := range map[string]types.List{
		"unknown list":    types.ListUnknown(locationType),
		"unknown element": types.ListValueMust(locationType, []attr.Value{types.ObjectUnknown(locationType.AttrTypes)}),
		"null list":       types.ListNull(locationType),
	} {
		t.Run(name, func(t *testing.T) {
			m := Model{
				WaitForSSL: types.BoolValue(false), SSLEnabled: types.BoolValue(true),
				Domains: types.SetNull(types.StringType), DomainStatuses: types.ListNull(statusType),
				Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})},
			}
			state := tfsdk.State{Schema: sr.Schema}
			if d := state.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			if d := state.SetAttribute(ctx, path.Root("location"), locations); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.ValidateConfigResponse{}
			r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: state.Raw}}, &resp)
			if len(resp.Diagnostics) != 0 {
				t.Fatal(resp.Diagnostics)
			}
		})
	}
}
