package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"testing"
)

func TestIdentityAndRegistry(t *testing.T) {
	reg := Registry{Resources: []func() resource.Resource{func() resource.Resource { return nil }}, DataSources: []func() datasource.DataSource{func() datasource.DataSource { return nil }}}
	p := New("1.2.3", reg)()
	var metadata framework.MetadataResponse
	p.Metadata(context.Background(), framework.MetadataRequest{}, &metadata)
	if metadata.TypeName != "zerops" || metadata.Version != "1.2.3" {
		t.Fatalf("unexpected identity: %#v", metadata)
	}
	if len(p.Resources(context.Background())) != 1 || len(p.DataSources(context.Background())) != 1 {
		t.Fatal("registry constructors not retained")
	}
}
