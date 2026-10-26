package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRemoteLoggingReplacementAndReset(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "PUT" || r.URL.Path != "/api/rest/public/project/p/remote-logging" {
			t.Errorf("unexpected route %s %s", r.Method, r.URL.Path)
		}
		var b map[string]any
		if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
			t.Error(e)
		}
		if len(b) != 3 {
			t.Errorf("not full replacement: %v", b)
		}
		if calls == 1 {
			if b["syslogConfig"] != "secret snippet" || b["certificate"] != "certificate" || b["type"] != "syslog" {
				t.Error("wrong logging payload")
			}
		} else {
			if b["syslogConfig"] != "" || b["certificate"] != "" || b["type"] != nil {
				t.Error("wrong reset payload")
			}
		}
		respond(w, 200, `{"id":"proc","status":"PENDING","actionName":"project.configureRemoteLogging"}`)
	})
	typ := "syslog"
	p, e := c.SetRemoteLogging(context.Background(), "p", RemoteLoggingWrite{SyslogConfig: "secret snippet", Certificate: "certificate", Type: &typ})
	if e != nil || p.ID != "proc" {
		t.Fatalf("direct process: %v %v", p, e)
	}
	if _, e = c.SetRemoteLogging(context.Background(), "p", RemoteLoggingWrite{}); e != nil {
		t.Fatal(e)
	}
}
func TestRemoteLoggingValidationSafe(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, 400, `{"error":{"code":"projectInvalidSyslogFile","message":"secret snippet","meta":{"desc":"certificate"}}}`)
	})
	_, e := c.SetRemoteLogging(context.Background(), "p", RemoteLoggingWrite{SyslogConfig: "secret snippet"})
	if e == nil || strings.Contains(e.Error(), "secret snippet") || strings.Contains(e.Error(), "certificate") {
		t.Fatalf("unsafe error %v", e)
	}
}
