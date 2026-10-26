package sweep

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/zeropsio/terraform-provider-zerops/internal/client"
)

func TestMain(m *testing.M) { resource.TestMain(m) }

func TestSelectProject(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "ledger")
	if err := os.WriteFile(ledgerPath, []byte(
		"2026-10-21T00:00:00Z\tproject\towned\ttfacc-original\tacctest\n"+
			"2026-10-21T00:00:00Z\tproject\tholder\ttfacc-holder\tacctest\n"+
			"2026-10-21T00:00:00Z\tipv4\tholder\t192.0.2.1\tprobe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ledger, err := client.ReadLedger(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEROPS_ACC_IPV4_PROJECT_ID", "")
	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousOutput) })
	for _, tc := range []struct {
		name, id, projectName, org string
		prod, want                 bool
	}{
		{"dev-prefix", "unrecorded", "tfacc-test", "pinned", false, true},
		{"dev-probe", "unrecorded", "tfprobe-test", "", false, true},
		{"dev-unprefixed", "owned", "customer-project", "pinned", false, false},
		{"production-owned", "owned", "tfacc-test", "pinned", true, true},
		{"production-probe", "owned", "tfprobe-test", "pinned", true, true},
		{"production-ipv4-holder", "holder", "tfacc-holder", "pinned", true, false},
		{"production-unrecorded", "unrecorded", "tfacc-test", "pinned", true, false},
		{"production-foreign", "owned", "tfacc-test", "foreign", true, false},
		{"production-unknown-org", "owned", "tfacc-test", "", true, false},
		{"production-unprefixed", "owned", "customer-project", "pinned", true, false},
		{"production-empty-id", "", "tfacc-test", "pinned", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := client.Project{ID: tc.id, Name: tc.projectName, ClientID: tc.org}
			if got := selectProject(project, "pinned", tc.prod, ledger); got != tc.want {
				t.Fatalf("selected=%v, want %v", got, tc.want)
			}
		})
	}
	if !strings.Contains(logs.String(), "Skipping production dedicated-IPv4 holder project holder") {
		t.Fatalf("missing holder skip log: %s", logs.String())
	}
}
