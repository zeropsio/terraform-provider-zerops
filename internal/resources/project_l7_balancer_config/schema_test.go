package project_l7_balancer_config

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
	values := s.Schema.Attributes["values"].(schema.MapAttribute)
	defaults := s.Schema.Attributes["defaults"].(schema.MapAttribute)
	if !values.Required || len(values.Validators) != 2 || !defaults.Computed || len(defaults.PlanModifiers) != 0 {
		t.Fatal("balancer schema deviates from contract")
	}

}
