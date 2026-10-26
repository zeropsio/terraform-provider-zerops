// Package routing implements project-wide staged routing ownership checks.
package routing

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

func EffectiveForce(value types.Bool, inherited bool) bool {
	if value.IsNull() {
		return inherited
	}
	return value.ValueBool()
}

func Gate(items []string, force bool, out *diag.Diagnostics) {
	if len(items) == 0 {
		return
	}
	sort.Strings(items)
	detail := "Project-wide synchronization also applies these foreign staged routes:\n" + strings.Join(items, "\n")
	if force {
		out.AddWarning("Applying foreign staged routing changes", detail)
	} else {
		out.AddError("Foreign staged routing changes", detail+"\nRefusing before staging or syncing. Set force_sync=true only if applying all these changes is intentional.")
	}
}
func action(deleting bool) string {
	if deleting {
		return "delete"
	}
	return "create/update"
}
func HTTPInventory(ctx context.Context, api client.API, project, own string) ([]string, error) {
	var result []string
	for offset := 0; ; {
		page, err := api.ListHTTPRoutes(ctx, project, client.PageQuery{Offset: offset, Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, v := range page.Items {
			if v.ID == own || (v.IsSynced && !v.DeleteOnSync) {
				continue
			}
			var domains, targets []string
			for _, d := range v.Domains {
				domains = append(domains, d.DomainName)
			}
			for _, l := range v.Locations {
				targets = append(targets, fmt.Sprintf("%s:%d%s", l.ServiceStackID, l.Port, l.Path))
			}
			result = append(result, fmt.Sprintf("id=%s domains=%v target_services=%v pending_action=%s", v.ID, domains, targets, action(v.DeleteOnSync)))
		}
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			return result, nil
		}
	}
}
func PortInventory(ctx context.Context, api client.API, project, own string) ([]string, error) {
	var result []string
	for offset := 0; ; {
		services, err := api.ListServices(ctx, project, client.ServiceQuery{PageQuery: client.PageQuery{Offset: offset, Limit: 100}})
		if err != nil {
			return nil, fmt.Errorf("listing project %s services for port routing: %w", project, err)
		}
		for _, s := range services.Items {
			for n := 0; ; {
				page, err := api.ListPortRoutes(ctx, s.ID, client.PageQuery{Offset: n, Limit: 100})
				if err != nil {
					return nil, fmt.Errorf("listing service %s port routing: %w", s.ID, err)
				}
				for _, v := range page.Items {
					if v.ID == own || (v.IsSynced && !v.DeleteOnSync) {
						continue
					}
					result = append(result, fmt.Sprintf("id=%s public_port=%d internal_port=%d protocol=%s target_service=%s pending_action=%s", v.ID, v.PublicPort, v.InternalPort, v.InternalProtocol, v.ServiceStackID, action(v.DeleteOnSync)))
				}
				n += len(page.Items)
				if len(page.Items) == 0 || n >= page.Total {
					break
				}
			}
		}
		offset += len(services.Items)
		if len(services.Items) == 0 || offset >= services.Total {
			return result, nil
		}
	}
}

// Sync accepts nothingToSync only as a request to verify, never as completion.
func Sync(ctx context.Context, api client.API, project string, http bool) error {
	var p client.Process
	var err error
	if http {
		p, err = api.SyncHTTPRoutes(ctx, project)
	} else {
		p, err = api.SyncPortRoutes(ctx, project)
	}
	if e, ok := client.AsAPIError(err); ok && e.Code == "nothingToSync" {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = waiter.WaitProcess(ctx, api, p.ID)
	return err
}
