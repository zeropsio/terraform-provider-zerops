package waiter

import (
	"context"
	"fmt"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"reflect"
	"time"
)

type GitIntegrationAPI interface {
	GetGitIntegration(context.Context, string) (client.GitIntegrationStatus, error)
}

func WaitGitIntegration(ctx context.Context, api GitIntegrationAPI, id string, want *client.GitIntegrationStatus, opts ...Option) (client.GitIntegrationStatus, error) {
	p := newPoll(opts)
	for {
		got, err := api.GetGitIntegration(ctx, id)
		absent := client.IsNotFound(err, "noExternalRepositoryIntegration", "serviceStackNotFound")
		if absent && want == nil {
			return client.GitIntegrationStatus{}, nil
		}
		if err == nil {
			_ = p.readError(nil)
			if (got.GithubIntegration == nil) == (got.GitlabIntegration == nil) {
				return got, fmt.Errorf("invalid Git integration status: expected exactly one provider")
			}
			if want != nil && gitMetadataEqual(got, *want) {
				return got, nil
			}
		} else if !absent {
			if e := p.readError(err); e != nil {
				return got, e
			}
		} else {
			_ = p.readError(nil)
		}
		if e := p.sleep(ctx, 5*time.Second); e != nil {
			return got, fmt.Errorf("waiting for Git integration on service %s: %w", id, e)
		}
	}
}
func gitMetadataEqual(a, b client.GitIntegrationStatus) bool {
	clean := func(v *client.GitIntegration) *client.GitIntegration {
		if v == nil {
			return nil
		}
		c := *v
		c.UserID = ""
		c.AuthorizationRequired = false
		c.WebhookID = nil
		return &c
	}
	return reflect.DeepEqual(clean(a.GithubIntegration), clean(b.GithubIntegration)) && reflect.DeepEqual(clean(a.GitlabIntegration), clean(b.GitlabIntegration))
}
