package projects

import (
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"reflect"
	"testing"
)

func TestFilterProjects(t *testing.T) {
	input := []client.Project{{ID: "z", Name: "app-two", Status: "ACTIVE"}, {ID: "a", Name: "app-one", Status: "STOPPED"}, {ID: "b", Name: "other", Status: "ACTIVE"}}
	for _, tc := range []struct {
		name, contains string
		statuses       []string
		want           []string
	}{{"unfiltered", "", nil, []string{"a", "b", "z"}}, {"name", "app", nil, []string{"a", "z"}}, {"status", "", []string{"ACTIVE"}, []string{"b", "z"}}, {"combined", "app", []string{"ACTIVE"}, []string{"z"}}, {"case-sensitive", "APP", nil, []string{}}, {"unknown-status", "", []string{"FUTURE"}, []string{}}} {
		t.Run(tc.name, func(t *testing.T) {
			got := []string{}
			for _, p := range filterProjects(input, tc.contains, tc.statuses) {
				got = append(got, p.ID)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	if input[0].ID != "z" {
		t.Fatal("filter mutated API order")
	}
}
