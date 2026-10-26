package service_deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

// Unlike an owned object, purging an invocation must not recreate application code.
func TestAccServiceDeployParentDisappears(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	s := accService(t)
	dir, e := filepath.Abs("testdata/app")
	if e != nil {
		t.Fatal(e)
	}
	yaml, e := os.ReadFile("testdata/app/zerops.yml")
	if e != nil {
		t.Fatal(e)
	}
	config := accConfig(s.ID, fmt.Sprintf("source_dir = %q", dir), string(yaml), false)
	tfresource.Test(t, tfresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(accFactories()), Steps: []tfresource.TestStep{
		{Config: config, Check: tfresource.TestCheckResourceAttr("zerops_service_deploy.app", "active", "true")},
		{PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			c := acctest.Client(t)
			p, e := c.DeleteService(ctx, s.ID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = waiter.WaitProcess(ctx, c, p.ID); e != nil {
				t.Fatal(e)
			}
			if e = waiter.WaitNotFound(ctx, s.ID, func(ctx context.Context) error { _, e := c.GetService(ctx, s.ID); return e }, []string{"serviceStackNotFound"}); e != nil {
				t.Fatal(e)
			}
		}, RefreshState: true, Check: tfresource.ComposeTestCheckFunc(tfresource.TestCheckResourceAttr("zerops_service_deploy.app", "active", "false"), tfresource.TestCheckResourceAttr("zerops_service_deploy.app", "status", "PURGED"))},
	}})
}
