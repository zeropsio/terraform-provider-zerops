package client

import (
	"context"
	"testing"
)

func TestCIOrganizationPin(t *testing.T) {
	t.Setenv("ZEROPS_CI_ORGANIZATION_ID", "ci-org")
	_, err := NewProdTransport(context.Background(), Config{Endpoint: ProductionEndpoint}, "other-org", "", "acctest")
	if err == nil {
		t.Fatal("accepted organization different from CI pin")
	}
	if guardPrefix("ci-run") || !guardPrefix("tfacc-local") || !guardPrefix("tfprobe-local") {
		t.Fatal("CI changed production prefix policy")
	}
}
