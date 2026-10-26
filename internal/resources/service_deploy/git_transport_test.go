package service_deploy

import (
	"context"
	"fmt"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeGitAuthenticatedOperations(t *testing.T) {
	const token = "private-runtime-value"
	t.Setenv("ZEROPS_GIT_TOKEN", token)
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, password, ok := r.BasicAuth()
		if !ok || password != token {
			t.Error("missing runtime Git authentication")
		}
		if strings.Contains(r.URL.String(), token) {
			t.Error("credential in URL")
		}
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "unsafe server echo "+token)
	}))
	defer server.Close()
	old := gitclient.Protocols["https"]
	gitclient.InstallProtocol("https", githttp.NewClient(server.Client()))
	defer gitclient.InstallProtocol("https", old)
	credential := runtimeGitToken("provider", types.StringNull())
	_, err := resolveGit(context.Background(), server.URL+"/repo", "main", "provider", credential)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatal("unsafe reference diagnostic")
	}
	before := requests
	err = materializeGit(context.Background(), t.TempDir(), server.URL+"/repo", strings.Repeat("a", 40), runtimeGitToken("provider", types.StringNull()))
	if err == nil || strings.Contains(err.Error(), token) || requests <= before {
		t.Fatal("exact commit fetch did not authenticate safely")
	}
}
func TestServerGitIgnoresRuntimeCredential(t *testing.T) {
	t.Setenv("ZEROPS_GIT_TOKEN", "must-not-send")
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "" {
			t.Error("runtime Git credential sent in server mode")
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	old := gitclient.Protocols["https"]
	gitclient.InstallProtocol("https", githttp.NewClient(server.Client()))
	defer gitclient.InstallProtocol("https", old)
	_, _ = resolveGit(context.Background(), server.URL+"/repo", "main", "server", runtimeGitToken("server", types.StringNull()))
	if requests == 0 {
		t.Fatal("server public-ref check not exercised")
	}
}

func TestRuntimeGitCredentialStaysAtRepositoryOrigin(t *testing.T) {
	targetRequests := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		if r.Header.Get("Authorization") != "" {
			t.Error("Git credential followed a cross-origin redirect")
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer target.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	old := gitclient.Protocols["https"]
	gitclient.InstallProtocol("https", githttp.NewClient(origin.Client()))
	defer gitclient.InstallProtocol("https", old)
	_, _ = resolveGit(context.Background(), origin.URL+"/repo", "main", "provider", "origin-only-secret")
	if targetRequests == 0 {
		t.Fatal("redirect destination was not exercised")
	}
}
