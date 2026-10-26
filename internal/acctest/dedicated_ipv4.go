package acctest

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"net/http"
	"os"
	"strings"
	"testing"
)

// DedicatedIPv4Fixture is production-only and never creates or destroys its holder project.
func DedicatedIPv4Fixture(t *testing.T) client.Project {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC required")
	}
	if !IsProd() {
		t.Skip("dedicated IPv4 lifecycle is top-up-blocked on dev; forbidden allocation covered by unit tests")
	}
	id := os.Getenv("ZEROPS_ACC_IPV4_PROJECT_ID")
	if id == "" {
		t.Skip("ZEROPS_ACC_IPV4_PROJECT_ID required for adopt-only production IPv4 tests")
	}
	PreCheck(t)
	ledger, err := client.ReadLedger(os.Getenv("ZEROPS_PROD_LEDGER"))
	if err != nil {
		t.Fatal(err)
	}
	name, ok := ledger.Projects[id]
	if !ok || !(strings.HasPrefix(name, "tfacc-") || strings.HasPrefix(name, "tfprobe-")) {
		t.Fatal("IPv4 fixture must be a prefixed ledger project")
	}
	p, err := Client(t).GetProject(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.ClientID != os.Getenv("ZEROPS_ORGANIZATION_ID") || p.Name != name {
		t.Fatal("IPv4 fixture organization/name does not match pinned ledger ownership")
	}
	address, err := waiter.DedicatedIPv4Address(p)
	if err != nil {
		t.Fatal(err)
	}
	if address == "" {
		t.Fatal("IPv4 fixture must already have a dedicated address; allocation is forbidden")
	}
	return p
}

type noIPv4Acquisition struct{}

func (noIPv4Acquisition) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/request-ipv4") {
		return nil, fmt.Errorf("acceptance assertion: request-ipv4 is forbidden")
	}
	return http.DefaultTransport.RoundTrip(req)
}

// AdoptOnlyIPv4Factories retains normal provider production guarding while rejecting acquisition at the underlying transport.
func AdoptOnlyIPv4Factories(reg provider.Registry) map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){"zerops": func() (tfprotov6.ProviderServer, error) {
		if _, _, err := Environment(); err != nil {
			return nil, err
		}
		return providerserver.NewProtocol6WithError(provider.NewWithTransport("acctest", reg, noIPv4Acquisition{})())()
	}}
}
