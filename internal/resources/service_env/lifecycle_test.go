package service_env

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

func TestAccServiceEnvPaginationAndOwnership(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	service := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	api := acctest.Client(t)
	last := ""
	for i := range 23 {
		p, err := api.CreateUserData(ctx, service, client.EnvWrite{Key: fmt.Sprintf("TFACC_PAGE_%02d", i), Content: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		last = p.ID
	}
	if _, err := waiter.WaitProcess(ctx, api, last); err != nil {
		t.Fatal(err)
	}
	config := acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_service_env\" \"test\" {\nservice_id=%q\nkey=\"TFACC_PAGE\"\ncontent=\"target\"\n}\n", service)
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), Steps: []testingresource.TestStep{{Config: config}, {ResourceName: "zerops_service_env.test", ImportState: true, ImportStateId: service + "/TFACC_PAGE", ImportStateVerify: true}, {ResourceName: "zerops_service_env.test", ImportState: true, ImportStateId: service + "/PATH", ExpectError: regexp.MustCompile("expected exactly one editable")}}})
}
func TestAccServiceEnvReadyAndStopped(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	project := acctest.CreateTestProject(t)
	api := acctest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	start := false
	s, err := api.CreateService(ctx, project.ID, acctest.MinimalServiceInput(client.ServiceCreate{Name: acctest.RandomHostname(), ServiceStackVersionName: "alpine/nodejs@22", StartWithoutCode: &start}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Process != nil {
		if _, err = waiter.WaitProcess(ctx, api, s.Process.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = waiter.WaitServiceSettled(ctx, api, s.ID, []string{"READY_TO_DEPLOY"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	stopped := fixture(t)
	p, err := api.StopService(ctx, stopped)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = waiter.WaitProcess(ctx, api, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = waiter.WaitServiceSettled(ctx, api, stopped, []string{"STOPPED"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	config := func(content string) string {
		return acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_service_env\" \"ready\" {\nservice_id=%q\nkey=\"TFACC_READY\"\ncontent=%q\n}\nresource \"zerops_service_env\" \"stopped\" {\nservice_id=%q\nkey=\"TFACC_STOPPED\"\ncontent=%q\n}\n", s.ID, content, stopped, content)
	}
	check := func(_ *terraform.State) error {
		for id, status := range map[string]string{s.ID: "READY_TO_DEPLOY", stopped: "STOPPED"} {
			got, err := api.GetService(ctx, id)
			if err != nil {
				return err
			}
			if got.Status != status {
				return fmt.Errorf("env mutation changed service lifecycle: %s => %s", status, got.Status)
			}
		}
		return nil
	}
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), Steps: []testingresource.TestStep{{Config: config("one"), Check: check}, {Config: config("two"), Check: check}}})
}
