package service_env_vars

import (
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
)

func TestEnvMapPreservesExactKeysAndMasks(t *testing.T) {
	m, masked := envMap([]client.Env{{Key: "URL", Content: "postgres://secret"}, {Key: "url", Content: "REDACTED"}, {Key: "EMPTY", Content: ""}})
	if !masked || len(m) != 3 || m["url"] != "REDACTED" || m["URL"] != "postgres://secret" {
		t.Fatal("environment rows filtered or mask altered")
	}
}
