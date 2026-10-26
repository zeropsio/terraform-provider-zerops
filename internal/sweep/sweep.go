// Package sweep registers opt-in, target-guarded acceptance fixture cleanup.
package sweep

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
)

func init() {
	resource.AddTestSweepers("zerops_project", &resource.Sweeper{Name: "zerops_project", F: SweepProjects})
}

// SweepProjects deletes prefixed projects in the configured organization.
// Production additionally requires that each project was recorded in the ledger.
// The region argument is the plugin-testing sweep label, not an API endpoint.
func SweepProjects(region string) error {
	cfg, org, err := acctest.Environment()
	if err != nil {
		return err
	}
	var ledger client.Ledger
	if acctest.IsProd() {
		ledger, err = client.ReadLedger(os.Getenv("ZEROPS_PROD_LEDGER"))
		if err != nil {
			return err
		}
	}
	c, err := client.New(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Minute)
	defer cancel()
	// Collect before deleting so pagination cannot skip rows when the list shrinks.
	var ids []string
	for offset := 0; ; {
		page, err := c.ListProjects(ctx, org, client.ProjectQuery{PageQuery: client.PageQuery{Offset: offset, Limit: 100}})
		if err != nil {
			return err
		}
		for _, project := range page.Items {
			if selectProject(project, org, acctest.IsProd(), ledger) {
				ids = append(ids, project.ID)
			}
		}
		if len(page.Items) == 0 || offset+len(page.Items) >= page.Total {
			break
		}
		offset += len(page.Items)
	}
	for _, id := range ids {
		if err := acctest.DeleteTestProject(ctx, c, id); err != nil {
			return fmt.Errorf("sweep project %s: %w", id, err)
		}
	}
	return nil
}

// selectProject receives only rows from the pinned organization's listing.
// In production, require its explicit client ID as a second fail-closed check.
func selectProject(project client.Project, org string, prod bool, ledger client.Ledger) bool {
	if !strings.HasPrefix(project.Name, "tfacc-") && !strings.HasPrefix(project.Name, "tfprobe-") {
		return false
	}
	if !prod {
		return true
	}
	if _, holder := ledger.IPv4Projects[project.ID]; holder {
		log.Printf("Skipping production dedicated-IPv4 holder project %s", project.ID)
		return false
	}
	_, owned := ledger.Projects[project.ID]
	return owned && project.ID != "" && project.ClientID == org
}
