package service_env_vars

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"os"
	"testing"
	"time"
)

func TestAccServiceEnvVars(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheck(t)
	project := acctest.CreateTestProject(t)
	c := acctest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	s, e := c.CreateService(ctx, project.ID, acctest.MinimalServiceInput(client.ServiceCreate{Name: acctest.RandomHostname(), ServiceStackVersionName: "valkey:single@7.2"}))
	if e != nil {
		t.Fatal(e)
	}
	if s.Process != nil {
		if _, e = waiter.WaitProcess(ctx, c, s.Process.ID); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = waiter.WaitServiceSettled(ctx, c, s.ID, []string{"ACTIVE"}, waiter.ProcessIDs{}, nil); e != nil {
		t.Fatal(e)
	}
	p, e := c.CreateUserData(ctx, s.ID, client.EnvWrite{Key: "TFACC_EXACT_KEY", Content: "map-value"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = waiter.WaitProcess(ctx, c, p.ID); e != nil {
		t.Fatal(e)
	}
	cfg := acctest.ProviderConfig() + fmt.Sprintf(`data "zerops_service_env_vars" "test" { service_id = %q }`, s.ID)
	run := func(t *testing.T) {
		resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{DataSources: []func() datasource.DataSource{NewDataSource}}), Steps: []resource.TestStep{{Config: cfg, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("data.zerops_service_env_vars.test", "id", s.ID), resource.TestCheckResourceAttr("data.zerops_service_env_vars.test", "vars.TFACC_EXACT_KEY", "map-value"), resource.TestCheckResourceAttrSet("data.zerops_service_env_vars.test", "vars.connectionString"))}}})
	}
	t.Run("sudo", run)
	t.Run("nonsudo", func(t *testing.T) {
		token := os.Getenv("ZEROPS_ACC_NONSUDO_TOKEN")
		if token == "" {
			t.Skip("ZEROPS_ACC_NONSUDO_TOKEN is not configured")
		}
		t.Setenv("ZEROPS_TOKEN", token)
		run(t)
	})
}
