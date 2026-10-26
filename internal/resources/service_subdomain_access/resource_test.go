package service_subdomain_access

import (
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"reflect"
	"testing"
)

func TestGeneratedURLs(t *testing.T) {
	target := []client.HTTPLocation{{ServiceStackID: "service"}}
	tests := []struct {
		name   string
		routes []client.HTTPRoute
		want   []string
	}{
		{"empty", nil, []string{}},
		{"generated sorted unique", []client.HTTPRoute{
			{Domains: []client.HTTPDomain{{DomainName: "b.example"}, {DomainName: "a.example"}, {DomainName: ""}}, Locations: target},
			{Domains: []client.HTTPDomain{{DomainName: "a.example"}}, Locations: target},
		}, []string{"https://a.example", "https://b.example"}},
		{"editable excluded", []client.HTTPRoute{{IsEditable: true, Domains: []client.HTTPDomain{{DomainName: "custom.example"}}, Locations: target}}, []string{}},
		{"other service excluded", []client.HTTPRoute{{Domains: []client.HTTPDomain{{DomainName: "other.example"}}, Locations: []client.HTTPLocation{{ServiceStackID: "other"}}}}, []string{}},
		{"any matching location", []client.HTTPRoute{{Domains: []client.HTTPDomain{{DomainName: "app.example"}}, Locations: []client.HTTPLocation{{ServiceStackID: "other"}, {ServiceStackID: "service"}}}}, []string{"https://app.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generatedURLs(tt.routes, "service"); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v; want %v", got, tt.want)
			}
		})
	}
}
