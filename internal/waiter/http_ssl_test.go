package waiter

import (
	"context"
	"errors"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"strings"
	"testing"
	"time"
)

type sslGetter struct {
	calls int
	get   func(int) (client.HTTPRoute, error)
}

func (g *sslGetter) GetHTTPRoute(context.Context, string) (client.HTTPRoute, error) {
	g.calls++
	return g.get(g.calls)
}
func TestWaitHTTPSSLAllDomains(t *testing.T) {
	clock := &fakeClock{limit: time.Minute}
	g := &sslGetter{get: func(n int) (client.HTTPRoute, error) {
		domains := []client.HTTPDomain{{DomainName: "a", SSLStatus: "ACTIVE"}}
		if n == 2 {
			domains = append(domains, client.HTTPDomain{DomainName: "b", DNSCheckStatus: "FAILED", SSLStatus: "ALIEN"})
		}
		if n >= 3 {
			domains = append(domains, client.HTTPDomain{DomainName: "b", DNSCheckStatus: "OK", SSLStatus: "ACTIVE"})
		}
		return client.HTTPRoute{ID: "route", Domains: domains}, nil
	}}
	if _, err := WaitHTTPSSL(context.Background(), g, "route", []string{"a", "b"}, WithClock(clock)); err != nil {
		t.Fatal(err)
	}
	if g.calls != 3 || clock.elapsed != 20*time.Second {
		t.Fatalf("unexpected polling: calls=%d elapsed=%s", g.calls, clock.elapsed)
	}
}
func TestWaitHTTPSSLTimeoutAndCancel(t *testing.T) {
	g := &sslGetter{get: func(int) (client.HTTPRoute, error) {
		return client.HTTPRoute{ID: "route", Domains: []client.HTTPDomain{{DomainName: "a", DNSCheckStatus: "FAILED", SSLStatus: "FUTURE"}}}, nil
	}}
	_, err := WaitHTTPSSL(context.Background(), g, "route", []string{"a", "missing"}, WithClock(&fakeClock{limit: 10 * time.Second}))
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "DNS=FAILED SSL=FUTURE") || !strings.Contains(err.Error(), "missing: missing") {
		t.Fatalf("missing timeout evidence: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = WaitHTTPSSL(ctx, g, "route", []string{"a"}, WithClock(&fakeClock{}))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSSLInstallationCodeRedaction(t *testing.T) {
	for raw, want := range map[string]string{
		"credential123": "unknown",
		`{"code":"acmeError","message":"credential123","meta":{"password":"credential123"}}`: "acmeError",
		`{"code":"contains secret"}`: "unknown",
	} {
		if got := sslInstallationCode(raw); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}
