package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestL7BalancerWire(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/rest/public/project/p/l7httpbalancer-config" {
			t.Error("wrong route")
		}
		if r.Method == "GET" {
			respond(w, 200, `{"values":[{"name":"a","current":null,"default":"10"},{"name":"z","current":"","default":"20"}]}`)
			return
		}
		var b struct {
			Values []L7BalancerWrite `json:"values"`
		}
		if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
			t.Error(e)
		}
		if len(b.Values) != 2 || b.Values[0].Name != "a" || b.Values[0].Value != nil || b.Values[1].Name != "z" || *b.Values[1].Value != "35" {
			t.Errorf("bad sorted/reset payload %v", b)
		}
		respond(w, 200, `{"id":"proc","actionName":"stack.updateProjectEnvs"}`)
	})
	got, e := c.GetL7BalancerConfig(context.Background(), "p")
	if e != nil || len(got.Values) != 2 || got.Values[0].Current != nil || got.Values[1].Current == nil || *got.Values[1].Current != "" {
		t.Fatalf("null vs empty lost: %v %v", got, e)
	}
	v := "35"
	p, e := c.SetL7BalancerConfig(context.Background(), "p", map[string]*string{"z": &v, "a": nil})
	if e != nil || p.ID != "proc" {
		t.Fatal(p, e)
	}
}
