package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/terraform-provider-zerops/internal/client"
)

type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"X-Test": []string{"safe"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func capture(t *testing.T, args ...string) (string, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	old := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = old }()
	err = run(args)
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		t.Fatal(e)
	}
	data, e := io.ReadAll(f)
	if e != nil {
		t.Fatal(e)
	}
	return string(data), err
}
func TestRequestRecordsThroughTransport(t *testing.T) {
	t.Setenv("ZEROPS_TARGET", "prod")
	t.Setenv("ZEROPS_ENDPOINT", client.ProductionEndpoint)
	t.Setenv("ZEROPS_TOKEN", "test-secret")
	t.Setenv("ZEROPS_ORGANIZATION_ID", "org-test")
	ledger := filepath.Join(t.TempDir(), "ledger")
	t.Setenv("ZEROPS_PROD_LEDGER", ledger)
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	mutations := 0
	http.DefaultTransport = fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("missing authorization")
		}
		switch r.URL.Path {
		case "/api/rest/public/user/client-list":
			return response(`{"list":[{"clientId":"` + "org-test" + `","client":{"id":"` + "org-test" + `","accountName":"Zerops"}}],"total":1}`), nil
		case "/api/rest/public/client/" + "org-test" + "/project":
			if r.Method != "POST" {
				t.Fatal("wrong method")
			}
			mutations++
			return response(`{"id":"project","name":"tfprobe-cli"}`), nil
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
			return nil, nil
		}
	})
	body := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(body, []byte(`{"name":"tfprobe-cli"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := capture(t, "request", "POST", "/client/"+"org-test"+"/project", body)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Status        int
		Body, Headers string
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != 200 || !strings.Contains(envelope.Body, `"id":"project"`) || !strings.Contains(envelope.Headers, "X-Test: safe") {
		t.Fatalf("bad envelope %s", out)
	}
	recorded, err := client.ReadLedger(ledger)
	if err != nil || recorded.Projects["project"] != "tfprobe-cli" || mutations != 1 {
		t.Fatalf("record=%v mutations=%d err=%v", recorded, mutations, err)
	}
}
func TestUploadUsesStdinWithoutAuthorization(t *testing.T) {
	t.Setenv("ZEROPS_TARGET", "prod")
	t.Setenv("ZEROPS_TOKEN", "test-secret")
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive")
	if err := os.WriteFile(archive, []byte("archive"), 0600); err != nil {
		t.Fatal(err)
	}
	stdin, err := os.CreateTemp(dir, "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if _, err := stdin.WriteString("https://proxy.app-prg1.zerops.io/archive?signature=private-signature"); err != nil {
		t.Fatal(err)
	}
	stdin.Seek(0, io.SeekStart)
	oldIn := os.Stdin
	os.Stdin = stdin
	defer func() { os.Stdin = oldIn }()
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "PUT" || r.Header.Get("Authorization") != "" {
			t.Fatal("unsafe upload")
		}
		return response(""), nil
	})
	out, err := capture(t, "upload", archive)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "signature") || strings.Contains(out, "test-secret") || !strings.Contains(out, `"host":"proxy.app-prg1.zerops.io"`) {
		t.Fatalf("unsafe upload output %s", out)
	}
}

func TestRecordIPv4Release(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
		allowed    bool
	}{
		{"released", `{"clientId":"` + "org-test" + `","publicIpV4":null}`, 200, true},
		{"deleted", `{"error":{"code":"projectNotFound"}}`, 400, true},
		{"still held", `{"clientId":"` + "org-test" + `","publicIpV4":"192.0.2.1"}`, 200, false},
		{"unavailable", `{"error":{"message":"test-secret"}}`, 500, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZEROPS_TARGET", "prod")
			t.Setenv("ZEROPS_ENDPOINT", client.ProductionEndpoint)
			t.Setenv("ZEROPS_TOKEN", "test-secret")
			t.Setenv("ZEROPS_ORGANIZATION_ID", "org-test")
			ledger := filepath.Join(t.TempDir(), "ledger")
			t.Setenv("ZEROPS_PROD_LEDGER", ledger)
			if err := client.RecordIPv4(ledger, "p", "192.0.2.1", "probe"); err != nil {
				t.Fatal(err)
			}
			old := http.DefaultTransport
			defer func() { http.DefaultTransport = old }()
			http.DefaultTransport = fakeTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.Host != "api.app-prg1.zerops.io" || r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Fatal("unsafe lookup")
				}
				switch r.URL.Path {
				case "/api/rest/public/user/client-list":
					return response(`{"list":[{"clientId":"` + "org-test" + `","client":{"id":"` + "org-test" + `"}}],"total":1}`), nil
				case "/api/rest/public/project/p":
					resp := response(tt.body)
					resp.StatusCode = tt.status
					return resp, nil
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
					return nil, nil
				}
			})
			out, err := capture(t, "record-ipv4-release", "p")
			if (err == nil) != tt.allowed {
				t.Fatalf("output=%q err=%v", out, err)
			}
			if strings.Contains(out, "test-secret") || (err != nil && strings.Contains(err.Error(), "test-secret")) {
				t.Fatal("token leaked")
			}
			if tt.allowed && out != "project \"p\": IPv4 release recorded\n" {
				t.Fatalf("unexpected output %q", out)
			}
			if !tt.allowed && (out != "" || !strings.Contains(err.Error(), `project "p": IPv4 release refused:`)) {
				t.Fatalf("unsafe refusal %q %v", out, err)
			}
			recorded, readErr := client.ReadLedger(ledger)
			if readErr != nil || recorded.IPv4 == tt.allowed || recorded.IPv4Projects["p"] != "192.0.2.1" {
				t.Fatalf("ledger=%+v err=%v", recorded, readErr)
			}
		})
	}
}

func TestRecordIPv4ReleaseUsage(t *testing.T) {
	t.Setenv("ZEROPS_TARGET", "prod")
	t.Setenv("ZEROPS_PROD_LEDGER", filepath.Join(t.TempDir(), "ledger"))
	for _, args := range [][]string{{"record-ipv4-release"}, {"record-ipv4-release", "p", "probe"}} {
		_, err := capture(t, args...)
		if err == nil || err.Error() != "usage: zguard record-ipv4-release PROJECT_ID" {
			t.Fatalf("error=%v", err)
		}
	}
}
