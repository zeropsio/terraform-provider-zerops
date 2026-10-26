package acctest

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// CreateDNSRecord creates an unproxied fixture using the restricted external tool.
// It registers independent cleanup immediately; provider code never invokes it.
func CreateDNSRecord(t *testing.T, kind, name, address string) string {
	t.Helper()
	domain := strings.ToLower(os.Getenv("ZEROPS_ACC_DNS_DOMAIN"))
	if domain == "" || !strings.HasPrefix(name, "tfacc-") || !strings.HasSuffix(name, "."+domain) {
		t.Fatal("DNS fixture must be tfacc-* inside ZEROPS_ACC_DNS_DOMAIN")
	}
	_, file, _, _ := runtime.Caller(0)
	helper := filepath.Join(filepath.Dir(file), "..", "..", "tools", "cloudflare-dns.sh")
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return exec.CommandContext(ctx, "bash", append([]string{helper}, args...)...).Output()
	}
	output, err := run("create", kind, name, address)
	if err != nil {
		t.Fatalf("creating restricted DNS fixture: %v", err)
	}
	var record struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(output, &record); err != nil || record.ID == "" {
		t.Fatal("DNS helper returned an invalid record ID")
	}
	t.Cleanup(func() {
		if _, err := run("delete", record.ID); err != nil {
			t.Errorf("deleting DNS fixture %s: %v", record.ID, err)
		}
	})
	return record.ID
}
