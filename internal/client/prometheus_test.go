package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestPrometheusPUTAndRead(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "prometheus-config"):
			if r.Method != "PUT" {
				t.Fatalf("mutation method %s", r.Method)
			}
			var b struct {
				Configs []PrometheusConfig `json:"configs"`
			}
			if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
				t.Fatal(e)
			}
			if b.Configs == nil {
				t.Fatal("null clear")
			}
			calls++
			respond(w, 200, `{"process":null}`)
		case strings.HasSuffix(r.URL.Path, "/project/p"):
			respond(w, 200, `{"id":"p"}`)
		case strings.HasSuffix(r.URL.Path, "service-stack"):
			if r.URL.Query().Get("offset") == "0" {
				respond(w, 200, `{"list":[{"id":"app"}],"total":2}`)
			} else {
				respond(w, 200, `{"list":[{"id":"core","serviceStackTypeInfo":{"serviceStackTypeCategory":"CORE"}}],"total":2}`)
			}
		case strings.HasSuffix(r.URL.Path, "user-data"):
			if r.URL.Query().Get("offset") == "0" {
				respond(w, 200, `{"list":[{"key":"OTHER","type":"SYSTEM"}],"total":2}`)
			} else {
				respond(w, 200, `{"list":[{"key":"ZEROPS_PROMETHEUS_CUSTOM_SD","type":"SYSTEM","content":"[{\"targets\":[\"127.0.0.1:9100\"],\"labels\":{\"b\":\"2\",\"a\":\"1\"}}]"}],"total":2}`)
			}
		default:
			t.Fatalf("unexpected route %s", r.URL.Path)
		}
	})
	p, e := c.SetPrometheusConfig(context.Background(), "p", nil)
	if e != nil || p != nil || calls != 1 {
		t.Fatalf("p=%v e=%v calls=%d", p, e, calls)
	}
	s, e := ReadPrometheusConfig(context.Background(), c, "p")
	if e != nil {
		t.Fatal(e)
	}
	want := []PrometheusConfig{{Targets: []string{"127.0.0.1:9100"}, Labels: map[string]string{"a": "1", "b": "2"}}}
	if s.CoreServiceID != "core" || !EqualPrometheusConfigs(want, s.Configs) {
		t.Fatalf("unexpected state %+v", s)
	}
	want[0].Targets = append(want[0].Targets, "localhost:9100")
	if EqualPrometheusConfigs(want, s.Configs) {
		t.Fatal("target ordering lost")
	}
}
func TestPrometheusReadFailures(t *testing.T) {
	for _, content := range []string{"null", "REDACTED", "{}", "[{\"labels\":{}}]", "["} {
		t.Run(content, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/project/p"):
					respond(w, 200, `{"id":"p"}`)
				case strings.HasSuffix(r.URL.Path, "service-stack"):
					respond(w, 200, `{"list":[{"id":"core","serviceStackTypeInfo":{"serviceStackTypeCategory":"CORE"}}],"total":1}`)
				default:
					b, _ := json.Marshal(content)
					respond(w, 200, fmt.Sprintf(`{"list":[{"key":"ZEROPS_PROMETHEUS_CUSTOM_SD","type":"SYSTEM","content":%s}],"total":1}`, b))
				}
			})
			if _, e := ReadPrometheusConfig(context.Background(), c, "p"); e == nil {
				t.Fatal("malformed content accepted")
			}
		})
	}
}

func TestPrometheusProcessAndNoReplay(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		respond(w, 200, `{"process":{"id":"process","actionName":"stack.updateUserData","status":"PENDING"}}`)
	})
	p, err := c.SetPrometheusConfig(context.Background(), "p", []PrometheusConfig{})
	if err != nil || p == nil || p.ID != "process" {
		t.Fatalf("process=%v error=%v", p, err)
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		respond(w, 500, `{"error":{"code":"internalServerError"}}`)
	})
	_, err = c.SetPrometheusConfig(context.Background(), "p", nil)
	if err == nil || calls != 2 {
		t.Fatalf("mutation replayed or failure lost: calls=%d error=%v", calls, err)
	}
}
