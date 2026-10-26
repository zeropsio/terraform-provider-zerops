package client

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestProjectNameLookupAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ambiguous  bool
	}{{"duplicate exact names", `{"list":[{"id":"a","name":"app"},{"id":"b","name":"app"}],"total":2}`, true}, {"substring is not exact", `{"list":[{"id":"a","name":"app-extra"}],"total":1}`, false}, {"case sensitive", `{"list":[{"id":"a","name":"APP"}],"total":1}`, false}} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("statuses") {
					t.Fatal("lookup sent broken server status filter")
				}
				respond(w, 200, tc.body)
			})
			_, err := c.GetProjectByName(context.Background(), "org", "app", nil)
			if tc.ambiguous {
				if err == nil || !strings.Contains(err.Error(), "multiple projects") {
					t.Fatalf("expected ambiguous error, got %v", err)
				}
			} else if !IsNotFound(err, "projectNotFound") {
				t.Fatalf("expected exact-match not-found, got %v", err)
			}
		})
	}
}
