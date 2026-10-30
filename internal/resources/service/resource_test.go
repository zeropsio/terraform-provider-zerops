package service_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	ds "github.com/zeropsio/terraform-provider-zerops/internal/datasources/service"
	envds "github.com/zeropsio/terraform-provider-zerops/internal/datasources/service_env_vars"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	svc "github.com/zeropsio/terraform-provider-zerops/internal/resources/service"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

func accProject(t *testing.T) string {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	acctest.PreCheck(t)
	return acctest.CreateTestProject(t).ID
}
func factories() map[string]func() (tfprotov6.ProviderServer, error) {
	return acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() fwresource.Resource{svc.NewResource}, DataSources: []func() datasource.DataSource{ds.NewDataSource, envds.NewDataSource}})
}
func config(project, name, typ string, start bool, extra string) string {
	if acctest.IsProd() {
		minimum := acctest.MinimalServiceSize(typ)
		// Managed cases supply catalogue minimums explicitly on both targets.
		// Runtime steps without scaling retain their production-safe defaults.
		if !minimum.Managed && !strings.Contains(extra, "vertical_autoscaling") {
			memory := minimum.RAMGB
			disk := ", min_disk_gb = 1, max_disk_gb = 1"
			extra += fmt.Sprintf(`
vertical_autoscaling = { cpu_mode = "SHARED", min_cpu = 1, max_cpu = 1, min_ram_gb = %g, max_ram_gb = %g%s, start_cpu_core_count = 1 }
`, memory, memory, disk)
		}
		if !minimum.Managed && !strings.Contains(extra, "horizontal_autoscaling") {
			extra += "\nhorizontal_autoscaling = { min_containers = 1, max_containers = 1 }\n"
		}
	}
	return acctest.ProviderConfig() + fmt.Sprintf(`
resource "zerops_service" "test" {
 project_id = %q
 hostname = %q
 type = %q
 start_without_code = %t
 %s
}
data "zerops_service" "byid" { id = zerops_service.test.id }
data "zerops_service" "byname" {
 project_id = zerops_service.test.project_id
 hostname = zerops_service.test.hostname
}
data "zerops_service_env_vars" "all" { service_id = zerops_service.test.id }
`, project, name, typ, start, extra)
}
func replacementStep(cfg string) resource.TestStep {
	return resource.TestStep{Config: cfg, PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("zerops_service.test", plancheck.ResourceActionReplace)}}}
}
func TestAccServiceRuntime(t *testing.T) {
	prod := acctest.IsProd()
	project := accProject(t)
	name := acctest.RandomHostname()
	cfg := func(extra string) string { return config(project, name, "nodejs@22", false, extra) }
	createScaling := `vertical_autoscaling = { max_ram_gb = 2 }
horizontal_autoscaling = { max_containers = 2 }`
	swapScaling := `vertical_autoscaling = { swap_enabled = false }`
	swapCheck := resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("zerops_service.test", "vertical_autoscaling.swap_enabled", "false"), resource.TestCheckNoResourceAttr("zerops_service.test", "horizontal_autoscaling"))
	if prod {
		createScaling = ""
		memory := acctest.MinimalServiceMemoryGB("nodejs@22")
		swapScaling = fmt.Sprintf(`vertical_autoscaling = { cpu_mode = "SHARED", min_cpu = 1, max_cpu = 1, min_ram_gb = %g, max_ram_gb = %g, min_disk_gb = 1, max_disk_gb = 1, start_cpu_core_count = 1, swap_enabled = false }`, memory, memory)
		swapCheck = resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("zerops_service.test", "vertical_autoscaling.swap_enabled", "false"), resource.TestCheckResourceAttr("zerops_service.test", "horizontal_autoscaling.max_containers", "1"))
	}
	steps := []resource.TestStep{
		{Config: cfg(createScaling), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("zerops_service.test", "status", "READY_TO_DEPLOY"), resource.TestCheckResourceAttr("zerops_service.test", "type", "nodejs@22"), resource.TestCheckResourceAttr("zerops_service.test", "base", "ubuntu/nodejs@22"), resource.TestCheckResourceAttrPair("data.zerops_service.byid", "id", "zerops_service.test", "id"), resource.TestCheckResourceAttrPair("data.zerops_service.byname", "id", "zerops_service.test", "id"))},
		{Config: cfg(swapScaling), Check: swapCheck},
	}
	if prod {
		// Clearing the block restores server defaults rather than retaining minimum bounds.
		t.Log("production gentleness: removing autoscaling restores server defaults that may exceed minimum bounds; step omitted")
	} else {
		steps = append(steps, resource.TestStep{Config: cfg(``), Check: resource.TestCheckNoResourceAttr("zerops_service.test", "vertical_autoscaling")})
	}
	steps = append(steps,
		resource.TestStep{ResourceName: "zerops_service.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"type"}},
		resource.TestStep{ResourceName: "zerops_service.test", ImportState: true, ImportStateId: project + "/" + name, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"type"}},
		// The provider changes this create-only hint in state without starting the service.
		resource.TestStep{Config: config(project, name, "nodejs@22", true, ""), Check: resource.TestCheckResourceAttr("zerops_service.test", "status", "READY_TO_DEPLOY")},
	)
	if prod {
		t.Log("production gentleness: DEDICATED CPU exceeds SHARED minimum; step omitted")
	} else {
		steps = append(steps, resource.TestStep{Config: cfg(`vertical_autoscaling = { cpu_mode = "DEDICATED" }`), Check: resource.TestCheckResourceAttr("zerops_service.test", "vertical_autoscaling.cpu_mode", "DEDICATED")})
	}
	steps = append(steps,
		replacementStep(config(project, name+"x", "nodejs@22", false, "")),
		replacementStep(config(project, name, "nodejs@24", false, "")),
		replacementStep(config(project, name, "nodejs@22", false, `env_isolation = "none"`)),
		replacementStep(config(project, name, "nodejs@22", false, `ssh_isolation = "vpn"`)),
		replacementStep(config("different-project-id", name, "nodejs@22", false, "")),
	)
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: factories(), Steps: steps})
}
func TestAccServiceLifecycle(t *testing.T) {
	prod := acctest.IsProd()
	project := accProject(t)
	name := acctest.RandomHostname()
	var serviceID string
	cfg := func(extra string) string { return config(project, name, "alpine/nodejs@22", true, extra) }
	stoppedUpdate := `desired_state = "STOPPED"
 vertical_autoscaling = { max_ram_gb = 2 }`
	running := `desired_state = "RUNNING"
 vertical_autoscaling = { min_cpu = 1, max_cpu = 2, min_ram_gb = 0.25, max_ram_gb = 2, min_disk_gb = 1, max_disk_gb = 2, start_cpu_core_count = 1, min_free_cpu_cores = 0.1, min_free_cpu_percent = 10, min_free_ram_gb = 0.125, min_free_ram_percent = 10, swap_enabled = false }
 horizontal_autoscaling = { min_containers = 1, max_containers = 2 }`
	stoppedCheck := resource.TestCheckResourceAttr("zerops_service.test", "status", "STOPPED")
	if prod {
		t.Log("production gentleness: autoscaling range expansion exceeds minimum RAM/disk/CPU/container bounds; expansion omitted")
		memory := acctest.MinimalServiceMemoryGB("alpine/nodejs@22")
		minimumSwapScaling := fmt.Sprintf(`vertical_autoscaling = { cpu_mode = "SHARED", min_cpu = 1, max_cpu = 1, min_ram_gb = %g, max_ram_gb = %g, min_disk_gb = 1, max_disk_gb = 1, start_cpu_core_count = 1, swap_enabled = false }`, memory, memory)
		stoppedUpdate = "desired_state = \"STOPPED\"\n" + minimumSwapScaling
		running = "desired_state = \"RUNNING\"\n" + minimumSwapScaling
		stoppedCheck = resource.ComposeTestCheckFunc(stoppedCheck, resource.TestCheckResourceAttr("zerops_service.test", "vertical_autoscaling.swap_enabled", "false"))
	}
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: factories(), Steps: []resource.TestStep{
		{Config: cfg(`desired_state = "STOPPED"`), Check: resource.TestCheckResourceAttr("zerops_service.test", "status", "STOPPED")},
		{Config: cfg(stoppedUpdate), Check: stoppedCheck},
		{Config: cfg(running), Check: resource.TestCheckResourceAttr("zerops_service.test", "status", "ACTIVE")},
		{Config: cfg(`desired_state = "RUNNING"`), Check: func(s *terraform.State) error {
			serviceID = s.RootModule().Resources["zerops_service.test"].Primary.ID
			return nil
		}},
		{Config: cfg(`desired_state = "RUNNING"`), PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			c := acctest.Client(t)
			baseline, e := waiter.SnapshotProcessIDs(ctx, c, serviceID)
			if e != nil {
				t.Fatal(e)
			}
			p, e := c.StopService(ctx, serviceID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = waiter.WaitProcess(ctx, c, p.ID); e != nil {
				t.Fatal(e)
			}
			if _, e = waiter.WaitServiceSettled(ctx, c, serviceID, []string{"STOPPED"}, baseline, []string{p.ID}); e != nil {
				t.Fatal(e)
			}
		}, Check: resource.TestCheckResourceAttr("zerops_service.test", "desired_state", "RUNNING")},
	}})
}
func TestAccServiceManaged(t *testing.T) {
	project := accProject(t)
	cases := []struct {
		family          string
		typ             string
		minimum         acctest.ServiceMinimum
		expectedEnvKeys []string
		stopStart       bool
		skipOnProd      string
		skip            string
	}{
		{family: "postgresql", typ: "postgresql:single@16", expectedEnvKeys: []string{"connectionString", "connectionTlsString", "dbName", "hostname", "password", "port", "portTls", "superUser", "superUserPassword", "user"}, stopStart: true},
		{family: "mariadb", typ: "mariadb:single@10.6", expectedEnvKeys: []string{"connectionString", "dbName", "hostname", "password", "port", "user"}, stopStart: true},
		{family: "valkey", typ: "valkey:single@7.2", expectedEnvKeys: []string{"connectionString", "connectionTlsString", "hostname", "password", "port", "portTls"}, stopStart: true},
		{family: "clickhouse", typ: "clickhouse:single@25.3", expectedEnvKeys: []string{"clusterName", "connectionString", "dbName", "hostname", "password", "port", "portHttp", "portMysql", "portNative", "portPostgresql", "superUser", "superUserPassword", "user"}, stopStart: true},
		{family: "elasticsearch", typ: "elasticsearch:single@9.2", expectedEnvKeys: []string{"connectionString", "hostname", "password", "port", "user"}, stopStart: true, skipOnProd: "heaviest family: 2 GB RAM provisioning floor"},
		{family: "kafka", typ: "kafka:single@3.9", expectedEnvKeys: []string{"KAFKA_HEAP_OPTS", "hostname", "password", "port", "user"}, stopStart: true},
		{family: "nats", typ: "nats:single@2.12", expectedEnvKeys: []string{"JET_STREAM_ENABLED", "MAX_PAYLOAD", "connectionString", "hostname", "password", "port", "portManagement", "user"}, stopStart: true},
		{family: "qdrant", typ: "qdrant:single@1.12", expectedEnvKeys: []string{"apiKey", "automaticClusterReplication", "connectionString", "grpcConnectionString", "grpcPort", "hostname", "logLevel", "port", "readOnlyApiKey"}, stopStart: true},
		{family: "seaweedfs", typ: "seaweedfs:single@3.85", expectedEnvKeys: []string{"hostname", "port"}, stopStart: true},
		{family: "typesense", typ: "typesense:single@30.2", expectedEnvKeys: []string{"apiKey", "connectionString", "hostname", "port"}, stopStart: true},
		{family: "meilisearch", typ: "meilisearch:single@1.44", expectedEnvKeys: []string{"connectionString", "defaultAdminKey", "defaultChatKey", "defaultReadOnlyKey", "defaultSearchKey", "environment", "hostname", "masterKey", "port"}, stopStart: true},
		{family: "mysql", typ: "mysql:ha@5.7", stopStart: true, skip: "only version mysql:ha@5.7 is DISABLED in the catalogue", skipOnProd: "HA-only family; production runs are NON_HA only"},
		{family: "local-storage", typ: "local-storage:single@1", expectedEnvKeys: []string{"hostname"}, stopStart: true},
		{family: "zbox", typ: "ubuntu/zbox@1", stopStart: true, skipOnProd: "Box Workspace VM, heavy: 2 CPU / 2 GB / 10 GB minimum"},
	}
	for _, tc := range cases {
		// Keep per-family production exclusions in skipOnProd above.
		tc.minimum = acctest.MinimalServiceSize(tc.typ)
		if tc.family == "postgresql" {
			tc.minimum.DiskGB = 0 // Exercise the provider's PostgreSQL 2 GB floor.
		}
		t.Run(tc.family, func(t *testing.T) {
			if tc.skip != "" {
				t.Skip(tc.skip)
			}
			if acctest.IsProd() && tc.skipOnProd != "" {
				t.Skip(tc.skipOnProd)
			}
			if !acctest.IsProd() && tc.family == "zbox" {
				t.Skip("not in the dev catalogue")
			}
			disk := ""
			if tc.minimum.DiskGB > 0 {
				disk = fmt.Sprintf(", min_disk_gb = %g, max_disk_gb = %g", tc.minimum.DiskGB, tc.minimum.DiskGB)
			}
			scaling := fmt.Sprintf(`vertical_autoscaling = { cpu_mode = "SHARED", min_cpu = %d, max_cpu = %d, min_ram_gb = %g, max_ram_gb = %g%s, start_cpu_core_count = %d }`, tc.minimum.CPU, tc.minimum.CPU, tc.minimum.RAMGB, tc.minimum.RAMGB, disk, tc.minimum.CPU)
			if tc.minimum.Containers > 0 {
				scaling += fmt.Sprintf("\nhorizontal_autoscaling = { min_containers = %d, max_containers = %d }\n", tc.minimum.Containers, tc.minimum.Containers)
			}
			name := acctest.RandomHostname()
			cfg := config(project, name, tc.typ, false, scaling)
			checks := []resource.TestCheckFunc{
				resource.TestCheckResourceAttr("zerops_service.test", "status", "ACTIVE"),
				resource.TestCheckResourceAttr("zerops_service.test", "base", tc.typ),
				func(st *terraform.State) error {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()
					id := st.RootModule().Resources["zerops_service.test"].Primary.ID
					got, err := acctest.Client(t).GetService(ctx, id)
					if err != nil {
						return err
					}
					// Canonical :single bases prove NON_HA; mode is not a Terraform attribute.
					if got.Base != tc.typ {
						return fmt.Errorf("managed service base %q; want %q", got.Base, tc.typ)
					}
					return nil
				},
				func(st *terraform.State) error {
					env, ok := st.RootModule().Resources["data.zerops_service_env_vars.all"]
					if !ok || env.Primary == nil {
						return fmt.Errorf("managed environment data source missing")
					}
					keys := make([]string, 0)
					for key := range env.Primary.Attributes {
						if strings.HasPrefix(key, "vars.") && key != "vars.%" {
							keys = append(keys, strings.TrimPrefix(key, "vars."))
						}
					}
					sort.Strings(keys)
					t.Logf("managed-env-keys %s: %s", tc.family, strings.Join(keys, ","))
					return nil
				},
			}
			for _, key := range tc.expectedEnvKeys {
				checks = append(checks, resource.TestCheckResourceAttrSet("data.zerops_service_env_vars.all", "vars."+key))
			}
			steps := []resource.TestStep{
				{Config: cfg, Check: resource.ComposeTestCheckFunc(checks...)},
				{Config: cfg, PlanOnly: true},
				{ResourceName: "zerops_service.test", ImportState: true, ImportStateVerify: true},
			}
			if tc.stopStart {
				steps = append(steps,
					resource.TestStep{Config: config(project, name, tc.typ, false, scaling+"\ndesired_state = \"STOPPED\"\n"), Check: resource.TestCheckResourceAttr("zerops_service.test", "status", "STOPPED")},
					resource.TestStep{Config: config(project, name, tc.typ, false, scaling+"\ndesired_state = \"RUNNING\"\n"), Check: resource.TestCheckResourceAttr("zerops_service.test", "status", "ACTIVE")},
				)
			}
			resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: factories(), Steps: steps})
		})
	}
}
func TestAccServiceReadyLifecycleError(t *testing.T) {
	project := accProject(t)
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: factories(), Steps: []resource.TestStep{{Config: config(project, acctest.RandomHostname(), "nodejs@22", false, `desired_state = "RUNNING"`), ExpectError: regexp.MustCompile("desired_state cannot be managed for READY_TO_DEPLOY")}}})
}
func TestAccServiceDisappears(t *testing.T) {
	project := accProject(t)
	c := acctest.Client(t)
	cfg := acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_service\" \"test\" {\nproject_id=%q\nhostname=%q\ntype=\"nodejs@22\"\n}\n", project, acctest.RandomHostname())
	if acctest.IsProd() {
		cfg = acctest.ProviderConfig() + fmt.Sprintf(`resource "zerops_service" "test" {
 project_id = %q
 hostname = %q
 type = "nodejs@22"
 vertical_autoscaling = { cpu_mode = "SHARED", min_cpu = 1, max_cpu = 1, min_ram_gb = 0.125, max_ram_gb = 0.125, min_disk_gb = 1, max_disk_gb = 1, start_cpu_core_count = 1 }
 horizontal_autoscaling = { min_containers = 1, max_containers = 1 }
}
`, project, acctest.RandomHostname())
	}
	resource.Test(t, resource.TestCase{PreCheck: func() { acctest.PreCheck(t) }, ProtoV6ProviderFactories: factories(), Steps: []resource.TestStep{{Config: cfg, Check: func(s *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		id := s.RootModule().Resources["zerops_service.test"].Primary.ID
		p, e := c.DeleteService(ctx, id)
		if e != nil {
			return e
		}
		if _, e = waiter.WaitProcess(ctx, c, p.ID); e != nil {
			return e
		}
		return waiter.WaitNotFound(ctx, id, func(ctx context.Context) error { _, e := c.GetService(ctx, id); return e }, []string{"serviceStackNotFound"})
	}, ExpectNonEmptyPlan: true}}})
}
