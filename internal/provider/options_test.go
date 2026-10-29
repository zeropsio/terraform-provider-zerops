package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	framework "github.com/hashicorp/terraform-plugin-framework/provider"
)

func TestNewWithOptions(t *testing.T) {
	transport := &http.Transport{}
	p := NewWithOptions("0.1.1", Registry{}, Options{Transport: transport, Product: "pulumi-zerops/0.1.0"})().(*zeropsProvider)
	if p.transport != transport || p.product != "pulumi-zerops/0.1.0" || p.version != "0.1.1" {
		t.Fatal("constructor did not propagate options and version")
	}
}

func TestUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name, product, want string
	}{
		{"terraform", "", "terraform-provider-zerops/0.1.1 (terraform 1.15.9)"},
		{"embedding", "pulumi-zerops/0.1.0", "pulumi-zerops/0.1.0 terraform-provider-zerops/0.1.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &zeropsProvider{version: "0.1.1", product: tc.product}
			if got := p.userAgent("1.15.9"); got != tc.want {
				t.Fatalf("User-Agent = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfigureUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name, product, want string
	}{
		{"terraform", "", "terraform-provider-zerops/0.1.1 (terraform 1.15.9)"},
		{"embedding", "pulumi-zerops/0.1.0", "pulumi-zerops/0.1.0 terraform-provider-zerops/0.1.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			unsetEnvironment(t, "ZEROPS_ORGANIZATION_ID")
			headers := make(chan string, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				headers <- r.Header.Get("User-Agent")
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"count":1,"total":1,"limit":100,"offset":0,"list":[{"id":"membership-id","clientId":"client-id","client":{"id":"client-id"}}]}`)
			}))
			defer server.Close()
			p := NewWithOptions("0.1.1", Registry{}, Options{Transport: server.Client().Transport, Product: tc.product})()
			var response framework.ConfigureResponse
			p.Configure(context.Background(), framework.ConfigureRequest{TerraformVersion: "1.15.9", Config: providerConfig(t, p, map[string]any{"endpoint": server.URL, "token": "test-token", "routing_force_sync": false})}, &response)
			if response.Diagnostics.HasError() {
				t.Fatal(response.Diagnostics)
			}
			select {
			case got := <-headers:
				if got != tc.want {
					t.Fatalf("User-Agent = %q, want %q", got, tc.want)
				}
			default:
				t.Fatal("Configure did not send an organization discovery request")
			}
		})
	}
}
