// Package acctest provides fail-closed helpers for explicitly selected acceptance targets.
package acctest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
)

// Target returns the explicitly selected harness target; dev is the default.
func Target() (client.Target, error) { return client.ParseTarget(os.Getenv("ZEROPS_TARGET")) }

// IsProd reports whether production was explicitly selected.
func IsProd() bool {
	target, err := Target()
	return err == nil && target == client.TargetProd
}

// ValidateEndpoint rejects endpoints outside the selected target's allowlist.
// Production is never accepted unless ZEROPS_TARGET is exactly prod.
func ValidateEndpoint(endpoint string) error {
	target, err := Target()
	if err != nil {
		return err
	}
	return client.ValidateTargetEndpoint(target, endpoint)
}

var productionEnvironment struct {
	once        sync.Once
	cfg         client.Config
	org, ledger string
	err         error
}

// Environment validates all credentials before any client or provider is created.
// Production organization resolution and transport initialization happen once.
func Environment() (client.Config, string, error) {
	target, err := Target()
	if err != nil {
		return client.Config{}, "", err
	}
	endpoint := os.Getenv("ZEROPS_ENDPOINT")
	if err := client.ValidateTargetEndpoint(target, endpoint); err != nil {
		return client.Config{}, "", err
	}
	token, org := os.Getenv("ZEROPS_TOKEN"), os.Getenv("ZEROPS_ORGANIZATION_ID")
	cfg := client.Config{Endpoint: endpoint, Token: token, UserAgent: "terraform-provider-zerops/acctest"}
	sandbox := "dev"
	if target == client.TargetProd {
		ledger := os.Getenv("ZEROPS_PROD_LEDGER")
		if strings.TrimSpace(token) == "" || strings.TrimSpace(org) == "" || !filepath.IsAbs(ledger) {
			return client.Config{}, "", fmt.Errorf("production requires ZEROPS_TOKEN, ZEROPS_ORGANIZATION_ID, and an absolute ZEROPS_PROD_LEDGER path")
		}
		productionEnvironment.once.Do(func() {
			guard, err := client.NewProdTransport(context.Background(), cfg, org, ledger, "acctest")
			productionEnvironment.err = err
			if err == nil {
				cfg.Transport = guard
				productionEnvironment.cfg, productionEnvironment.org, productionEnvironment.ledger = cfg, guard.OrgID, ledger
			}
		})
		if productionEnvironment.err != nil {
			return client.Config{}, "", productionEnvironment.err
		}
		if token != productionEnvironment.cfg.Token || endpoint != productionEnvironment.cfg.Endpoint || ledger != productionEnvironment.ledger || (org != "" && org != productionEnvironment.org) {
			return client.Config{}, "", fmt.Errorf("production acceptance configuration changed after organization pinning")
		}
		cfg, org, sandbox = productionEnvironment.cfg, productionEnvironment.org, "prod"
		if err := os.Setenv("ZEROPS_ORGANIZATION_ID", org); err != nil {
			return client.Config{}, "", err
		}
	} else {
		if strings.TrimSpace(token) == "" || strings.TrimSpace(org) == "" {
			return client.Config{}, "", fmt.Errorf("ZEROPS_TOKEN and ZEROPS_ORGANIZATION_ID must be explicitly set")
		}
		cfg.Transport = client.DevTransport{Domain: os.Getenv("ZEROPS_DEV_DOMAIN")}
	}
	if err := os.Setenv("ZEROPS_SANDBOX", sandbox); err != nil {
		return client.Config{}, "", err
	}
	return cfg, org, nil
}

// PreCheck must run before any acceptance request.
func PreCheck(t *testing.T) {
	t.Helper()
	if _, _, err := Environment(); err != nil {
		t.Fatal(err)
	}
}

// plugin-testing registers in-process providers under
// $TF_ACC_PROVIDER_HOST/$TF_ACC_PROVIDER_NAMESPACE/<name> (default hashicorp).
// Align it with the provider's registry source address used in ProviderConfig.
func init() {
	if os.Getenv("TF_ACC_PROVIDER_NAMESPACE") == "" {
		_ = os.Setenv("TF_ACC_PROVIDER_NAMESPACE", "zeropsio")
	}
	if os.Getenv("TF_ACC_PROVIDER_HOST") == "" {
		_ = os.Setenv("TF_ACC_PROVIDER_HOST", "registry.terraform.io")
	}
}

// ProtoV6ProviderFactories constructs guarded protocol 6 provider servers.
func ProtoV6ProviderFactories(reg provider.Registry) map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){"zerops": func() (tfprotov6.ProviderServer, error) {
		cfg, _, err := Environment()
		if err != nil {
			return nil, err
		}
		// Production is re-resolved by Configure rather than nesting pinned guards.
		transport := cfg.Transport
		if IsProd() {
			transport = nil
		}
		return providerserver.NewProtocol6WithError(provider.NewWithTransport("acctest", reg, transport)())()
	}}
}

// ProviderConfig pins the factory registry host so Terraform and OpenTofu use
// the same provider address, and renders an endpoint without embedding credentials.
func ProviderConfig() string {
	return fmt.Sprintf(`terraform {
  required_providers {
    zerops = { source = "registry.terraform.io/zeropsio/zerops" }
  }
}
provider "zerops" {
  endpoint = %s
}
`, strconv.Quote(os.Getenv("ZEROPS_ENDPOINT")))
}

// Client returns a guarded direct client for out-of-band assertions.
func Client(t *testing.T) *client.Client {
	t.Helper()
	PreCheck(t)
	cfg, _, _ := Environment()
	c, err := client.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// RandomName produces a collision-resistant project name with a sweepable prefix.
func RandomName(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("generate acceptance name: %w", err))
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

// RandomHostname satisfies the lowercase, letters-and-digits service-name rule.
func RandomHostname() string { return strings.ReplaceAll(RandomName("tfacc"), "-", "") }
