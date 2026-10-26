package public_port_routing

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"strings"
	"testing"
)

func TestIPv4ReadWriteAndValidation(t *testing.T) {
	ctx := context.Background()
	var m Model
	v := client.PortRoute{ID: "route", ServiceStackID: "service", IsSynced: true, PortRouteWrite: client.PortRouteWrite{PublicIPType: "IP_V4", PublicPort: 23084, InternalPort: 3000, InternalProtocol: "tcp", FirewallPolicy: "BLACKLIST", FirewallIPRanges: []string{}}}
	if err := m.read(ctx, v); err != nil {
		t.Fatal(err)
	}
	if m.PublicIPType.ValueString() != "IP_V4" {
		t.Fatal("IPv4 read/import lost type")
	}
	w, err := m.write(ctx)
	if err != nil || w.PublicIPType != "IP_V4" || w.FirewallAllowMyIP {
		t.Fatalf("write=%+v err=%v", w, err)
	}
	var sr resource.SchemaResponse
	(&Resource{}).Schema(ctx, resource.SchemaRequest{}, &sr)
	a := sr.Schema.Attributes["public_ip_type"]
	for _, value := range []string{"IP_V4", "IP_V6", "IP_V5"} {
		for _, v := range a.(schema.StringAttribute).Validators {
			resp := validator.StringResponse{}
			v.ValidateString(ctx, validator.StringRequest{ConfigValue: types.StringValue(value)}, &resp)
			if resp.Diagnostics.HasError() != (value == "IP_V5") {
				t.Fatalf("validation %s: %v", value, resp.Diagnostics)
			}
		}
	}
}
func TestIPv4PrerequisiteDiagnostic(t *testing.T) {
	e := &client.APIError{HTTPStatus: 400, Code: "publicIpTypeNotSupported"}
	if detail := portMutationError(e); !strings.Contains(detail, "dedicated project IPv4") || !strings.Contains(detail, "shared IPv4 flag is insufficient") {
		t.Fatal(detail)
	}
	e.HTTPStatus = 403
	if portMutationError(e) != e.Error() {
		t.Fatal("wrong-status error specialized")
	}
}
