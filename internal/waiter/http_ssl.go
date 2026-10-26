package waiter

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"strings"
	"time"
)

type HTTPRouteGetter interface {
	GetHTTPRoute(context.Context, string) (client.HTTPRoute, error)
}

// WaitHTTPSSL waits for every configured domain, not merely every returned domain.
// DNS failure and unknown status enums can be transient and are never terminal here.
func WaitHTTPSSL(ctx context.Context, api HTTPRouteGetter, id string, domains []string, opts ...Option) (client.HTTPRoute, error) {
	p := newPoll(opts)
	var last client.HTTPRoute
	summary := func() string {
		statuses := make([]string, 0, len(domains))
		for _, name := range domains {
			status := name + ": missing"
			for _, d := range last.Domains {
				if d.DomainName == name {
					status = fmt.Sprintf("%s: DNS=%s SSL=%s", name, d.DNSCheckStatus, d.SSLStatus)
					if d.SSLInstallationError != nil {
						status += " installation_error=" + sslInstallationCode(*d.SSLInstallationError)
					}
					break
				}
			}
			statuses = append(statuses, status)
		}
		return strings.Join(statuses, "; ")
	}
	for {
		v, err := api.GetHTTPRoute(ctx, id)
		if e := p.readError(err); e != nil {
			return last, fmt.Errorf("route %s SSL readiness (%s): %w", id, summary(), e)
		}
		if err == nil {
			last = v
			ready := len(domains) > 0
			for _, name := range domains {
				active := false
				for _, d := range v.Domains {
					if d.DomainName == name && d.SSLStatus == "ACTIVE" {
						active = true
						break
					}
				}
				ready = ready && active
			}
			if ready {
				return last, nil
			}
		}
		if err := p.sleep(ctx, 10*time.Second); err != nil {
			return last, fmt.Errorf("route %s SSL readiness (%s): %w", id, summary(), err)
		}
	}
}

// The API exposes installation errors as free text, not a typed error code.
// Only extract a structured code; never echo the arbitrary message or metadata.
func sslInstallationCode(raw string) string {
	var value struct {
		Code string `json:"code"`
	}
	if json.Unmarshal([]byte(raw), &value) != nil || value.Code == "" {
		return "unknown"
	}
	return sanitizeProcessCode(value.Code)
}
