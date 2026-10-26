package client

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func TestRequestDedicatedIPv4(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"direct process", `{"id":"process","status":"PENDING","actionName":"instance.publicIpV4.request"}`, 200, ""},
		{"first top-up", `{"error":{"code":"publicIpRequestForbidden","message":"Public IP address is only available after client's first account top-up"}}`, 400, "publicIpRequestForbidden"},
		{"conflict", `{"error":{"code":"publicIpRequestOnlyOne"}}`, 400, "publicIpRequestOnlyOne"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "PUT" || r.URL.Path != "/api/rest/public/project/project/request-ipv4" {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				b, _ := io.ReadAll(r.Body)
				if len(b) != 0 {
					t.Fatalf("expected bodyless request: %q", b)
				}
				respond(w, tc.status, tc.body)
			})
			p, err := c.RequestDedicatedIPv4(context.Background(), "project")
			if tc.code != "" {
				e, ok := AsAPIError(err)
				if !ok || e.Code != tc.code || e.HTTPStatus != 400 {
					t.Fatalf("wrong error %v", err)
				}
			} else if err != nil || p.ID != "process" {
				t.Fatalf("process=%+v error=%v", p, err)
			}
			if calls != 1 {
				t.Fatalf("mutation replayed %d times", calls)
			}
		})
	}
}

func TestDedicatedIPv4ProjectMapping(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, `{"id":"project","publicIpV4":"192.0.2.8"}`)
	})
	p, err := c.GetProject(context.Background(), "project")
	if err != nil || p.PublicIPv4 == nil || *p.PublicIPv4 != "192.0.2.8" {
		t.Fatalf("project=%+v error=%v", p, err)
	}
}
