package service

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
)

func TestPortsSortedAndDescription(t *testing.T) {
	ports := []client.Port{{Protocol: "udp", Port: 8080}, {Protocol: "tcp", Port: 4000}, {Protocol: "tcp", Port: 3000, Scheme: "http", Description: "application"}}
	v := portsValue(ports)
	if len(v.Elements()) != 3 {
		t.Fatal(v)
	}
	first := v.Elements()[0].(types.Object)
	if first.Attributes()["port"].(types.Int64).ValueInt64() != 3000 || first.Attributes()["description"].(types.String).ValueString() != "application" {
		t.Fatal(first)
	}
	if ports[0].Protocol != "udp" {
		t.Fatal("mapping mutated API model")
	}
	if portsValue(nil).IsNull() {
		t.Fatal("empty ports must be an empty list")
	}
}
