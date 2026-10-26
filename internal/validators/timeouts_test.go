package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPositiveDuration(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{{"30m", true}, {"1ns", true}, {"0s", false}, {"-1s", false}, {"bad", false}, {"", false}} {
		var response validator.StringResponse
		PositiveDuration().ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(tc.value)}, &response)
		if response.Diagnostics.HasError() == tc.valid {
			t.Errorf("%q: %v", tc.value, response.Diagnostics)
		}
	}
}

func TestTimeoutsBlock(t *testing.T) {
	block := TimeoutsBlock(context.Background(), timeouts.Opts{Create: true, Update: true, Delete: true}).(schema.SingleNestedBlock)
	if block.CustomType == nil || block.MarkdownDescription == "" || len(block.Attributes) != 3 {
		t.Fatal("timeout block contract lost")
	}
	for name, a := range block.Attributes {
		value := a.(schema.StringAttribute)
		if !value.Optional || value.MarkdownDescription == "" {
			t.Errorf("missing optional or description: %s", name)
		}
		var response validator.StringResponse
		for _, v := range value.Validators {
			v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue("0s")}, &response)
		}
		if !response.Diagnostics.HasError() {
			t.Errorf("nonpositive %s accepted", name)
		}
	}
}
