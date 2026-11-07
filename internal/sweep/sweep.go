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
// Production requires ledger ownership and retains dedicated-IPv4 holders.
// CI further limits selection to tfacc- projects in its pinned organization.
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
				if acctest.IsProd() && os.Getenv("ZEROPS_CI_ORGANIZATION_ID") != "" {
					// Adopt only IDs inventoried in the pinned CI organization.
					// Deletion still passes the normal ledger and API ownership guard.
					if err := client.RecordProject(os.Getenv("ZEROPS_PROD_LEDGER"), project.ID, project.Name, "acctest"); err != nil {
						return err
					}
				}
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
	if acctest.IsProd() && os.Getenv("ZEROPS_CI_ORGANIZATION_ID") != "" {
		for offset := 0; ; {
			page, err := c.ListProjects(ctx, org, client.ProjectQuery{PageQuery: client.PageQuery{Offset: offset, Limit: 100}, NameContains: "tfacc-"})
			if err != nil {
				return err
			}
			for _, project := range page.Items {
				if project.ClientID == org && strings.HasPrefix(project.Name, "tfacc-") {
					return fmt.Errorf("CI project remains after sweep: %s", project.ID)
				}
			}
			if len(page.Items) == 0 || offset+len(page.Items) >= page.Total {
				break
			}
			offset += len(page.Items)
		}
	}
	return nil
}

// selectProject receives only rows from the pinned organization's listing.
// In production, require its explicit client ID as a second fail-closed check.
func selectProject(project client.Project, org string, prod bool, ledger client.Ledger) bool {
	if pin := os.Getenv("ZEROPS_CI_ORGANIZATION_ID"); pin != "" {
		if !prod || pin != org || !strings.HasPrefix(project.Name, "tfacc-") {
			return false
		}
		_, holder := ledger.IPv4Projects[project.ID]
		return !holder && project.ClientID == org && project.ID != ""
	}
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
