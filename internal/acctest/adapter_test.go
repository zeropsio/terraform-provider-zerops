package acctest

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestAccAdapterSmoke(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 for guarded dev acceptance checks")
	}
	PreCheck(t)
	c := Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	orgs, err := c.ListOrganizations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, org := range orgs {
		if org.ID == os.Getenv("ZEROPS_ORGANIZATION_ID") {
			found = true
		}
	}
	if !found {
		t.Fatal("configured organization not present in memberships")
	}
	if _, err = c.Settings(ctx, os.Getenv("ZEROPS_ORGANIZATION_ID")); err != nil {
		t.Fatal(err)
	}
}
