package service_deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

const gitRepo = "https://github.com/zeropsio/recipe-nodejs"
const gitSHA = "4a156a716a3087ea4b4f0b00814b5ea1d3f21590"
const gitYAML = `zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - echo tfacc-build
      deployFiles:
        - package.json
    run:
      base: nodejs@22
      ports:
        - port: 3000
          protocol: tcp
          httpSupport: true
      start: node -e "require('http').createServer((q,s)=>s.end('tfacc')).listen(3000,'0.0.0.0')"
`

func accService(t *testing.T) client.Service {
	t.Helper()
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	c := acctest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	start := true
	s, e := c.CreateService(ctx, p.ID, acctest.MinimalServiceInput(client.ServiceCreate{Name: acctest.RandomHostname(), ServiceStackVersionName: "alpine/nodejs@22", StartWithoutCode: &start}))
	if e != nil {
		t.Fatal(e)
	}
	if s.Process != nil {
		if _, e = waiter.WaitProcess(ctx, c, s.Process.ID); e != nil {
			t.Fatal(e)
		}
	}
	baseline, e := waiter.SnapshotProcessIDs(ctx, c, s.ID)
	if e != nil {
		t.Fatal(e)
	}
	v, e := waiter.WaitServiceSettled(ctx, c, s.ID, []string{"ACTIVE", "READY_TO_DEPLOY"}, baseline, nil)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func accConfig(id, source, yaml string, build bool) string {
	return acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_service_deploy\" \"app\" {\n service_id = %q\n %s\n setup = \"app\"\n build = %t\n zerops_yaml = %q\n}\n", id, source, build, yaml)
}
func accFactories() provider.Registry {
	return provider.Registry{Resources: []func() resource.Resource{NewResource}}
}
func TestAccServiceDeployDirectory(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	s := accService(t)
	dir := t.TempDir()
	for _, n := range []string{"index.js", "zerops.yml"} {
		b, e := os.ReadFile(filepath.Join("testdata/app", n))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, n), b, 0644); e != nil {
			t.Fatal(e)
		}
	}
	yaml, e := os.ReadFile("testdata/app/zerops.yml")
	if e != nil {
		t.Fatal(e)
	}
	config := accConfig(s.ID, fmt.Sprintf("source_dir = %q", dir), string(yaml), true)
	var first string
	tfresource.Test(t, tfresource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(accFactories()), Steps: []tfresource.TestStep{
		{Config: config, Check: tfresource.ComposeTestCheckFunc(tfresource.TestCheckResourceAttr("zerops_service_deploy.app", "active", "true"), func(st *terraform.State) error {
			first = st.RootModule().Resources["zerops_service_deploy.app"].Primary.ID
			return nil
		})},
		{Config: config, PlanOnly: true},
		{PreConfig: func() {
			if e := os.WriteFile(filepath.Join(dir, "index.js"), []byte("require('http').createServer((q,s)=>s.end('changed')).listen(3000,'0.0.0.0');"), 0644); e != nil {
				t.Fatal(e)
			}
		}, Config: config, Check: func(st *terraform.State) error {
			if st.RootModule().Resources["zerops_service_deploy.app"].Primary.ID == first {
				return fmt.Errorf("source mutation did not replace app version")
			}
			return nil
		}},
		{Config: config, PlanOnly: true},
	}})
}
func TestAccServiceDeployArchive(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	s := accService(t)
	p := filepath.Join(t.TempDir(), "app.tar.gz")
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = writeDirectory(f, "testdata/app"); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	yaml, e := os.ReadFile("testdata/app/zerops.yml")
	if e != nil {
		t.Fatal(e)
	}
	tfresource.Test(t, tfresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(accFactories()), Steps: []tfresource.TestStep{{Config: accConfig(s.ID, fmt.Sprintf("archive_path = %q", p), string(yaml), false), Check: tfresource.TestCheckResourceAttr("zerops_service_deploy.app", "active", "true")}}})
}
func TestAccServiceDeployGit(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	for _, tc := range []struct{ name, url, ref, mode string }{{"providerSHA", gitRepo, gitSHA, "provider"}, {"providerTag", "https://github.com/expressjs/express", "v5.1.0", "provider"}, {"serverBranch", gitRepo, "main", "server"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := accService(t)
			source := fmt.Sprintf("git = {url = %q, ref = %q, mode = %q}", tc.url, tc.ref, tc.mode)
			config := accConfig(s.ID, source, gitYAML, true)
			tfresource.Test(t, tfresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(accFactories()), Steps: []tfresource.TestStep{{Config: config, Check: tfresource.TestCheckResourceAttr("zerops_service_deploy.app", "active", "true")}, {Config: config, PlanOnly: true}}})
		})
	}
}
func TestAccServiceDeployServerTagRejected(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	acctest.PreCheck(t)
	tfresource.Test(t, tfresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(accFactories()), Steps: []tfresource.TestStep{{Config: accConfig("not-used", `git = {url = "https://github.com/expressjs/express", ref = "v5.1.0", mode = "server"}`, gitYAML, true), ExpectError: regexp.MustCompile("server Git mode requires an existing branch")}}})
}
func TestAccServiceDeployBuildFailure(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	s := accService(t)
	yaml := `zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - exit 42
      deployFiles:
        - index.js
    run:
      base: nodejs@22
      start: node index.js
`
	dir, err := filepath.Abs("testdata/app")
	if err != nil {
		t.Fatal(err)
	}
	tfresource.Test(t, tfresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(accFactories()), Steps: []tfresource.TestStep{{Config: accConfig(s.ID, fmt.Sprintf("source_dir = %q", dir), yaml, true), ExpectError: regexp.MustCompile("Deployment failed")}}})
}
