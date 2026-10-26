package waiter

import (
	"context"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
	"time"
)

type gitReader func() (client.GitIntegrationStatus, error)

func (f gitReader) GetGitIntegration(context.Context, string) (client.GitIntegrationStatus, error) {
	return f()
}
func TestWaitGitIntegration(t *testing.T) {
	want := client.GitIntegrationStatus{GithubIntegration: &client.GitIntegration{RepositoryFullName: "owner/repo", EventType: "BRANCH", IsActive: true}}
	calls := 0
	reader := gitReader(func() (client.GitIntegrationStatus, error) {
		calls++
		if calls == 1 {
			return client.GitIntegrationStatus{}, &client.APIError{HTTPStatus: 400, Code: "noExternalRepositoryIntegration"}
		}
		return want, nil
	})
	clock := &fakeClock{limit: time.Minute}
	if _, err := WaitGitIntegration(context.Background(), reader, "s", &want, WithClock(clock)); err != nil || calls != 2 {
		t.Fatalf("calls %d: %v", calls, err)
	}
	for _, code := range []string{"githubAuthorizationRequired", "gitlabAuthorizationRequired"} {
		reader = func() (client.GitIntegrationStatus, error) {
			return client.GitIntegrationStatus{}, &client.APIError{HTTPStatus: 400, Code: code}
		}
		if _, err := WaitGitIntegration(context.Background(), reader, "s", nil, WithClock(clock)); err == nil {
			t.Fatal("OAuth became absence")
		}
	}
	reader = func() (client.GitIntegrationStatus, error) {
		return client.GitIntegrationStatus{}, &client.APIError{HTTPStatus: 400, Code: "noExternalRepositoryIntegration"}
	}
	if _, err := WaitGitIntegration(context.Background(), reader, "s", nil); err != nil {
		t.Fatal(err)
	}
}
