package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestHTTPRoutingConfigAdapters(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "PUT" {
			t.Fatal("expected PUT")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/api/rest/public/public-http-routing/route/location":
			locations := body["locations"].([]any)
			if len(locations) != 2 {
				t.Fatal("partial replacement")
			}
			config := locations[0].(map[string]any)["config"].(map[string]any)
			if config["basicAuth"] != nil || config["content"] == nil {
				t.Fatal("null clearing or disabled retention lost")
			}
			respond(w, 200, `{"id":"route","locations":[],"domains":[]}`)
		case "/api/rest/public/public-http-routing/route/trigger-domains-dns-check":
			if len(body) != 1 || body["domains"] != nil {
				t.Fatal("DNS check must select all domains")
			}
			respond(w, 200, `{"success":true}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	locations := []HTTPLocation{{Path: "/", Port: 3000, ServiceStackID: "service", Config: map[string]any{"content": map[string]any{"enabled": false, "code": 200, "content": "fixture", "contentType": "text/plain"}}}, {Path: "/other", Port: 3000, ServiceStackID: "service"}}
	if _, err := c.SetHTTPLocations(context.Background(), "route", locations); err != nil {
		t.Fatal(err)
	}
	if err := c.TriggerHTTPDomainsDNSCheck(context.Background(), "route"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("mutations replayed: %d", calls)
	}
}
