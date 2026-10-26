package public_http_routing

import (
	"context"
	"encoding/json"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
)

func TestLocationConfigRoundTrip(t *testing.T) {
	ctx := context.Background()
	raw := map[string]any{
		"redirect":     map[string]any{"enabled": false, "to": "/new", "code": 302, "preservePath": true, "preserveQuery": false},
		"basicAuth":    map[string]any{"enabled": true, "realm": "Test", "users": []any{map[string]any{"username": "user", "password": "fixture", "comment": ""}}},
		"accessPolicy": map[string]any{"enabled": true, "cidr": []any{"192.0.2.0/24", "2001:db8::/32"}, "defaultPolicy": "deny", "realIpHeader": "X-Forwarded-For", "setRealIpFrom": nil},
		"content":      map[string]any{"enabled": false, "code": 200, "content": "Hello world!", "contentType": "text/plain"},
		"rateLimiting": map[string]any{"enabled": true, "key": "server_name", "zone": "zone", "rate": 10, "burst": 12, "zoneSize": 15},
	}
	v, err := configValue(ctx, locationConfigSchema().GetType(), raw)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := configWire(v)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(raw)
	b, _ := json.Marshal(wire)
	if string(a) != string(b) {
		t.Fatal("config round-trip changed values")
	}
	auth := locationConfigSchema().Attributes["basic_auth"].(schema.SingleNestedAttribute)
	users := auth.Attributes["users"].(schema.ListNestedAttribute)
	if !users.NestedObject.Attributes["password"].(schema.StringAttribute).Sensitive {
		t.Fatal("plaintext password is not sensitive")
	}
	m := Model{Location: []Location{{Config: v.(types.Object)}}}
	desired := client.HTTPRouteWrite{Locations: []client.HTTPLocation{{Config: raw}}}
	// A backend that retains cleared settings must fail verification rather than
	// silently adopting them and hiding an unsuccessful removal.
	if err := m.verify(ctx, client.HTTPRoute{ID: "r", IsSynced: true, Locations: desired.Locations}, client.HTTPRouteWrite{}); err == nil {
		t.Fatal("retained config was accepted")
	}
	empty, e := configValue(ctx, locationConfigSchema().GetType(), normalizedConfig(map[string]any{"redirect": nil, "basicAuth": nil, "content": nil, "accessPolicy": nil, "rateLimiting": nil}))
	if e != nil || !empty.IsNull() {
		t.Fatal("all-null config did not become absent")
	}
}

func TestConfigRequiresNonNullChild(t *testing.T) {
	ctx := context.Background()
	typ := locationConfigSchema().GetType().(types.ObjectType)
	attrs := map[string]attr.Value{}
	for k, child := range typ.AttrTypes {
		v, e := configValue(ctx, child, nil)
		if e != nil {
			t.Fatal(e)
		}
		attrs[k] = v
	}
	empty, d := types.ObjectValue(typ.AttrTypes, attrs)
	if d.HasError() {
		t.Fatal(d)
	}
	resp := validator.ObjectResponse{}
	configObjectValidator{}.ValidateObject(ctx, validator.ObjectRequest{ConfigValue: empty}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("empty config accepted")
	}
	attrs["content"] = types.ObjectUnknown(typ.AttrTypes["content"].(types.ObjectType).AttrTypes)
	unknown, d := types.ObjectValue(typ.AttrTypes, attrs)
	if d.HasError() {
		t.Fatal(d)
	}
	resp = validator.ObjectResponse{}
	configObjectValidator{}.ValidateObject(ctx, validator.ObjectRequest{ConfigValue: unknown}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal("unknown config was prematurely rejected")
	}
}
