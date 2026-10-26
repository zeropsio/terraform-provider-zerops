package client

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProductionExactMutationRoutes(t *testing.T) {
	routes := map[string][]string{
		"PUT":    {"project/p", "project/p/mode-upgrade", "project/p/public-ipv4-shared", "project/p/sync-public-http-routing", "project/p/sync-public-port-routing", "project/p/request-ipv4", "project/p/remote-logging", "project/p/l7httpbalancer-config", "project/p/core-stack/prometheus-config", "service-stack/s/autoscaling", "service-stack/s/start", "service-stack/s/stop", "service-stack/s/enable-subdomain-access", "service-stack/s/disable-subdomain-access", "service-stack/s/object-storage-size", "service-stack/s/trigger-pipeline", "service-stack/s/backup-config", "service-stack/s/restart", "service-stack/s/external-repository-integration", "service-stack/s/cdn", "project-env/e", "user-data/e", "public-http-routing/h", "public-http-routing/h/restore", "public-http-routing/h/location", "public-http-routing/h/trigger-domains-dns-check", "public-port-routing/h", "public-port-routing/h/restore", "app-version/a/deploy", "app-version/a/build-and-deploy"},
		"POST":   {"project/p/env", "project/p/service-stack", "project/p/service-stack/object_storage_v1", "project/p/service-stack/import", "project/p/public-http-routing", "project/p/vpn", "service-stack/s/user-data", "service-stack/s/public-port-routing", "service-stack/s/app-version"},
		"DELETE": {"project/p", "project/p/vpn", "service-stack/s", "service-stack/s/cdn", "project-env/e", "user-data/e", "public-http-routing/h", "public-port-routing/h"},
	}
	for method, paths := range routes {
		for _, path := range paths {
			parts := strings.Split(path, "/")
			if !productionMutationRoute(method, parts) {
				t.Errorf("refused %s %s", method, path)
			}
			if productionMutationRoute("PATCH", parts) {
				t.Errorf("allowed PATCH %s", path)
			}
		}
	}
	for _, path := range []string{"project/p/arbitrary", "project/p/service-stack/import/extra", "service-stack/s/arbitrary", "public-http-routing/h/location/extra", "user-data/u/reveal", "process/p/cancel", "app-version/a"} {
		for _, method := range []string{"PUT", "POST", "DELETE", "PATCH"} {
			if productionMutationRoute(method, strings.Split(path, "/")) {
				t.Errorf("allowed %s %s", method, path)
			}
		}
	}
}

func TestProductionTokenOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, method, route, body, remoteName, ambient string
		ledger, project                                bool
		ok                                             bool
	}{
		{"create", "POST", "/client/org/integration-token", `{"name":"tfacc-token","roleCode":"NO_ACCESS","projects":[{"projectId":"p"}]}`, "", "", false, true, true},
		{"create sudo false", "POST", "/client/org/integration-token", `{"name":"tfacc-token","roleCode":"NO_ACCESS","sudo":false,"projects":[{"projectId":"p"}]}`, "", "", false, true, true},
		{"create sudo refused", "POST", "/client/org/integration-token", `{"name":"tfacc-token","roleCode":"NO_ACCESS","sudo":true,"projects":[{"projectId":"p"}]}`, "", "", false, true, false},
		{"empty scopes", "POST", "/client/org/integration-token", `{"name":"tfacc-token","roleCode":"NO_ACCESS","projects":[]}`, "", "", false, false, true},
		{"foreign org", "POST", "/client/foreign/integration-token", `{"name":"tfacc-token","roleCode":"NO_ACCESS","projects":[]}`, "", "", false, false, false},
		{"bad prefix", "POST", "/client/org/integration-token", `{"name":"real-token","roleCode":"NO_ACCESS","projects":[]}`, "", "", false, false, false},
		{"missing scopes", "POST", "/client/org/integration-token", `{"name":"tfacc-token","roleCode":"NO_ACCESS"}`, "", "", false, false, false},
		{"foreign scope", "POST", "/client/org/integration-token", `{"name":"tfacc-token","roleCode":"NO_ACCESS","projects":[{"projectId":"foreign"}]}`, "", "", false, true, false},
		{"update", "PUT", "/client/org/integration-token/t", `{"name":"tfacc-renamed","roleCode":"NO_ACCESS","projects":[{"projectId":"p"}]}`, "tfacc-token", "ambient", true, true, true},
		{"update sudo false", "PUT", "/client/org/integration-token/t", `{"name":"tfacc-renamed","roleCode":"NO_ACCESS","sudo":false,"projects":[{"projectId":"p"}]}`, "tfacc-token", "ambient", true, true, true},
		{"update sudo refused", "PUT", "/client/org/integration-token/t", `{"name":"tfacc-renamed","roleCode":"NO_ACCESS","sudo":true,"projects":[{"projectId":"p"}]}`, "tfacc-token", "ambient", true, true, false},
		{"rename refused", "PUT", "/client/org/integration-token/t", `{"name":"important","roleCode":"NO_ACCESS","projects":[]}`, "tfacc-token", "ambient", true, false, false},
		{"delete", "DELETE", "/client/org/integration-token/t", "", "tfacc-token", "ambient", true, false, true},
		{"rotate", "PUT", "/client/org/integration-token/t/regenerate", "", "tfacc-token", "ambient", true, false, true},
		{"wrong rotate method", "POST", "/client/org/integration-token/t/regenerate", "", "tfacc-token", "ambient", true, false, false},
		{"no ledger", "DELETE", "/client/org/integration-token/t", "", "tfacc-token", "ambient", false, false, false},
		{"unprefixed metadata", "DELETE", "/client/org/integration-token/t", "", "important", "ambient", true, false, false},
		{"ambient token", "DELETE", "/client/org/integration-token/t", "", "tfacc-token", "t", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ledger := guardLedger(t)
			if tc.project {
				if err := RecordProject(ledger, "p", "tfacc-project", "acctest"); err != nil {
					t.Fatal(err)
				}
			}
			if tc.ledger {
				f, err := openLedger(ledger)
				if err != nil {
					t.Fatal(err)
				}
				err = appendRecord(f, "token", "t", "tfacc-token", "acctest")
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			sent := 0
			g := &ProdTransport{OrgID: "org", LedgerPath: ledger, Base: guardRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					switch r.URL.Path {
					case "/api/rest/public/user/info":
						return guardResponse(200, `{"id":"`+tc.ambient+`"}`), nil
					case "/api/rest/public/project/p":
						return guardResponse(200, `{"clientId":"org","name":"tfacc-project"}`), nil
					default:
						return guardResponse(200, `{"id":"t","name":"`+tc.remoteName+`"}`), nil
					}
				}
				sent++
				return guardResponse(200, `{"id":"t","token":"never-log-secret"}`), nil
			})}
			response, err := g.RoundTrip(guardRequest(tc.method, tc.route, tc.body))
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
			if err != nil {
				if sent != 0 || strings.Contains(err.Error(), "never-log-secret") {
					t.Fatal("unsafe denied mutation")
				}
				return
			}
			data, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if !strings.Contains(string(data), "never-log-secret") {
				t.Fatal("response consumed")
			}
			if tc.method == "POST" {
				l, err := ReadLedger(ledger)
				if err != nil || l.Tokens["t"] != "tfacc-token" || len(l.Projects) != btoi(tc.project) {
					t.Fatalf("ledger=%v err=%v", l, err)
				}
			}
		})
	}
}
func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestProductionUnknownRoutesFailBeforeOwnership(t *testing.T) {
	g := &ProdTransport{OrgID: "org", Base: guardRoundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network lookup"); return nil, nil })}
	for _, path := range []string{"/project/p/unlisted", "/service-stack/s/unlisted", "/public-http-routing/h/location/extra", "/process/p/cancel"} {
		if err := g.Check(context.Background(), guardRequest("PUT", path, `{}`)); err == nil {
			t.Errorf("accepted %s", path)
		}
	}
}

func TestProductionImportRecordsProjectID(t *testing.T) {
	path := guardLedger(t)
	g := &ProdTransport{OrgID: "org", LedgerPath: path, Base: guardRoundTripper(func(*http.Request) (*http.Response, error) {
		return guardResponse(200, `{"projectId":"p","serviceStacks":[]}`), nil
	})}
	resp, err := g.RoundTrip(guardRequest("POST", "/client/org/project/import", `{"yaml":"project:\n  name: tfacc-recipe\nservices: []\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	l, err := ReadLedger(path)
	if err != nil || l.Projects["p"] != "tfacc-recipe" {
		t.Fatalf("ledger=%v err=%v", l, err)
	}
}

func TestProductionTokenRefusesOrganizationPermissions(t *testing.T) {
	for _, method := range []string{"POST", "PUT"} {
		for _, projects := range []string{`[]`, `[{"projectId":"p","roleCode":"READ_ONLY"}]`} {
			for _, permission := range []string{
				`"roleCode":"OWNER"`, `"roleCode":"ADMIN"`, `"roleCode":"BASIC_USER"`,
				`"roleCode":"READ_ONLY"`, `"roleCode":""`, `"roleCode":null`,
				`"canCreateProjects":true`, `"canViewFinances":true`, `"canEditFinances":true`,
				`"sudo":false`, // Missing roleCode must also fail closed.
			} {
				t.Run(method+"/"+projects+"/"+permission, func(t *testing.T) {
					path := guardLedger(t)
					if err := RecordProject(path, "p", "tfacc-owned", "acctest"); err != nil {
						t.Fatal(err)
					}
					f, err := openLedger(path)
					if err != nil {
						t.Fatal(err)
					}
					err = appendRecord(f, "token", "t", "tfacc-owned-token", "acctest")
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
					g := &ProdTransport{OrgID: "org", LedgerPath: path, Base: guardRoundTripper(func(req *http.Request) (*http.Response, error) {
						if req.Method != "GET" {
							t.Fatal("elevated token mutation reached network")
						}
						switch req.URL.Path {
						case "/api/rest/public/user/info":
							return guardResponse(200, `{"id":"ambient"}`), nil
						case "/api/rest/public/client/org/integration-token/t":
							return guardResponse(200, `{"id":"t","name":"tfacc-owned-token"}`), nil
						case "/api/rest/public/project/p":
							return guardResponse(200, `{"clientId":"org","name":"tfacc-owned"}`), nil
						default:
							t.Fatal("unexpected lookup")
							return nil, nil
						}
					})}
					route := "/client/org/integration-token"
					if method == "PUT" {
						route += "/t"
					}
					role := ""
					if strings.HasPrefix(permission, `"can`) {
						role = `"roleCode":"NO_ACCESS",`
					}
					body := `{"name":"tfprobe-wide",` + role + permission + `,"projects":` + projects + `}`
					if _, err := g.RoundTrip(guardRequest(method, route, body)); err == nil {
						t.Fatal("organization-wide permission accepted")
					}
				})
			}
		}
	}
}
