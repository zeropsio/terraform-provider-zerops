package project_remote_logging

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
	config := s.Schema.Attributes["syslog_config"].(schema.StringAttribute)
	cert := s.Schema.Attributes["certificate"].(schema.StringAttribute)
	typ := s.Schema.Attributes["type"].(schema.StringAttribute)
	if !config.Required || !config.Sensitive || !cert.Optional || !cert.Computed || !cert.Sensitive || cert.Default == nil || !typ.Optional || typ.Default != nil {
		t.Fatal("logging schema deviates from contract")
	}

}
