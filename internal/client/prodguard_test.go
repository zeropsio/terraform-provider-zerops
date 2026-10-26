package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type guardRoundTripper func(*http.Request) (*http.Response, error)

func (f guardRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func guardResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func guardRequest(method, path, body string) *http.Request {
	r, _ := http.NewRequest(method, ProductionEndpoint+"/api/rest/public"+path, strings.NewReader(body))
	return r
}
func guardLedger(t *testing.T) string { t.Helper(); return filepath.Join(t.TempDir(), "ledger") }
func TestProductionTargets(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for _, value := range []string{"", "dev", "prod"} {
		if _, err := ParseTarget(value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ParseTarget("production"); err == nil {
		t.Fatal("unknown target accepted")
	}
	for _, target := range []Target{TargetDev, TargetProd} {
		for _, endpoint := range []string{ProductionEndpoint, "https://api.app-prg1.zerops.io.evil.test", "https://x.api.app-prg1.zerops.io", "http://api.app-prg1.zerops.io", "https://secret@api.app-prg1.zerops.io", "https://api.other.zerops.io", "https://api.app-prg1.zerops.io:443", "https://api.app-prg1.zerops.io/"} {
			err := ValidateTargetEndpoint(target, endpoint)
			want := target == TargetProd && endpoint == ProductionEndpoint
			if (err == nil) != want {
				t.Errorf("%s %s: %v", target, endpoint, err)
			}
		}
	}
	for _, value := range []string{"https://api.dev.example.test", "https://proxy.dev.example.test"} {
		if err := ValidateTargetEndpoint(TargetDev, value); err != nil {
			t.Fatal(err)
		}
	}
}
func TestProductionResolveOrganization(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	tests := []struct {
		name, body, requested string
		ok                    bool
	}{
		{"pinned", `{"list":[{"id":"membership","clientId":"org-test","client":{"id":"org-test","accountName":"Zerops"}}],"total":1}`, "org-test", true},
		{"auto", `{"list":[{"id":"membership","clientId":"org-test","client":{"id":"org-test","accountName":"Zerops"}},{"id":"other-membership","clientId":"other","client":{"id":"other","accountName":"OTHER"}}],"total":2}`, "", false},
		{"name match is not enough", `{"list":[{"id":"m","clientId":"lookalike","client":{"id":"lookalike","accountName":"ZEROPS"}}],"total":1}`, "", false},
		{"no membership", `{"list":[{"id":"m","clientId":"org","client":{"id":"org","accountName":"OTHER"}}],"total":1}`, "", false},
		{"foreign requested", `{"list":[{"id":"m","clientId":"org-test","client":{"id":"org-test","accountName":"Zerops"}},{"id":"n","clientId":"foreign","client":{"id":"foreign","accountName":"Zerops Dev"}}],"total":2}`, "foreign", true},
		{"membership is not org", `{"list":[{"id":"membership","clientId":"org-test","client":{"id":"org-test","accountName":"Zerops"}}],"total":1}`, "membership", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := guardRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.Path != "/api/rest/public/user/client-list" {
					t.Fatalf("unexpected bootstrap request %s %s", r.Method, r.URL.Path)
				}
				return guardResponse(200, tt.body), nil
			})
			g, err := NewProdTransport(context.Background(), Config{Endpoint: ProductionEndpoint, Token: "secret", Transport: base}, tt.requested, guardLedger(t), "provider")
			if (err == nil) != tt.ok {
				t.Fatalf("guard=%v err=%v", g, err)
			}
			if tt.ok && g.OrgID != tt.requested {
				t.Fatalf("wrong organization %s", g.OrgID)
			}
		})
	}
}
func TestProductionHostAndBootstrapRefusals(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	g := &ProdTransport{OrgID: "org", LedgerPath: guardLedger(t), Base: guardRoundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("refused request reached base"); return nil, nil })}
	for _, value := range []string{"https://api.app-prg1.zerops.io.evil.test/path?secret=signature", "https://x.api.app-prg1.zerops.io", "http://api.app-prg1.zerops.io", "https://secret@api.app-prg1.zerops.io", "https://api.app-prg1.zerops.io:443", "https://proxy.dev.example.test/upload?secret=signature"} {
		r, _ := http.NewRequest("PUT", value, nil)
		r.Header.Set("Authorization", "Bearer secret-token")
		_, err := g.RoundTrip(r)
		if err == nil {
			t.Fatal("unsafe host accepted")
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "signature") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
	for _, method := range []string{"GET", "HEAD", "PUT"} {
		if _, err := g.RoundTrip(guardRequest(method, "/client/foreign/project", "")); err == nil {
			t.Fatal("foreign organization accepted")
		}
	}
	g.OrgID = ""
	if err := g.Check(context.Background(), guardRequest("GET", "/project/p", "")); err == nil {
		t.Fatal("unpinned GET accepted")
	}
}
func TestProductionDefaultDeny(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	g := &ProdTransport{OrgID: "org", LedgerPath: guardLedger(t)}
	for _, path := range []string{"/user-token", "/registration", "/auth/login", "/user/settings", "/client/org", "/client/org/integration-token", "/payments", "/project/../client/org"} {
		for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
			if err := g.Check(context.Background(), guardRequest(method, path, `{}`)); err == nil {
				t.Errorf("allowed %s %s", method, path)
			}
		}
	}
	for _, body := range []string{`{}`, `{"name":"production"}`, `not json`} {
		if err := g.Check(context.Background(), guardRequest("POST", "/client/org/project", body)); err == nil {
			t.Fatal("unprefixed project accepted")
		}
	}
	for _, body := range []string{`{"yaml":"project:\n  name: real-project\n"}`, `{"yaml":"!broken ["}`} {
		if err := g.Check(context.Background(), guardRequest("POST", "/client/org/project/import", body)); err == nil {
			t.Fatal("unprefixed import accepted")
		}
	}
}
func TestProductionCreateRecordsLedger(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for _, importing := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "import"}[importing], func(t *testing.T) {
			path := guardLedger(t)
			g := &ProdTransport{OrgID: "org", LedgerPath: path, Source: "probe", Base: guardRoundTripper(func(r *http.Request) (*http.Response, error) {
				return guardResponse(200, map[bool]string{false: `{"id":"created"}`, true: `{"project":{"id":"created"},"process":{"id":"process"}}`}[importing]), nil
			})}
			route, body := "/client/org/project", `{"name":"tfacc-created"}`
			if importing {
				route += "/import"
				body = `{"yaml":"project:\n  name: tfprobe-import\n"}`
			}
			resp, err := g.RoundTrip(guardRequest("POST", route, body))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			data, _ := io.ReadAll(resp.Body)
			if len(data) == 0 {
				t.Fatal("response body consumed")
			}
			ledger, err := ReadLedger(path)
			if err != nil || !guardPrefix(ledger.Projects["created"]) {
				t.Fatalf("ledger=%v err=%v", ledger, err)
			}
		})
	}
}
func TestProductionOwnership(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	cases := []struct {
		name, org, projectName string
		record                 bool
		ok                     bool
	}{{"owned", "org", "tfacc-created", true, true}, {"not recorded", "org", "tfacc-created", false, false}, {"foreign org", "foreign", "tfacc-created", true, false}, {"unprefixed", "org", "important", true, false}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := guardLedger(t)
			if tt.record {
				if err := RecordProject(path, "p", "tfacc-created", "acctest"); err != nil {
					t.Fatal(err)
				}
			}
			mutations := 0
			g := &ProdTransport{OrgID: "org", LedgerPath: path, Base: guardRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					return guardResponse(200, `{"clientId":"`+tt.org+`","name":"`+tt.projectName+`"}`), nil
				}
				mutations++
				return guardResponse(200, `{}`), nil
			})}
			_, err := g.RoundTrip(guardRequest("DELETE", "/project/p", ""))
			if (err == nil) != tt.ok || mutations != map[bool]int{true: 1, false: 0}[tt.ok] {
				t.Fatalf("mutations %d err %v", mutations, err)
			}
		})
	}
}
func TestProductionServiceImportAndReservedHostnames(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	cases := []struct {
		name, method, path, body string
		ok                       bool
	}{
		{"import ok", "POST", "/project/p/service-stack/import", `{"yaml":"services:\n  - hostname: app\n    type: nodejs@22\n"}`, true},
		{"import core", "POST", "/project/p/service-stack/import", `{"yaml":"services:\n  - hostname: Core\n    type: nodejs@22\n"}`, false},
		{"import project section", "POST", "/project/p/service-stack/import", `{"yaml":"project:\n  name: tfacc-x\nservices:\n  - hostname: app\n"}`, false},
		{"import empty hostname", "POST", "/project/p/service-stack/import", `{"yaml":"services:\n  - type: nodejs@22\n"}`, false},
		{"import invalid yaml", "POST", "/project/p/service-stack/import", `{"yaml":"services: [\n"}`, false},
		{"create core", "POST", "/project/p/service-stack", `{"name":" core ","serviceStackVersionName":"alpine/nodejs@22"}`, false},
		{"create storage core", "POST", "/project/p/service-stack/object_storage_v1", `{"name":"core","diskGBytes":1}`, false},
		{"create app", "POST", "/project/p/service-stack", `{"name":"app","serviceStackVersionName":"alpine/nodejs@22"}`, true},
		{"project import core", "POST", "/client/org/project/import", `{"yaml":"project:\n  name: tfacc-x\nservices:\n  - hostname: core\n"}`, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := guardLedger(t)
			if err := RecordProject(path, "p", "tfacc-created", "acctest"); err != nil {
				t.Fatal(err)
			}
			mutations := 0
			g := &ProdTransport{OrgID: "org", LedgerPath: path, Base: guardRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					return guardResponse(200, `{"clientId":"org","name":"tfacc-created"}`), nil
				}
				mutations++
				return guardResponse(200, `{"projectId":"q","projectName":"tfacc-x","serviceStacks":[]}`), nil
			})}
			_, err := g.RoundTrip(guardRequest(tt.method, tt.path, tt.body))
			if (err == nil) != tt.ok || mutations != map[bool]int{true: 1, false: 0}[tt.ok] {
				t.Fatalf("mutations %d err %v", mutations, err)
			}
		})
	}
}
func TestProductionObjectOwnership(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for kind := range productionObjectRoutes {
		t.Run(kind, func(t *testing.T) {
			path := guardLedger(t)
			if err := RecordProject(path, "p", "tfacc-created", "acctest"); err != nil {
				t.Fatal(err)
			}
			var calls []string
			g := &ProdTransport{OrgID: "org", LedgerPath: path, Base: guardRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				if r.Method != "GET" {
					return guardResponse(200, `{}`), nil
				}
				switch r.URL.Path {
				case "/api/rest/public/project/p":
					return guardResponse(200, `{"clientId":"org","name":"tfacc-created"}`), nil
				case "/api/rest/public/service-stack/s":
					return guardResponse(200, `{"projectId":"p"}`), nil
				default:
					return guardResponse(200, `{"serviceStackId":"s"}`), nil
				}
			})}
			id := "object"
			if kind == "service-stack" {
				id = "s"
			}
			route := "/" + kind + "/" + id
			if kind == "service-stack" {
				route += "/restart"
			}
			if kind == "app-version" {
				route += "/deploy"
			}
			if _, err := g.RoundTrip(guardRequest("PUT", route, `{}`)); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(calls, "\n")
			if !strings.Contains(joined, "GET /api/rest/public/service-stack/s") || !strings.Contains(joined, "GET /api/rest/public/project/p") {
				t.Fatal(joined)
			}
		})
	}
}
func TestProductionOneIPv4(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	path := guardLedger(t)
	if err := RecordProject(path, "p", "tfacc-created", "acctest"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	acquisitions := 0
	base := guardRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return guardResponse(200, `{"clientId":"org","name":"tfacc-created"}`), nil
		}
		mu.Lock()
		acquisitions++
		mu.Unlock()
		return guardResponse(200, `{"ipv4":"192.0.2.1"}`), nil
	})
	var wg sync.WaitGroup
	success := 0
	for range 2 {
		wg.Go(func() {
			g := &ProdTransport{OrgID: "org", LedgerPath: path, Source: "acctest", Base: base}
			if _, err := g.RoundTrip(guardRequest("PUT", "/project/p/request-ipv4", "")); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if acquisitions != 1 || success != 1 {
		t.Fatalf("acquisitions=%d success=%d", acquisitions, success)
	}
	ledger, err := ReadLedger(path)
	if err != nil || !ledger.IPv4 {
		t.Fatalf("ledger=%v err=%v", ledger, err)
	}
}
func TestProductionLedgerSafety(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	path := guardLedger(t)
	if err := RecordProject(path, "p", "tfacc-created", "provider"); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", st.Mode())
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLedger(link); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLedger(path); err == nil {
		t.Fatal("insecure permissions accepted")
	}
	if _, err := ReadLedger("relative"); err == nil {
		t.Fatal("relative ledger accepted")
	}
}
func TestProductionRedirectsRefused(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	g := &ProdTransport{OrgID: "org", Base: guardRoundTripper(func(*http.Request) (*http.Response, error) {
		r := guardResponse(307, "")
		r.Header.Set("Location", "https://evil.test/?token=secret")
		return r, nil
	})}
	_, err := g.RoundTrip(guardRequest("GET", "/project/p", ""))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe redirect error %v", err)
	}
}

func TestProductionIPv4ReservationSurvivesFailure(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	path := guardLedger(t)
	if err := RecordProject(path, "p", "tfacc-created", "acctest"); err != nil {
		t.Fatal(err)
	}
	requests := 0
	g := &ProdTransport{OrgID: "org", LedgerPath: path, Base: guardRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return guardResponse(200, `{"clientId":"org","name":"tfacc-created"}`), nil
		}
		requests++
		return guardResponse(503, `{}`), nil
	})}
	response, err := g.RoundTrip(guardRequest("PUT", "/project/p/request-ipv4", ""))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	// A separate transport must also see the reservation after an ambiguous
	// failure, rather than depending on an in-memory success cache.
	next := &ProdTransport{OrgID: "org", LedgerPath: path, Base: g.Base}
	if _, err := next.RoundTrip(guardRequest("PUT", "/project/p/request-ipv4", "")); err == nil {
		t.Fatal("IPv4 retried after reservation")
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestProductionCreationLedgerFailureReportsID(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	path := guardLedger(t)
	g := &ProdTransport{OrgID: "org", LedgerPath: path, Base: guardRoundTripper(func(r *http.Request) (*http.Response, error) {
		// Simulate losing access to the ledger after the API created a project.
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		return guardResponse(200, `{"id":"cleanup-id"}`), nil
	})}
	api, err := New(Config{Endpoint: ProductionEndpoint, Token: "private-token", Transport: g})
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.CreateProject(context.Background(), "org", ProjectCreate{Name: "tfacc-created"})
	if err == nil || !strings.Contains(err.Error(), "cleanup-id") {
		t.Fatalf("cleanup id absent from error: %v", err)
	}
}

func TestProductionUploadAllowlist(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	if err := ValidateUploadURL(TargetProd, "https://proxy.app-prg1.zerops.io/upload?token=private-signature"); err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{
		"https://proxy.app-prg1.zerops.io.evil.test/upload?token=private-signature",
		"https://proxy.app-prg1.zerops.io:8443/upload?token=private-signature",
		"http://proxy.app-prg1.zerops.io/upload?token=private-signature",
		"https://x.proxy.app-prg1.zerops.io/upload?token=private-signature",
		"https://proxy.dev.example.test/upload?token=private-signature",
	} {
		err := ValidateUploadURL(TargetProd, unsafe)
		if err == nil || strings.Contains(err.Error(), "private-signature") || strings.Contains(err.Error(), "/upload") {
			t.Fatalf("unsafe upload refusal for %s: %v", unsafe, err)
		}
	}
	if err := ValidateUploadURL(TargetDev, "https://proxy.dev.example.test/upload?token=private-signature"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateUploadURL(TargetDev, "https://proxy.app-prg1.zerops.io/upload?token=private-signature"); err == nil {
		t.Fatal("dev target accepted production upload host")
	}
}

func TestCreatedProjectImportShapes(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for _, response := range []string{
		`{"projectId":"created","projectName":"tfprobe-import","serviceStacks":[]}`,
		`{"id":"process-not-project","project":{"id":"created"}}`,
	} {
		req := guardRequest("POST", "/client/org/project/import", `{"yaml":"project:\n  name: tfprobe-import\n"}`)
		id, name, err := CreatedProject(req, []byte(response))
		if err != nil || id != "created" || name != "tfprobe-import" {
			t.Fatalf("id=%s name=%s err=%v", id, name, err)
		}
	}
	req := guardRequest("POST", "/client/org/project/import", `{"yaml":"project:\n  name: tfprobe-import\n"}`)
	if _, _, err := CreatedProject(req, []byte(`{"id":"process-not-project"}`)); err == nil {
		t.Fatal("import process id treated as project id")
	}
}

func TestProductionMutationCanonicalRoutes(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	tests := []struct {
		method string
		path   string
	}{
		{"PUT", "/api/rest/public/project/p/request-ipv4?ignored=true"},
		{"PUT", "/api/rest/public/project/p/request-ipv4?"},
		{"POST", "/api/rest/public/client/org/project?ignored=true"},
		{"POST", "/api/rest/public/client/org/project?"},
		{"POST", "/api/rest/public/client/org/project/import?ignored=true"},
		{"DELETE", "//project/p"},
		{"DELETE", "/api/rest/public//project/p"},
		{"DELETE", "/api/rest/public/project/p//action"},
		{"PUT", "/api/rest/public/project/p/request-ipv4/"},
		{"POST", "/api/rest/public/client/org/project/"},
		{"POST", "/api/rest/public/client/org/project/import/"},
		{"DELETE", "/api/rest/public/project/./p"},
		{"DELETE", "/api/rest/public/project/other/../p"},
		{"DELETE", "/api/rest/public/project/p%2faction"},
		{"DELETE", "/api/rest/public/project/p%2Faction"},
		{"DELETE", "/api/rest/public/project/%2e%2e/p"},
		{"POST", "/api/rest/publicclient/org/project"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			calls := 0
			ledgerPath := guardLedger(t)
			g := &ProdTransport{
				OrgID:      "org",
				LedgerPath: ledgerPath,
				Base: guardRoundTripper(func(*http.Request) (*http.Response, error) {
					calls++
					return guardResponse(200, `{"id":"unexpected"}`), nil
				}),
			}
			req, err := http.NewRequest(tt.method, ProductionEndpoint+tt.path, strings.NewReader(`{"name":"tfacc-created","yaml":"project:\n  name: tfprobe-import\n"}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := g.RoundTrip(req); err == nil {
				t.Fatal("noncanonical mutation accepted")
			}
			if calls != 0 {
				t.Fatalf("refused request sent %d network requests", calls)
			}
			if _, err := os.Stat(ledgerPath); !os.IsNotExist(err) {
				t.Fatalf("refused mutation touched ledger: %v", err)
			}
			if tt.method == http.MethodPost {
				if _, _, err := CreatedProject(req, []byte(`{"id":"unexpected","projectId":"unexpected"}`)); err == nil {
					t.Fatal("response recorder accepted noncanonical creation route")
				}
			}
		})
	}
}

func TestProductionCanonicalRecordingAndReadQueries(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for _, prefix := range []string{"", "/api/rest/public"} {
		t.Run(prefix, func(t *testing.T) {
			path := guardLedger(t)
			g := &ProdTransport{OrgID: "org", LedgerPath: path, Base: guardRoundTripper(func(*http.Request) (*http.Response, error) {
				return guardResponse(200, `{"id":"created"}`), nil
			})}
			req, err := http.NewRequest(http.MethodPost, ProductionEndpoint+prefix+"/client/org/project", strings.NewReader(`{"name":"tfacc-created"}`))
			if err != nil {
				t.Fatal(err)
			}
			response, err := g.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			ledger, err := ReadLedger(path)
			if err != nil || ledger.Projects["created"] != "tfacc-created" {
				t.Fatalf("ledger=%v err=%v", ledger, err)
			}
		})
	}
	g := &ProdTransport{OrgID: "org"}
	if err := g.Check(context.Background(), guardRequest(http.MethodGet, "/project/p?offset=0&limit=100", "")); err != nil {
		t.Fatalf("read query refused: %v", err)
	}
}

func TestProductionDeleteProjectLedgerFallback(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for _, tt := range []struct {
		name, method, path, ledgerName, body string
		status                               int
		transportError, allowed              bool
	}{
		{"500", "DELETE", "/project/p", "tfacc-p", "", 500, false, true},
		{"503", "DELETE", "/project/p", "tfprobe-p", "", 503, false, true},
		{"missing ledger", "DELETE", "/project/p", "", "", 500, false, false},
		{"unprefixed ledger", "DELETE", "/project/p", "important", "", 500, false, false},
		{"403", "DELETE", "/project/p", "tfacc-p", "", 403, false, false},
		{"404", "DELETE", "/project/p", "tfacc-p", "", 404, false, false},
		{"400", "DELETE", "/project/p", "tfacc-p", "", 400, false, false},
		{"transport", "DELETE", "/project/p", "tfacc-p", "", 0, true, false},
		{"put", "PUT", "/project/p", "tfacc-p", "", 500, false, false},
		{"post", "POST", "/project/p", "tfacc-p", "", 500, false, false},
		{"vpn", "DELETE", "/project/p/vpn", "tfacc-p", "", 500, false, false},
		{"stack", "DELETE", "/service-stack/s", "tfacc-p", "", 500, false, false},
		{"wrong org", "DELETE", "/project/p", "tfacc-p", `{"clientId":"foreign","name":"tfacc-p"}`, 200, false, false},
		{"wrong name", "DELETE", "/project/p", "tfacc-p", `{"clientId":"org","name":"important"}`, 200, false, false},
		{"decode", "DELETE", "/project/p", "tfacc-p", `{`, 200, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := guardLedger(t)
			if tt.ledgerName != "" {
				f, err := openLedger(path)
				if err != nil {
					t.Fatal(err)
				}
				err = appendRecord(f, "project", "p", tt.ledgerName, "probe")
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			logFile, err := os.CreateTemp(t.TempDir(), "stderr")
			if err != nil {
				t.Fatal(err)
			}
			defer logFile.Close()
			old := os.Stderr
			os.Stderr = logFile
			defer func() { os.Stderr = old }()
			mutations := 0
			g := &ProdTransport{OrgID: "org", LedgerPath: path, Base: guardRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != productionHost {
					t.Fatal("wrong preflight host")
				}
				if r.Method == "GET" {
					if tt.transportError {
						return nil, errors.New("secret transport failure")
					}
					return guardResponse(tt.status, tt.body), nil
				}
				mutations++
				return guardResponse(200, `{}`), nil
			})}
			resp, err := g.RoundTrip(guardRequest(tt.method, tt.path, `{}`))
			if resp != nil {
				resp.Body.Close()
			}
			if (err == nil) != tt.allowed || mutations != map[bool]int{true: 1, false: 0}[tt.allowed] {
				t.Fatalf("mutations=%d error=%v", mutations, err)
			}
			if _, err := logFile.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			logs, err := io.ReadAll(logFile)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if tt.allowed {
				want = fmt.Sprintf("prodguard: ledger-proven ownership fallback used for DELETE /project/p after preflight HTTP %d\n", tt.status)
			}
			if string(logs) != want {
				t.Fatalf("log=%q want=%q", logs, want)
			}
			if tt.allowed {
				if _, cached := g.owned.Load("p"); cached {
					t.Fatal("fallback cached ownership")
				}
				if err := g.Check(context.Background(), guardRequest("PUT", "/project/p", `{}`)); err == nil {
					t.Fatal("fallback leaked to PUT")
				}
			}
		})
	}
}

func guardLedgerRecords(t *testing.T, records ...string) string {
	t.Helper()
	path := guardLedger(t)
	var data strings.Builder
	for _, record := range records {
		fmt.Fprintf(&data, "2026-10-26T00:00:00Z\t%s\tprobe\n", record)
	}
	if err := os.WriteFile(path, []byte(data.String()), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProductionIPv4ReleaseLedger(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for _, tt := range []struct {
		name    string
		records []string
		active  bool
	}{
		{"acquired", []string{"ipv4\tp\t192.0.2.1"}, true},
		{"reserved", []string{"note\tp\tipv4-reserved"}, true},
		{"released", []string{"note\tp\tipv4-reserved", "ipv4\tp\t192.0.2.1", "note\tp\tipv4-released"}, false},
		{"early release", []string{"note\tp\tipv4-released", "ipv4\tp\t192.0.2.1"}, true},
		{"two holders", []string{"ipv4\tp\t192.0.2.1", "ipv4\tq\t192.0.2.2", "note\tp\tipv4-released"}, true},
		{"new reservation", []string{"ipv4\tp\t192.0.2.1", "note\tp\tipv4-released", "note\tp\tipv4-reserved"}, true},
		{"wrong id release", []string{"note\tp\tipv4-reserved", "note\tq\tipv4-released"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ledger, err := ReadLedger(guardLedgerRecords(t, tt.records...))
			if err != nil || ledger.IPv4 != tt.active {
				t.Fatalf("ledger=%+v err=%v", ledger, err)
			}
			for _, record := range tt.records {
				fields := strings.Split(record, "\t")
				if fields[0] == "ipv4" && ledger.IPv4Projects[fields[1]] != fields[2] {
					t.Fatal("historical holder lost")
				}
			}
		})
	}
}

func TestProductionRecordIPv4Release(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for _, tt := range []struct {
		name, body              string
		status                  int
		records                 []string
		transportError, allowed bool
	}{
		{"not found", `{"error":{"code":"projectNotFound"}}`, 400, []string{"ipv4\tp\t192.0.2.1"}, false, true},
		{"absent", `{"clientId":"` + "org-test" + `"}`, 200, []string{"ipv4\tp\t192.0.2.1"}, false, true},
		{"null", `{"clientId":"` + "org-test" + `","publicIpV4":null}`, 200, []string{"note\tp\tipv4-reserved"}, false, true},
		{"empty", `{"clientId":"` + "org-test" + `","publicIpV4":""}`, 200, []string{"ipv4\tp\t192.0.2.1"}, false, true},
		{"still held", `{"clientId":"` + "org-test" + `","publicIpV4":"192.0.2.1"}`, 200, []string{"ipv4\tp\t192.0.2.1"}, false, false},
		{"500", `{}`, 500, []string{"ipv4\tp\t192.0.2.1"}, false, false},
		{"403", `{"error":{"code":"projectNotFound"}}`, 403, []string{"ipv4\tp\t192.0.2.1"}, false, false},
		{"404", `{"error":{"code":"projectNotFound"}}`, 404, []string{"ipv4\tp\t192.0.2.1"}, false, false},
		{"other 400", `{"error":{"code":"other"}}`, 400, []string{"ipv4\tp\t192.0.2.1"}, false, false},
		{"foreign", `{"clientId":"foreign"}`, 200, []string{"ipv4\tp\t192.0.2.1"}, false, false},
		{"invalid json", `{`, 200, []string{"ipv4\tp\t192.0.2.1"}, false, false},
		{"transport", ``, 0, []string{"ipv4\tp\t192.0.2.1"}, true, false},
		{"no acquisition", `{}`, 200, nil, false, false},
		{"already released", `{}`, 200, []string{"ipv4\tp\t192.0.2.1", "note\tp\tipv4-released"}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := guardLedgerRecords(t, tt.records...)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			g := &ProdTransport{OrgID: "org-test", LedgerPath: path, Source: "probe", token: "test-secret", Base: guardRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.String() != ProductionEndpoint+"/api/rest/public/project/p" || r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Fatal("unsafe release lookup")
				}
				if tt.transportError {
					return nil, errors.New("test-secret")
				}
				return guardResponse(tt.status, tt.body), nil
			})}
			err = g.RecordIPv4Release(context.Background(), "p")
			if (err == nil) != tt.allowed {
				t.Fatalf("error=%v", err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tt.allowed {
				if !strings.HasPrefix(string(after), string(before)) || !strings.Contains(string(after[len(before):]), "\tnote\tp\tipv4-released\tprobe\n") {
					t.Fatalf("bad release record %q", after)
				}
			} else if string(before) != string(after) {
				t.Fatal("refusal changed ledger")
			}
			if (tt.name == "no acquisition" || tt.name == "already released") && calls != 0 {
				t.Fatal("unnecessary lookup")
			}
		})
	}
}

func TestProductionIPv4RequestAfterRelease(t *testing.T) {
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for _, released := range []bool{false, true} {
		t.Run(fmt.Sprint(released), func(t *testing.T) {
			records := []string{"project\tp\ttfacc-p", "ipv4\tp\t192.0.2.1"}
			if released {
				records = append(records, "note\tp\tipv4-released")
			}
			path := guardLedgerRecords(t, records...)
			mutations := 0
			g := &ProdTransport{OrgID: "org", LedgerPath: path, Source: "probe", Base: guardRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					return guardResponse(200, `{"clientId":"org","name":"tfacc-p"}`), nil
				}
				mutations++
				ledgerData, err := os.ReadFile(path)
				if err != nil || !strings.Contains(string(ledgerData), "\tnote\tp\tipv4-reserved\tprobe\n") {
					t.Fatal("reservation missing before send")
				}
				return guardResponse(200, `{"ipv4":"192.0.2.2"}`), nil
			})}
			resp, err := g.RoundTrip(guardRequest("PUT", "/project/p/request-ipv4", ""))
			if resp != nil {
				resp.Body.Close()
			}
			if (err == nil) != released || mutations != map[bool]int{true: 1, false: 0}[released] {
				t.Fatalf("mutations=%d err=%v", mutations, err)
			}
			ledger, err := ReadLedger(path)
			if err != nil || !ledger.IPv4 {
				t.Fatalf("new acquisition not active: %+v %v", ledger, err)
			}
		})
	}
}

func TestDevelopmentDomainBoundary(t *testing.T) {
	for _, domain := range []string{"", "https://dev.example.test", ".dev.example.test", "dev.example.test.", "bad..example.test", "127.0.0.1"} {
		t.Run("invalid-domain-"+domain, func(t *testing.T) {
			t.Setenv("ZEROPS_DEV_DOMAIN", domain)
			if err := ValidateTargetEndpoint(TargetDev, "https://api.dev.example.test"); err == nil {
				t.Fatal("invalid development domain accepted")
			}
		})
	}
	t.Setenv("ZEROPS_DEV_DOMAIN", "dev.example.test")
	for _, host := range []string{"dev.example.test", "api.dev.example.test", "proxy.dev.example.test"} {
		if err := ValidateTargetEndpoint(TargetDev, "https://"+host); err != nil {
			t.Fatal(err)
		}
		if err := ValidateUploadURL(TargetDev, "https://"+host+"/upload?signature=private-signature"); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"https://evil-dev.example.test", "https://dev.example.test.evil", "http://api.dev.example.test", "https://user@api.dev.example.test", "https://api.dev.example.test/path", "https://api.dev.example.test?query=secret", "https://api.dev.example.test#fragment"} {
		if err := ValidateTargetEndpoint(TargetDev, value); err == nil {
			t.Fatalf("unsafe origin accepted: %s", value)
		}
	}
	for _, value := range []string{"https://evil-dev.example.test/upload?signature=private-signature", "https://dev.example.test.evil/upload?signature=private-signature", "http://api.dev.example.test/upload?signature=private-signature", "https://user@api.dev.example.test/upload?signature=private-signature"} {
		if err := ValidateUploadURL(TargetDev, value); err == nil || strings.Contains(err.Error(), "private-signature") {
			t.Fatal("unsafe or revealing upload validation")
		}
	}
	// Production is forbidden even when its domain would otherwise match.
	t.Setenv("ZEROPS_DEV_DOMAIN", "zerops.io")
	if err := ValidateTargetEndpoint(TargetDev, ProductionEndpoint); err == nil {
		t.Fatal("production origin accepted for development")
	}
	if err := ValidateUploadURL(TargetDev, ProductionEndpoint+"/upload"); err == nil {
		t.Fatal("production origin accepted for development upload")
	}
}
