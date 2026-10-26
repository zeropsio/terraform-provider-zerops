package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestImportProject(t *testing.T) {
	for _, processes := range []string{`[]`, `[{"id":"q","actionName":"stack.create","status":"PENDING"}]`} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.URL.Path != "/api/rest/public/client/org/project/import" {
				t.Errorf("unexpected route %s %s", r.Method, r.URL.Path)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["yaml"] != "secret recipe" {
				t.Error("recipe changed")
			}
			respond(w, 200, `{"projectId":"p","projectName":"name","serviceStacks":[{"id":"s","name":"cache","processes":`+processes+`}]}`)
		})
		value, err := c.ImportProject(context.Background(), "org", "secret recipe")
		if err != nil || value.ProjectID != "p" || len(value.ServiceStacks) != 1 {
			t.Fatalf("unexpected import: %+v %v", value, err)
		}
	}
}
func TestImportProjectErrorsDoNotEchoRecipe(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		respond(w, 400, `{"error":{"code":"projectImportInvalidYaml","message":"secret recipe","meta":{"reason":"secret recipe"}}}`)
	})
	_, err := c.ImportProject(context.Background(), "org", "secret recipe")
	if err == nil || strings.Contains(err.Error(), "secret recipe") || calls != 1 {
		t.Fatalf("unsafe or replayed error: %v calls %d", err, calls)
	}
}
