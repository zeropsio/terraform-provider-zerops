package service_deploy

import (
	"context"
	"strings"
	"testing"
)

func TestResolveGitExactSHA(t *testing.T) {
	sha := "4A156A716A3087EA4B4F0B00814B5EA1D3F21590"
	got, e := resolveGit(context.Background(), "https://example.invalid/repo", sha, "provider", "")
	if e != nil || got != strings.ToLower(sha) {
		t.Fatalf("%s %v", got, e)
	}
}
func TestResolveGitRejectsCredentialURL(t *testing.T) {
	for _, u := range []string{"http://example.com/repo", "https://secret@example.com/repo", "https://example.com/repo?token=secret", "https://example.com/repo#main"} {
		_, e := resolveGit(context.Background(), u, strings.Repeat("a", 40), "provider", "")
		if e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatalf("unsafe diagnostic: %v", e)
		}
	}
}
func TestGitErrorsRedactToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := resolveGit(ctx, "https://example.invalid/repo", "main", "provider", "private-value")
	if e == nil || strings.Contains(e.Error(), "private-value") {
		t.Fatalf("unsafe diagnostic %v", e)
	}
}
