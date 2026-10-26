package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func providerConfig(t *testing.T, p framework.Provider, values map[string]any) tfsdk.Config {
	t.Helper()
	var response framework.SchemaResponse
	p.Schema(context.Background(), framework.SchemaRequest{}, &response)
	attrs := map[string]tftypes.Type{"endpoint": tftypes.String, "token": tftypes.String, "organization_id": tftypes.String, "routing_force_sync": tftypes.Bool, "postgresql_min_disk_gb": tftypes.Number}
	raw := make(map[string]tftypes.Value, len(attrs))
	for key, typ := range attrs {
		raw[key] = tftypes.NewValue(typ, values[key])
	}
	return tfsdk.Config{Schema: response.Schema, Raw: tftypes.NewValue(tftypes.Object{AttributeTypes: attrs}, raw)}
}

func cleanEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"ZEROPS_ENDPOINT", "ZEROPS_TOKEN", "ZEROPS_ORGANIZATION_ID", "ZEROPS_ROUTING_FORCE_SYNC", "ZEROPS_SANDBOX", "ZEROPS_PROD_LEDGER", "ZEROPS_DEV_DOMAIN"} {
		t.Setenv(key, "")
	}
	t.Setenv("ZEROPS_POSTGRESQL_MIN_DISK_GB", "")
	unsetEnvironment(t, "ZEROPS_POSTGRESQL_MIN_DISK_GB")
	// Most tests use explicit values, intentionally overriding even invalid environment.
}

func unsetEnvironment(t *testing.T, key string) {
	t.Helper()
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointValidation(t *testing.T) {
	for _, value := range []string{"", "http://localhost", "https://", "https://user:pass@example.com", "https://example.com/path", "https://example.com?x=y", "https://example.com#x", "https://example.com?", "https://example.com#"} {
		t.Run(value, func(t *testing.T) {
			if _, err := ValidateEndpoint(value); err == nil {
				t.Fatalf("accepted invalid endpoint %q", value)
			}
		})
	}
	for _, value := range []string{"https://api.example.com", "https://api.example.com/", "https://api.example.com:8443"} {
		actual, err := ValidateEndpoint(value)
		if err != nil || actual != strings.TrimSuffix(value, "/") {
			t.Fatalf("endpoint %q: %q %v", value, actual, err)
		}
	}
}

func TestConfigureExplicitPrecedence(t *testing.T) {
	cleanEnv(t)
	t.Setenv("ZEROPS_ENDPOINT", "invalid")
	t.Setenv("ZEROPS_TOKEN", "environment-token")
	t.Setenv("ZEROPS_ORGANIZATION_ID", "environment-org")
	t.Setenv("ZEROPS_ROUTING_FORCE_SYNC", "invalid")
	calls := 0
	p := NewWithTransport("test", Registry{}, roundTripperFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("unexpected request") }))()
	var response framework.ConfigureResponse
	p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, map[string]any{"endpoint": "https://api.example.com/", "token": "explicit-token", "organization_id": "explicit-org", "routing_force_sync": false})}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	data, ok := response.ResourceData.(*providerdata.ProviderData)
	if !ok || data != response.DataSourceData || data.OrganizationID != "explicit-org" || data.Endpoint != "https://api.example.com" || data.RoutingForceSync || data.Locks == nil || data.API == nil || calls != 0 {
		t.Fatalf("unexpected configured payload: %#v calls=%d", data, calls)
	}
}

func TestConfigureEnvironment(t *testing.T) {
	cleanEnv(t)
	t.Setenv("ZEROPS_ENDPOINT", "https://api.example.com")
	t.Setenv("ZEROPS_TOKEN", "token")
	t.Setenv("ZEROPS_ORGANIZATION_ID", "org")
	t.Setenv("ZEROPS_ROUTING_FORCE_SYNC", "true")
	p := New("test", Registry{})()
	var response framework.ConfigureResponse
	p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, nil)}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	data := response.ResourceData.(*providerdata.ProviderData)
	if data.OrganizationID != "org" || !data.RoutingForceSync || data.Endpoint != "https://api.example.com" {
		t.Fatalf("unexpected configuration: %#v", data)
	}
}

func TestConfigureDefaultEndpoint(t *testing.T) {
	cleanEnv(t)
	unsetEnvironment(t, "ZEROPS_ENDPOINT")
	unsetEnvironment(t, "ZEROPS_ROUTING_FORCE_SYNC")
	p := New("test", Registry{})()
	var response framework.ConfigureResponse
	p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, map[string]any{"token": "token", "organization_id": "org"})}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	if response.ResourceData.(*providerdata.ProviderData).Endpoint != DefaultEndpoint {
		t.Fatal("missing default endpoint")
	}
}

func TestConfigureInvalidEnvironmentBoolean(t *testing.T) {
	cleanEnv(t)
	t.Setenv("ZEROPS_ROUTING_FORCE_SYNC", "maybe")
	p := New("test", Registry{})()
	var response framework.ConfigureResponse
	p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, map[string]any{"endpoint": "https://api.example.com", "token": "token", "organization_id": "org"})}, &response)
	if !response.Diagnostics.HasError() || response.ResourceData != nil {
		t.Fatal("invalid boolean accepted")
	}
}

func TestConfigureInvalidAndUnknownNeverCallsAPI(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]any
	}{
		{"empty-token", map[string]any{"token": "", "organization_id": "org"}},
		{"empty-org", map[string]any{"token": "token", "organization_id": ""}},
		{"unknown-token", map[string]any{"token": tftypes.UnknownValue, "organization_id": "org"}},
		{"unknown-org", map[string]any{"token": "token", "organization_id": tftypes.UnknownValue}},
		{"invalid-endpoint", map[string]any{"endpoint": "http://localhost", "token": "token", "organization_id": "org"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			calls := 0
			p := NewWithTransport("test", Registry{}, roundTripperFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("unexpected request") }))()
			var response framework.ConfigureResponse
			p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, tc.values)}, &response)
			if !response.Diagnostics.HasError() || calls != 0 || response.ResourceData != nil {
				t.Fatalf("invalid config accepted: %v calls=%d", response.Diagnostics, calls)
			}
		})
	}
}

func TestConfigureOrganizationDiscovery(t *testing.T) {
	cleanEnv(t) // an exported ZEROPS_SANDBOX=dev would reject the httptest host
	for _, tc := range []struct {
		name, list string
		total      int
		wantError  bool
	}{
		{"single", `[{"id":"membership-id","clientId":"client-id","client":{"id":"client-id","accountName":"test"}}]`, 1, false},
		{"none", `[]`, 0, true},
		{"multiple", `[{"id":"m1","clientId":"c1","client":{"id":"c1"}},{"id":"m2","clientId":"c2","client":{"id":"c2"}}]`, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			p := NewWithTransport("test", Registry{}, roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != "/api/rest/public/user/client-list" || r.Header.Get("Authorization") != "Bearer token" {
					t.Errorf("unexpected request method/path/auth")
				}
				body := fmt.Sprintf(`{"count":%d,"total":%d,"limit":100,"offset":0,"list":%s}`, tc.total, tc.total, tc.list)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			}))()
			// Explicit values avoid depending on the caller's environment except that org must be absent.
			t.Setenv("ZEROPS_ORGANIZATION_ID", "unused")
			t.Setenv("ZEROPS_TOKEN", "token")
			// An absent HCL org falls back to absence, not an explicitly empty environment value.
			unsetEnvironment(t, "ZEROPS_ORGANIZATION_ID")
			var response framework.ConfigureResponse
			p.Configure(context.Background(), framework.ConfigureRequest{TerraformVersion: "test", Config: providerConfig(t, p, map[string]any{"endpoint": "https://api.example.com", "token": "token", "routing_force_sync": false})}, &response)
			if response.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("diagnostics: %v", response.Diagnostics)
			}
			if !tc.wantError && response.ResourceData.(*providerdata.ProviderData).OrganizationID != "client-id" {
				t.Fatal("membership id selected instead of clientId")
			}
			if calls == 0 {
				t.Fatal("discovery did not call API")
			}
		})
	}
}

func TestConfigureSandbox(t *testing.T) {
	for _, tc := range []struct {
		name, sandbox, endpoint              string
		wantConfigureError, wantRequestError bool
	}{
		{"dev", "dev", "https://api.dev.example.test", false, false},
		{"production", "dev", DefaultEndpoint, true, false},
		{"lookalike", "dev", "https://dev.example.test.evil.test", true, false},
		{"disabled", "", DefaultEndpoint, false, false},
		{"unknown", "production", "https://api.dev.example.test", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			t.Setenv("ZEROPS_SANDBOX", tc.sandbox)
			t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
			calls := 0
			p := NewWithTransport("test", Registry{}, roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
			}))()
			var response framework.ConfigureResponse
			p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, map[string]any{"endpoint": tc.endpoint, "token": "token", "organization_id": "org", "routing_force_sync": false})}, &response)
			if response.Diagnostics.HasError() != tc.wantConfigureError {
				t.Fatalf("configure diagnostics: %v", response.Diagnostics)
			}
			if tc.wantConfigureError {
				if calls != 0 || response.ResourceData != nil {
					t.Fatal("invalid sandbox configured a client")
				}
				return
			}
			api := response.ResourceData.(*providerdata.ProviderData).API
			// POST avoids GET retries while exercising the API transport.
			_, err := api.CreateProject(context.Background(), "org", client.ProjectCreate{Name: "test"})
			if (err != nil) != tc.wantRequestError {
				t.Fatalf("API error: %v", err)
			}
			err = api.UploadArchive(context.Background(), tc.endpoint+"/upload?signature=secret", strings.NewReader("archive"), 7)
			if (err != nil) != tc.wantRequestError {
				t.Fatalf("upload error: %v", err)
			}
			wantCalls := 2
			if tc.wantRequestError {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("base transport calls=%d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestConfigureSandboxProd(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, org, ledger string
		wantError                   bool
		wantCalls                   int
	}{
		{"matching", client.ProductionEndpoint, "org-test", "absolute", false, 1},
		{"missing-org", client.ProductionEndpoint, "", "absolute", true, 0},
		{"missing-membership", client.ProductionEndpoint, "org-absent", "absolute", true, 1},
		{"dev", "https://api.dev.example.test", "org-test", "absolute", true, 0},
		{"trailing-slash", client.ProductionEndpoint + "/", "org-test", "absolute", true, 0},
		{"missing-ledger", client.ProductionEndpoint, "org-test", "", true, 0},
		{"relative-ledger", client.ProductionEndpoint, "org-test", "relative", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			t.Setenv("ZEROPS_SANDBOX", "prod")
			ledger := tc.ledger
			if ledger == "absolute" {
				ledger = filepath.Join(t.TempDir(), "ledger")
			}
			t.Setenv("ZEROPS_PROD_LEDGER", ledger)
			t.Setenv("ZEROPS_ORGANIZATION_ID", tc.org)
			calls := 0
			p := NewWithTransport("test", Registry{}, roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Scheme+"://"+r.URL.Host != client.ProductionEndpoint || r.URL.Path != "/api/rest/public/user/client-list" {
					t.Fatalf("unexpected bootstrap request: %s %s", r.Method, r.URL.Path)
				}
				body := `{"count":2,"total":2,"limit":100,"offset":0,"list":[{"id":"membership","clientId":"` + "org-test" + `","client":{"id":"` + "org-test" + `","accountName":"Zerops"}},{"id":"m2","clientId":"foreign","client":{"id":"foreign","accountName":"ZEROPS"}}]}`
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			}))()
			values := map[string]any{"endpoint": tc.endpoint, "token": "token", "routing_force_sync": false}
			if tc.org != "" {
				values["organization_id"] = tc.org
			}
			var response framework.ConfigureResponse
			p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, values)}, &response)
			if response.Diagnostics.HasError() != tc.wantError || calls != tc.wantCalls {
				t.Fatalf("diagnostics=%v calls=%d, want error=%v calls=%d", response.Diagnostics, calls, tc.wantError, tc.wantCalls)
			}
			if tc.wantError {
				if response.ResourceData != nil {
					t.Fatal("invalid production configuration returned provider data")
				}
				return
			}
			data := response.ResourceData.(*providerdata.ProviderData)
			if data.OrganizationID != "org-test" {
				t.Fatalf("organization=%q", data.OrganizationID)
			}
			// The configured client must retain the guard after bootstrap.
			if _, err := data.API.CreateProject(context.Background(), "foreign", client.ProjectCreate{Name: "tfacc-test"}); err == nil || calls != 1 {
				t.Fatal("foreign organization mutation reached base transport")
			}
		})
	}
}
