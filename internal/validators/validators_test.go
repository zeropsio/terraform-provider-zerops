package validators

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"math/big"
	"testing"
)

func TestStrings(t *testing.T) {
	cases := []struct {
		v         validator.String
		good, bad []string
	}{
		{Hostname(), []string{"a", "node22", "abcdefghijklmnopqrstuvwxyz12345678901234"}, []string{"core", "l7httpbalancer", "Bad", "a-b", "1a", "abcdefghijklmnopqrstuvwxyz123456789012345"}},
		{EnvKey(), []string{"_A", "hello1"}, []string{"ZEROPS_A", "CORE_ZEROPS_A", "zerops_A", "1a", "A-B"}},
		{ServiceType(), []string{"nodejs@22", "ubuntu/nodejs@22", "postgresql:ha@16"}, []string{"Nodejs", "nodejs@", "a/b/c"}},
		{PositiveDecimalString(), []string{"1", "0.01", "001.10"}, []string{"0", "0.000", "-1", "1e2", "+1", "NaN", ".1"}},
		{CIDROrIP(), []string{"127.0.0.1", "::1", "192.0.2.0/24", "2001:db8::/32"}, []string{"bad", "1.2.3.4/33"}},
		{Domain(), []string{"example.com", "*.example.com", "a-b.example"}, []string{"*example.com", "a..com", "-bad.com", "localhost", "a.*.com"}},
		{GitBranch(), []string{"main", "feature/topic", "v1.2"}, []string{"", "refs/heads/main", "a..b", "a.lock", "a/.b", "a/", "a b", "@", "a@{b", "-main", "0123456789012345678901234567890123456789"}},
	}
	for _, tc := range cases {
		for _, good := range tc.good {
			var out validator.StringResponse
			tc.v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("test"), ConfigValue: types.StringValue(good)}, &out)
			if out.Diagnostics.HasError() {
				t.Errorf("rejected %q: %v", good, out.Diagnostics)
			}
		}
		for _, bad := range tc.bad {
			var out validator.StringResponse
			tc.v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(bad)}, &out)
			if !out.Diagnostics.HasError() {
				t.Errorf("accepted %q", bad)
			}
		}
		for _, v := range []types.String{types.StringNull(), types.StringUnknown()} {
			var out validator.StringResponse
			tc.v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: v}, &out)
			if out.Diagnostics.HasError() {
				t.Fatal(out.Diagnostics)
			}
		}
	}
}
func TestPorts(t *testing.T) {
	for _, n := range []int64{9, 10, 80, 443, 65435, 65436} {
		var out validator.Int64Response
		PublicPort().ValidateInt64(context.Background(), validator.Int64Request{ConfigValue: types.Int64Value(n)}, &out)
		want := n < 10 || n > 65435 || n == 80 || n == 443
		if out.Diagnostics.HasError() != want {
			t.Errorf("port %d", n)
		}
	}
}
func TestPrecision(t *testing.T) {
	for _, s := range []string{"0", "0.1", "1.001", "1.0001", "0.0625", "123.999"} {
		f, _, err := big.ParseFloat(s, 10, 512, big.ToNearestEven)
		if err != nil {
			t.Fatal(err)
		}
		var out validator.NumberResponse
		Precision().ValidateNumber(context.Background(), validator.NumberRequest{ConfigValue: types.NumberValue(f)}, &out)
		bad := s == "1.0001" || s == "0.0625"
		if out.Diagnostics.HasError() != bad {
			t.Errorf("precision %s: %v", s, out.Diagnostics)
		}
	}
}
func TestObjects(t *testing.T) {
	for _, tc := range []struct {
		lo, hi        types.Int64
		empty, bounds bool
	}{{types.Int64Null(), types.Int64Null(), true, false}, {types.Int64Unknown(), types.Int64Null(), false, false}, {types.Int64Value(2), types.Int64Value(1), false, true}, {types.Int64Value(1), types.Int64Value(2), false, false}} {
		obj := types.ObjectValueMust(map[string]attr.Type{"min": types.Int64Type, "max": types.Int64Type}, map[string]attr.Value{"min": tc.lo, "max": tc.hi})
		var out validator.ObjectResponse
		NonEmptyObject().ValidateObject(context.Background(), validator.ObjectRequest{ConfigValue: obj}, &out)
		if out.Diagnostics.HasError() != tc.empty {
			t.Error("empty validation", out.Diagnostics)
		}
		out = validator.ObjectResponse{}
		MinMax("min", "max").ValidateObject(context.Background(), validator.ObjectRequest{ConfigValue: obj}, &out)
		if out.Diagnostics.HasError() != tc.bounds {
			t.Error("bounds validation", out.Diagnostics)
		}
	}
}
