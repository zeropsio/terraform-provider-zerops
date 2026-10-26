package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestIntegrationTokenAdapter(t *testing.T) {
	ctx := context.Background()
	input := IntegrationTokenWrite{Name: "test", RoleCode: "NO_ACCESS", Projects: []IntegrationTokenProject{{ProjectID: "p", RoleCode: "READ_ONLY"}}, CanCreateProjects: false, CanViewFinances: true, CanEditFinances: false, Sudo: true}
	count := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		expected := []string{"POST /api/rest/public/client/o/integration-token", "GET /api/rest/public/client/o/integration-token/t", "PUT /api/rest/public/client/o/integration-token/t", "PUT /api/rest/public/client/o/integration-token/t/regenerate", "DELETE /api/rest/public/client/o/integration-token/t"}[count-1]
		if r.Method+" "+r.URL.Path != expected {
			t.Errorf("unexpected route %s %s", r.Method, r.URL.Path)
		}
		if count == 1 || count == 3 {
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"name", "roleCode", "projects", "canCreateProjects", "canViewFinances", "canEditFinances", "sudo"} {
				if _, ok := got[key]; !ok {
					t.Errorf("missing %s", key)
				}
			}
			if got["canCreateProjects"] != false || got["sudo"] != true {
				t.Error("boolean payload lost")
			}
			projects := got["projects"].([]any)
			if projects[0].(map[string]any)["projectId"] != "p" {
				t.Error("project scope lost")
			}
		}
		if count == 4 {
			b, _ := io.ReadAll(r.Body)
			if len(b) != 0 || r.Header.Get("Content-Type") != "" {
				t.Error("regeneration must be bodyless")
			}
		}
		if count == 5 {
			respond(w, 200, `{}`)
			return
		}
		secret := ""
		if count == 1 || count == 4 {
			secret = `,"token":"new-secret"`
		}
		respond(w, 200, `{"id":"t","name":"test","roleCode":"NO_ACCESS","projects":[{"projectId":"p","roleCode":"READ_ONLY"}],"canCreateProjects":false,"canViewFinances":true,"canEditFinances":false,"sudo":true`+secret+`}`)
	})
	raw, err := c.CreateIntegrationToken(ctx, "o", input)
	if err != nil || raw.ID != "t" || raw.Token != "new-secret" || !raw.Sudo {
		t.Fatalf("create failed: %v", err)
	}
	meta, err := c.GetIntegrationToken(ctx, "o", "t")
	if err != nil || meta.ID != "t" || len(meta.Projects) != 1 {
		t.Fatalf("read failed: %v", err)
	}
	meta, err = c.UpdateIntegrationToken(ctx, "o", "t", input)
	if err != nil || !meta.CanViewFinances {
		t.Fatalf("update failed: %v", err)
	}
	raw, err = c.RegenerateIntegrationToken(ctx, "o", "t")
	if err != nil || raw.Token != "new-secret" {
		t.Fatalf("rotate failed: %v", err)
	}
	if err := c.DeleteIntegrationToken(ctx, "o", "t"); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("calls %d", count)
	}
}

func TestIntegrationTokenMutationNeverReplays(t *testing.T) {
	for _, action := range []string{"create", "update", "rotate", "delete"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				respond(w, 500, `{"error":{"code":"internalServerError","message":"raw-secret"}}`)
			})
			v := IntegrationTokenWrite{Name: "test", RoleCode: "NO_ACCESS", Projects: []IntegrationTokenProject{}}
			var err error
			switch action {
			case "create":
				_, err = c.CreateIntegrationToken(context.Background(), "o", v)
			case "update":
				_, err = c.UpdateIntegrationToken(context.Background(), "o", "t", v)
			case "rotate":
				_, err = c.RegenerateIntegrationToken(context.Background(), "o", "t")
			case "delete":
				err = c.DeleteIntegrationToken(context.Background(), "o", "t")
			}
			if err == nil || calls != 1 || strings.Contains(err.Error(), "raw-secret") {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestIntegrationTokenAbsence(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, 400, `{"error":{"code":"clientUserConnectionNotFound"}}`)
	})
	_, err := c.GetIntegrationToken(context.Background(), "o", "t")
	if !IsNotFound(err, "clientUserConnectionNotFound") || IsNotFound(err, "projectNotFound") {
		t.Fatal("token-specific absence classification lost")
	}
}
