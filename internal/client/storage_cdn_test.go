package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestStorageCDNAdapter(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disable", true: "enable"}[enabled], func(t *testing.T) {
			calls := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				method := "DELETE"
				if enabled {
					method = "PUT"
				}
				if r.Method != method || r.URL.Path != "/api/rest/public/service-stack/service/cdn" {
					t.Fatal(r.Method, r.URL.Path)
				}
				b, _ := io.ReadAll(r.Body)
				if len(b) != 0 {
					t.Fatalf("CDN must be bodyless: %q", b)
				}
				respond(w, 200, `{"id":"cdn","status":"PENDING"}`)
			})
			p, e := c.SetStorageCDN(context.Background(), "service", enabled)
			if e != nil || p.ID != "cdn" || calls != 1 {
				t.Fatal(p, e, calls)
			}
		})
	}
}
func TestStorageSizePreservesCDN(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("bad JSON")
		}
		if value, ok := body["cdnEnabled"]; ok && string(value) != "null" {
			t.Fatal("size update must not toggle CDN", string(value))
		}
		respond(w, 200, `{"id":"size","status":"FINISHED"}`)
	})
	if _, e := c.UpdateObjectStorage(context.Background(), "service", StorageWrite{DiskGBytes: 2}); e != nil {
		t.Fatal(e)
	}
}
func TestStorageCDNReadMapping(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { respond(w, 200, `{"id":"service","cdnEnabled":true}`) })
	s, e := c.GetService(context.Background(), "service")
	if e != nil || s.CDNEnabled == nil || !*s.CDNEnabled {
		t.Fatal(s, e)
	}
}
