package service_backup_config

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	testingresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"os"
	"testing"
	"time"
)

func TestAccServiceBackupConfig(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	project := acctest.CreateTestProject(t)
	api := acctest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	created, err := api.CreateService(ctx, project.ID, acctest.MinimalServiceInput(client.ServiceCreate{Name: acctest.RandomHostname(), ServiceStackVersionName: "valkey:single@7.2"}))
	if err != nil {
		t.Fatal(err)
	}
	opts := waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute)
	if created.Process != nil {
		if _, err = waiter.WaitProcess(ctx, api, created.Process.ID, opts); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = waiter.WaitServiceSettled(ctx, api, created.ID, []string{"ACTIVE"}, nil, nil, opts); err != nil {
		t.Fatal(err)
	}
	config := func(cron string, files int) string {
		return acctest.ProviderConfig() + fmt.Sprintf(`resource "zerops_service_backup_config" "test" {
 service_id=%q
 backup_period=%q
 retention_policy {
 max_total_files=%d
 max_total_gib=1
 protected_tags=["tfacc"]
 min_daily=1
 max_daily=3
 min_weekly=1
 max_weekly=2
 min_monthly=1
 max_monthly=2
 }
}
`, created.ID, cron, files)
	}
	first := config("15 3 * * *", 10)
	second := config("25 4 * * *", 12)
	reset := acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_service_backup_config\" \"test\" {\nservice_id=%q\n}\n", created.ID)
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), CheckDestroy: func(_ *terraform.State) error {
		v, e := api.GetBackupConfig(context.Background(), created.ID)
		if e != nil {
			return e
		}
		if v.BackupPeriod != nil || v.RetentionPolicy != nil || v.DefaultRetentionPolicy == nil {
			return fmt.Errorf("delete did not reset backup configuration")
		}
		return nil
	}, Steps: []testingresource.TestStep{
		{Config: first, Check: testingresource.TestCheckResourceAttr("zerops_service_backup_config.test", "retention_policy.max_total_files", "10")},
		{Config: first, PlanOnly: true},
		{ResourceName: "zerops_service_backup_config.test", ImportState: true, ImportStateVerify: true},
		{Config: second, Check: testingresource.TestCheckResourceAttr("zerops_service_backup_config.test", "backup_period", "25 4 * * *")},
		{Config: second, PreConfig: func() {
			p, e := api.SetBackupConfig(ctx, created.ID, client.BackupConfigWrite{})
			if e != nil {
				t.Fatal(e)
			}
			if p != nil {
				if _, e = waiter.WaitProcess(ctx, api, p.ID, opts); e != nil {
					t.Fatal(e)
				}
			}
		}, PlanOnly: true, ExpectNonEmptyPlan: true},
		{Config: second},
		{Config: reset, Check: testingresource.TestCheckNoResourceAttr("zerops_service_backup_config.test", "backup_period")},
		{Config: reset, PlanOnly: true},
	}})
}
