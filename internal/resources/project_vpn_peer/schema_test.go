package project_vpn_peer

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"testing"
)

func TestSchemaContract(t *testing.T) {
	r := &configResource{}
	var s resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	id := s.Schema.Attributes["id"].(schema.StringAttribute)
	project := s.Schema.Attributes["project_id"].(schema.StringAttribute)
	if !id.Computed || len(id.PlanModifiers) != 1 || !project.Required || len(project.PlanModifiers) != 1 || s.Schema.Blocks["timeouts"] == nil {
		t.Fatal("identity/timeouts contract")
	}
	key := s.Schema.Attributes["public_key"].(schema.StringAttribute)
	if !key.Required || len(key.PlanModifiers) != 1 || len(key.Validators) != 1 {
		t.Fatal("key schema deviates from contract")
	}
	for _, name := range []string{"ipv4_address", "ipv6_address", "setup", "setup_linux", "setup_macos", "setup_windows"} {
		a := s.Schema.Attributes[name].(schema.StringAttribute)
		if !a.Computed || a.Sensitive || len(a.PlanModifiers) != 0 {
			t.Fatalf("%s readback must remain public/drift-readable", name)
		}
	}

}
