package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestGitIntegrationWire(t *testing.T) {
	for _, provider := range []string{"githubIntegration", "gitlabIntegration"} {
		t.Run(provider, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					respond(w, 200, `{"`+provider+`":{"repositoryFullName":"owner/repo","eventType":"BRANCH","branchName":"main","isActive":true,"webhookId":null,"userId":"u","authorizationRequired":false}}`)
					return
				}
				var b map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
					t.Fatal(err)
				}
				// SDK emits null for the unused provider. The selected block must be exclusive.
				other := "githubIntegration"
				if provider == other {
					other = "gitlabIntegration"
				}
				if string(b[other]) != "null" && len(b[other]) != 0 {
					t.Fatal("both providers sent")
				}
				var g map[string]any
				if err := json.Unmarshal(b[provider], &g); err != nil {
					t.Fatal(err)
				}
				if g["triggerBuild"] != false {
					t.Fatal("build triggered")
				}
				if _, ok := g["userId"]; ok {
					t.Fatal("computed metadata sent")
				}
				respond(w, 200, `{}`)
			})
			g := &GitIntegration{RepositoryFullName: "owner/repo", EventType: "BRANCH", IsActive: true}
			v := GitIntegrationStatus{}
			if provider == "githubIntegration" {
				v.GithubIntegration = g
			} else {
				v.GitlabIntegration = g
			}
			p, err := c.SetGitIntegration(context.Background(), "s", v)
			if err != nil || p != nil {
				t.Fatalf("nullable process: %v %v", p, err)
			}
			got, err := c.GetGitIntegration(context.Background(), "s")
			if err != nil || (got.GithubIntegration == nil && got.GitlabIntegration == nil) {
				t.Fatalf("readback: %v", err)
			}
		})
	}
}
func TestGitIntegrationAbsenceAndOAuth(t *testing.T) {
	for _, code := range []string{"noExternalRepositoryIntegration", "githubAuthorizationRequired", "gitlabAuthorizationRequired"} {
		t.Run(code, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				respond(w, 400, `{"error":{"code":"`+code+`","message":"secret-token"}}`)
			})
			_, err := c.GetGitIntegration(context.Background(), "s")
			if IsNotFound(err, "noExternalRepositoryIntegration") != (code == "noExternalRepositoryIntegration") {
				t.Fatal("OAuth classified as absence")
			}
		})
	}
}
func TestGitIntegrationReset(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		for _, v := range b {
			if v != nil {
				t.Fatal("reset retained configuration")
			}
		}
		respond(w, 200, `{}`)
	})
	if _, err := c.SetGitIntegration(context.Background(), "s", GitIntegrationStatus{}); err != nil {
		t.Fatal(err)
	}
}
