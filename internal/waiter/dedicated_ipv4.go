package waiter

import (
	"context"
	"fmt"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"net/netip"
	"time"
)

type DedicatedIPv4Getter interface {
	GetProject(context.Context, string) (client.Project, error)
}

// DedicatedIPv4Address rejects malformed readbacks rather than treating them as absence.
func DedicatedIPv4Address(project client.Project) (string, error) {
	if project.PublicIPv4 == nil {
		return "", nil
	}
	address, err := netip.ParseAddr(*project.PublicIPv4)
	if err != nil || !address.Is4() {
		return "", fmt.Errorf("project %s returned an invalid dedicated IPv4 address", project.ID)
	}
	return address.String(), nil
}

func WaitDedicatedIPv4(ctx context.Context, api DedicatedIPv4Getter, id string, opts ...Option) (string, error) {
	p := newPoll(opts)
	for {
		if err := ctx.Err(); err != nil {
			return "", &Error{ResourceID: id, Cause: err}
		}
		project, err := api.GetProject(ctx, id)
		if fatal := p.readError(err); fatal != nil {
			return "", &Error{ResourceID: id, Cause: fatal}
		}
		if err == nil {
			address, err := DedicatedIPv4Address(project)
			if err != nil {
				return "", err
			}
			if address != "" {
				return address, nil
			}
		}
		if err := p.sleep(ctx, 5*time.Second); err != nil {
			return "", &Error{ResourceID: id, Cause: err}
		}
	}
}
