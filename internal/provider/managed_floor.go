package provider

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"

	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
)

func managedRAMFloor(ctx context.Context, env string, fallback float64, resp *framework.ConfigureResponse) float64 {
	value, present := os.LookupEnv(env)
	if !present {
		return fallback
	}
	floor, err := strconv.ParseFloat(value, 64)
	if err != nil || !(floor > 0) || math.IsInf(floor, 0) {
		resp.Diagnostics.AddError("Invalid managed RAM floor", fmt.Sprintf("%s must be a finite positive number with at most three decimal places.", env))
		return fallback
	}
	number, _, err := big.ParseFloat(value, 10, 512, big.ToNearestEven)
	if err != nil {
		resp.Diagnostics.AddError("Invalid managed RAM floor", fmt.Sprintf("%s must be a finite positive number with at most three decimal places.", env))
		return fallback
	}
	var precision validator.NumberResponse
	validators.Precision().ValidateNumber(ctx, validator.NumberRequest{ConfigValue: types.NumberValue(number)}, &precision)
	if precision.Diagnostics.HasError() {
		resp.Diagnostics.AddError("Invalid managed RAM floor", fmt.Sprintf("%s must be a finite positive number with at most three decimal places.", env))
		return fallback
	}
	return floor
}
