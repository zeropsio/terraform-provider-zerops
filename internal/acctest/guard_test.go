package acctest

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/terraform-provider-zerops/internal/client"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGuardedTransport(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	t.Setenv("ZEROPS_TARGET", "")
	for _, endpoint := range []string{
		"https://api.app-prg1.zerops.io", "https://other.example.test", "http://api.dev.example.test",
		"https://dev.example.test.evil.test", "https://example.test", "https://api.other.example.test",
		"https://api.dev.example.test@evil.test",
	} {
		t.Run(endpoint, func(t *testing.T) {
			called := false
			g := client.DevTransport{Domain: "dev.example.test", Base: roundTripFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, nil })}
			req, err := http.NewRequest(http.MethodGet, endpoint, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := g.RoundTrip(req); err == nil {
				t.Fatal("unsafe request accepted")
			}
			if called {
				t.Fatal("unsafe request reached base transport")
			}
			if ValidateEndpoint(endpoint) == nil {
				t.Fatal("unsafe endpoint accepted")
			}
		})
	}
	t.Run("accepts dev including upload paths", func(t *testing.T) {
		called := false
		g := client.DevTransport{Domain: "dev.example.test", Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		req, _ := http.NewRequest(http.MethodPut, "https://api.dev.example.test/upload?signature=secret", nil)
		if _, err := g.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
		if !called {
			t.Fatal("safe request not forwarded")
		}
		if err := ValidateEndpoint("https://api.dev.example.test"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGuardRedirect(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	calls := 0
	c := http.Client{Transport: client.DevTransport{Domain: "dev.example.test", Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://api.app-prg1.zerops.io/secret"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	if _, err := c.Get("https://api.dev.example.test"); err == nil {
		t.Fatal("unsafe redirect accepted")
	}
	if calls != 1 {
		t.Fatalf("unsafe redirect reached transport: %d calls", calls)
	}
}

func TestEnvironmentRequiresExplicitConfiguration(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	t.Setenv("ZEROPS_TARGET", "dev")
	for _, missing := range []string{"ZEROPS_ENDPOINT", "ZEROPS_TOKEN", "ZEROPS_ORGANIZATION_ID", "ZEROPS_DEV_DOMAIN"} {
		t.Run(missing, func(t *testing.T) {
			t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
			t.Setenv("ZEROPS_ENDPOINT", "https://api.dev.example.test")
			t.Setenv("ZEROPS_TOKEN", "test")
			t.Setenv("ZEROPS_ORGANIZATION_ID", "test")
			t.Setenv(missing, "")
			if _, _, err := Environment(); err == nil {
				t.Fatalf("accepted missing %s", missing)
			}
		})
	}
}

func TestTargetEndpointSelection(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for _, target := range []string{"", "dev", "garbage", "prod"} {
		t.Run(target, func(t *testing.T) {
			t.Setenv("ZEROPS_TARGET", target)
			for _, endpoint := range []string{
				client.ProductionEndpoint,
				"https://api.dev.example.test",
				"https://api.app-prg1.zerops.io.evil.test",
				"https://x.api.app-prg1.zerops.io",
				"http://api.app-prg1.zerops.io",
				"https://user@api.app-prg1.zerops.io",
				"https://api.other.zerops.io",
				"https://api.app-prg1.zerops.io/",
			} {
				wantValid := target == "prod" && endpoint == client.ProductionEndpoint ||
					(target == "" || target == "dev") && endpoint == "https://api.dev.example.test"
				if err := ValidateEndpoint(endpoint); (err == nil) != wantValid {
					t.Errorf("endpoint %q: error=%v, want valid=%v", endpoint, err, wantValid)
				}
			}
		})
	}
}

func TestUnsetTargetRejectsProduction(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	t.Setenv("ZEROPS_TARGET", "")
	if err := os.Unsetenv("ZEROPS_TARGET"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEndpoint(client.ProductionEndpoint); err == nil {
		t.Fatal("unset target accepted production")
	}
}

func TestEnvironmentRejectsInvalidTargetBeforeRequests(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	t.Setenv("ZEROPS_TARGET", "garbage")
	t.Setenv("ZEROPS_ENDPOINT", client.ProductionEndpoint)
	t.Setenv("ZEROPS_TOKEN", "test")
	t.Setenv("ZEROPS_ORGANIZATION_ID", "test")
	if _, _, err := Environment(); err == nil {
		t.Fatal("garbage target accepted")
	}
}

func TestEnvironmentForcesDevelopmentSandbox(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	t.Setenv("ZEROPS_TARGET", "")
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	t.Setenv("ZEROPS_ENDPOINT", "https://api.dev.example.test")
	t.Setenv("ZEROPS_TOKEN", "test")
	t.Setenv("ZEROPS_ORGANIZATION_ID", "test")
	t.Setenv("ZEROPS_SANDBOX", "prod")
	cfg, org, err := Environment()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Transport.(client.DevTransport); !ok || org != "test" || os.Getenv("ZEROPS_SANDBOX") != "dev" {
		t.Fatal("dev environment did not retain its guard")
	}
}

func TestProviderConfigUsesExplicitRegistryHost(t *testing.T) {
	t.Setenv("ZEROPS_ENDPOINT", "https://api.dev.example.test")
	if !strings.Contains(ProviderConfig(), `source = "registry.terraform.io/zeropsio/zerops"`) {
		t.Fatal("acceptance config must use the factory registry host for both Terraform and OpenTofu")
	}
}
