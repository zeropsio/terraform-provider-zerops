package service_restart

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
	"regexp"
	"testing"
	"time"
)

func TestAccServiceRestart(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	project := acctest.CreateTestProject(t)
	api := acctest.Client(t)
	service := acctest.DeployHTTPApp(t, project.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	opts := waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute)
	// Reuse the deployed runtime for backup capability rejection; do not build
	// another runtime merely for a negative backup test.
	cron := "15 3 * * *"
	if _, err := api.SetBackupConfig(ctx, service.ID, client.BackupConfigWrite{BackupPeriod: &cron}); err == nil {
		t.Fatal("runtime unexpectedly accepted backup configuration")
	} else if apiError, ok := client.AsAPIError(err); !ok || apiError.Code != "backupUnsupportedStackType" {
		t.Fatal(err)
	}
	config := func(version string) string {
		return acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_service_restart\" \"test\" {\nservice_id=%q\ntriggers={version=%q}\n}\n", service.ID, version)
	}
	count := func() int {
		p, e := api.AllServiceProcesses(ctx, service.ID)
		if e != nil {
			t.Fatal(e)
		}
		n := 0
		for _, p := range p {
			if p.ActionName == "stack.restart" {
				n++
			}
		}
		return n
	}
	before := count()
	lastID := ""
	check := func(expected int) testingresource.TestCheckFunc {
		return func(s *terraform.State) error {
			if n := count(); n != before+expected {
				return fmt.Errorf("restart count %d, want %d", n, before+expected)
			}
			row := s.RootModule().Resources["zerops_service_restart.test"].Primary
			id := row.ID
			if id == "" || row.Attributes["last_restart"] != id {
				return fmt.Errorf("missing process identity")
			}
			if expected > 1 && lastID == id {
				return fmt.Errorf("replacement retained old process identity")
			}
			lastID = id
			v, e := api.GetService(ctx, service.ID)
			if e != nil {
				return e
			}
			if v.Status != "ACTIVE" {
				return fmt.Errorf("restart left status %s", v.Status)
			}
			return nil
		}
	}
	env, e := api.CreateUserData(ctx, service.ID, client.EnvWrite{Key: "TFACC_RESTART", Content: "one"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = waiter.WaitProcess(ctx, api, env.ID, opts); e != nil {
		t.Fatal(e)
	}
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), CheckDestroy: func(_ *terraform.State) error {
		v, e := api.GetService(ctx, service.ID)
		if e == nil && v.Status != "ACTIVE" {
			return fmt.Errorf("local restart delete altered service")
		}
		return e
	}, Steps: []testingresource.TestStep{
		{Config: config("one"), Check: check(1)},
		{Config: config("one"), PlanOnly: true, Check: func(_ *terraform.State) error {
			if count() != before+1 {
				return fmt.Errorf("no-op restarted service")
			}
			return nil
		}},
		{Config: config("two"), PreConfig: func() {
			rows, e := api.ListServiceEnv(ctx, service.ID)
			if e != nil {
				t.Fatal(e)
			}
			for _, row := range rows {
				if row.Key == "TFACC_RESTART" {
					p, e := api.UpdateUserData(ctx, row.ID, client.EnvWrite{Key: row.Key, Content: "two"})
					if e != nil {
						t.Fatal(e)
					}
					if _, e = waiter.WaitProcess(ctx, api, p.ID, opts); e != nil {
						t.Fatal(e)
					}
					return
				}
			}
			t.Fatal("missing restart env fixture")
		}, Check: check(2)},
		{Config: config("three"), PreConfig: func() {
			p, e := api.StopService(ctx, service.ID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = waiter.WaitProcess(ctx, api, p.ID, opts); e != nil {
				t.Fatal(e)
			}
			if _, e = waiter.WaitServiceSettled(ctx, api, service.ID, []string{"STOPPED"}, nil, []string{p.ID}, opts); e != nil {
				t.Fatal(e)
			}
		}, Check: check(3)},
	}})
	// An undeployed service is not a stopped service and must not be deployed implicitly.
	dormant, e := api.CreateService(ctx, project.ID, acctest.MinimalServiceInput(client.ServiceCreate{Name: acctest.RandomHostname(), ServiceStackVersionName: "alpine/nodejs@22"}))
	if e != nil {
		t.Fatal(e)
	}
	if dormant.Process != nil {
		if _, e = waiter.WaitProcess(ctx, api, dormant.Process.ID, opts); e != nil {
			t.Fatal(e)
		}
	}
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), Steps: []testingresource.TestStep{{Config: acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_service_restart\" \"test\" {\nservice_id=%q\ntriggers={}\n}\n", dormant.ID), ExpectError: regexp.MustCompile("serviceStackIsReadyToDeploy")}}})
}
