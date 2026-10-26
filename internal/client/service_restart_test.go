package client

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func TestRestartServiceAdapter(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "PUT" || r.URL.Path != "/api/rest/public/service-stack/service/restart" {
			t.Fatal(r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		if len(b) != 0 {
			t.Fatalf("restart must be bodyless: %q", b)
		}
		respond(w, 200, `{"id":"restart","actionName":"stack.restart","status":"PENDING"}`)
	})
	p, e := c.RestartService(context.Background(), "service")
	if e != nil || p.ID != "restart" || p.ActionName != "stack.restart" || calls != 1 {
		t.Fatal(p, e, calls)
	}
}
func TestRestartUndeployedError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, 400, `{"error":{"code":"serviceStackIsReadyToDeploy"}}`)
	})
	_, e := c.RestartService(context.Background(), "service")
	v, ok := AsAPIError(e)
	if !ok || v.Code != "serviceStackIsReadyToDeploy" {
		t.Fatal(e)
	}
}
