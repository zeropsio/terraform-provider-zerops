package service_deploy

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

var fullSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// Runtime credentials are resolved at each operation, never copied into a model.
func runtimeGitToken(mode string, explicit types.String) string {
	if mode != "provider" || explicit.IsUnknown() {
		return ""
	}
	if !explicit.IsNull() {
		return explicit.ValueString()
	}
	return os.Getenv("ZEROPS_GIT_TOKEN")
}

func gitAuth(token string) transport.AuthMethod {
	if token == "" {
		return nil
	}
	return &http.BasicAuth{Username: "token", Password: token}
}
func resolveGit(ctx context.Context, rawURL, ref, mode, token string) (string, error) {
	u, e := url.Parse(rawURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("git.url must be HTTPS without embedded credentials, query or fragment")
	}
	if mode != "provider" && mode != "server" {
		return "", fmt.Errorf("git.mode must be provider or server")
	}
	if mode == "provider" && fullSHA.MatchString(ref) {
		return strings.ToLower(ref), nil
	}
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{rawURL}})
	refs, e := remote.ListContext(ctx, &git.ListOptions{Auth: gitAuth(token), PeelingOption: git.AppendPeeled})
	if e != nil {
		return "", fmt.Errorf("unable to list Git remote references (check repository access and credentials)")
	}
	heads := map[string]string{}
	for _, r := range refs {
		heads[r.Name().String()] = r.Hash().String()
	}
	branch := strings.TrimPrefix(ref, "refs/heads/")
	if mode == "server" {
		if ref != branch || fullSHA.MatchString(ref) {
			return "", serverRefError()
		}
		if h, ok := heads["refs/heads/"+branch]; ok {
			return h, nil
		}
		return "", serverRefError()
	}
	if h, ok := heads["refs/heads/"+branch]; ok {
		return h, nil
	}
	tag := strings.TrimPrefix(ref, "refs/tags/")
	if h, ok := heads["refs/tags/"+tag+"^{}"]; ok {
		return h, nil
	}
	if h, ok := heads["refs/tags/"+tag]; ok {
		return h, nil
	}
	return "", fmt.Errorf("Git ref does not name an advertised branch or tag; use a full commit SHA for an exact commit")
}
func serverRefError() error {
	return fmt.Errorf("server Git mode requires an existing branch, not a tag or SHA: the server silently falls back to main/master for unsupported refs; use provider mode for pinned commits")
}
func materializeGit(ctx context.Context, dir, rawURL, commit, token string) error {
	repo, e := git.PlainInit(dir, false)
	if e != nil {
		return fmt.Errorf("initialize Git checkout: %w", e)
	}
	_, e = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{rawURL}})
	if e != nil {
		return fmt.Errorf("initialize Git remote")
	}
	e = repo.FetchContext(ctx, &git.FetchOptions{RemoteName: "origin", Depth: 1, Auth: gitAuth(token), RefSpecs: []config.RefSpec{config.RefSpec("+" + commit + ":refs/terraform/source")}, Tags: git.NoTags})
	if e != nil && e != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("unable to fetch exact planned Git commit %s; no branch fallback is permitted", commit)
	}
	wt, e := repo.Worktree()
	if e != nil {
		return e
	}
	if e = wt.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(commit), Force: true}); e != nil {
		return fmt.Errorf("unable to check out exact planned Git commit %s", commit)
	}
	return nil
}
