package project

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"testing"
)

func TestUpdatePayload(t *testing.T) {
	m := Model{Name: types.StringValue("name"), Description: types.StringNull(), Tags: types.SetValueMust(types.StringType, nil), PublicIPv4Shared: types.BoolValue(false), MaxCreditLimit: types.StringValue("25.5")}
	p, e := m.update(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if p.Name != "name" || p.Description != nil || len(p.TagList) != 0 || p.PublicIPv4Shared || p.MaxCreditLimit == nil || *p.MaxCreditLimit != 25.5 {
		t.Fatalf("replacement payload lost desired fields: %+v", p)
	}
	m.MaxCreditLimit = types.StringNull()
	p, e = m.update(context.Background())
	if e != nil || p.MaxCreditLimit != nil {
		t.Fatal("never-known credit must send null")
	}
}
