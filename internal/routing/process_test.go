package routing

import (
	"context"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
)

type syncAPI struct {
	client.API
	err   error
	polls int
}

func (f *syncAPI) SyncHTTPRoutes(context.Context, string) (client.Process, error) {
	return client.Process{ID: "sync"}, f.err
}
func (f *syncAPI) SyncPortRoutes(context.Context, string) (client.Process, error) {
	return client.Process{ID: "sync"}, f.err
}
func (f *syncAPI) GetProcess(context.Context, string) (client.Process, error) {
	f.polls++
	return client.Process{ID: "sync", Status: "FINISHED"}, nil
}
func TestSyncProcessAndNothingToSync(t *testing.T) {
	for _, http := range []bool{true, false} {
		api := &syncAPI{}
		if err := Sync(context.Background(), api, "project", http); err != nil || api.polls != 1 {
			t.Fatalf("must wait returned process: %v polls=%d", err, api.polls)
		}
		api = &syncAPI{err: &client.APIError{HTTPStatus: 400, Code: "nothingToSync"}}
		if err := Sync(context.Background(), api, "project", http); err != nil || api.polls != 0 {
			t.Fatalf("no-op must proceed to caller verification without polling: %v", err)
		}
		api = &syncAPI{err: &client.APIError{HTTPStatus: 400, Code: "invalidUserInput"}}
		if err := Sync(context.Background(), api, "project", http); err == nil {
			t.Fatal("non-no-op sync error suppressed")
		}
	}
}
