package service

import (
	"context"
	"math"
	"math/big"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	v "github.com/zeropsio/terraform-provider-zerops/internal/validators"
)

var verticalNumbers = []string{"min_cpu", "max_cpu", "min_ram_gb", "max_ram_gb", "min_disk_gb", "max_disk_gb", "start_cpu_core_count", "min_free_cpu_cores", "min_free_cpu_percent", "min_free_ram_gb", "min_free_ram_percent"}

func ScalingTypes(vertical bool) map[string]attr.Type {
	a := map[string]attr.Type{}
	if vertical {
		for _, k := range verticalNumbers {
			a[k] = types.NumberType
		}
		for _, k := range []string{"min_cpu", "max_cpu", "start_cpu_core_count"} {
			a[k] = types.Int64Type
		}
		a["cpu_mode"] = types.StringType
		a["swap_enabled"] = types.BoolType
	} else {
		a["min_containers"] = types.Int64Type
		a["max_containers"] = types.Int64Type
	}
	return a
}
func ScalingSchema(vertical, computed bool) schema.SingleNestedAttribute {
	a := map[string]schema.Attribute{}
	if vertical {
		for _, k := range verticalNumbers {
			if k == "min_cpu" || k == "max_cpu" || k == "start_cpu_core_count" {
				var vs []validator.Int64
				if !computed {
					vs = []validator.Int64{int64validator.AtLeast(1)}
				}
				a[k] = schema.Int64Attribute{Optional: !computed, Computed: computed, MarkdownDescription: "Autoscaling " + k + " integer CPU count.", Validators: vs}
				continue
			}
			min := 0.0
			strict := false
			switch k {
			case "min_cpu", "max_cpu", "min_ram_gb", "max_ram_gb", "start_cpu_core_count":
				strict = true
			case "min_disk_gb", "max_disk_gb":
				min = 1
			}
			max := math.Inf(1)
			if k == "min_free_cpu_percent" || k == "min_free_ram_percent" {
				max = 100
			}
			vs := []validator.Number{v.Precision(), numberBounds{min: min, max: max, strict: strict}}
			if computed {
				vs = nil
			}
			disk := k == "min_disk_gb" || k == "max_disk_gb"
			ram := k == "min_ram_gb" || k == "max_ram_gb"
			description := "Autoscaling " + k + " setting."
			if disk && !computed {
				description += " PostgreSQL defaults and bounds respect the provider postgresql_min_disk_gb floor."
			}
			if ram && !computed {
				description += " ClickHouse and Elasticsearch defaults and bounds respect their provider RAM floors (ZEROPS_CLICKHOUSE_MIN_RAM_GB and ZEROPS_ELASTICSEARCH_MIN_RAM_GB)."
			}
			a[k] = schema.NumberAttribute{Optional: !computed, Computed: computed || disk || ram, MarkdownDescription: description, Validators: vs}
		}
		var sv []validator.String
		if !computed {
			sv = []validator.String{stringvalidator.OneOf("SHARED", "DEDICATED")}
		}
		a["cpu_mode"] = schema.StringAttribute{Optional: !computed, Computed: computed, MarkdownDescription: "CPU allocation mode. Mode changes are subject to the server cooldown.", Validators: sv}
		a["swap_enabled"] = schema.BoolAttribute{Optional: !computed, Computed: computed, MarkdownDescription: "Whether swap is enabled."}
	} else {
		for _, k := range []string{"min_containers", "max_containers"} {
			var vs []validator.Int64
			if !computed {
				vs = []validator.Int64{int64validator.Between(1, 10)}
			}
			a[k] = schema.Int64Attribute{Optional: !computed, Computed: computed, MarkdownDescription: "Autoscaling " + k + " setting.", Validators: vs}
		}
	}
	var vs []validator.Object
	if !computed {
		vs = []validator.Object{v.NonEmptyObject()}
		if vertical {
			for _, p := range [][2]string{{"min_cpu", "max_cpu"}, {"min_ram_gb", "max_ram_gb"}, {"min_disk_gb", "max_disk_gb"}} {
				vs = append(vs, v.MinMax(p[0], p[1]))
			}
		} else {
			vs = append(vs, v.MinMax("min_containers", "max_containers"))
		}
	}
	description := "Custom autoscaling overrides; omitted values use server defaults."
	if computed {
		description = "Effective autoscaling settings, including server defaults."
	}
	return schema.SingleNestedAttribute{Optional: !computed, Computed: computed || vertical, MarkdownDescription: description, Attributes: a, Validators: vs}
}
func CurrentSchema() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Effective server autoscaling, including defaults.", Attributes: map[string]schema.Attribute{"vertical_autoscaling": ScalingSchema(true, true), "horizontal_autoscaling": ScalingSchema(false, true)}}
}
func nullScaling(vertical bool) types.Object { return types.ObjectNull(ScalingTypes(vertical)) }
func nval(p *float64, round bool) types.Number {
	if p == nil {
		return types.NumberNull()
	}
	x := *p
	if round {
		x = math.Round(x*1000) / 1000
	}
	// Terraform numbers use 512-bit decimal parsing. Widening a float64 preserves
	// its binary artifact and is not equal to a configured decimal such as 0.1.
	n, _, _ := big.ParseFloat(strconv.FormatFloat(x, 'f', -1, 64), 10, 512, big.ToNearestEven)
	return types.NumberValue(n)
}
func ival(p *int) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*p))
}
func ScalingValues(a *client.Autoscaling, round bool) (types.Object, types.Object) {
	va, ha := map[string]attr.Value{}, map[string]attr.Value{}
	for k, t := range ScalingTypes(true) {
		switch t {
		case types.Int64Type:
			va[k] = types.Int64Null()
		case types.NumberType:
			va[k] = types.NumberNull()
		case types.StringType:
			va[k] = types.StringNull()
		case types.BoolType:
			va[k] = types.BoolNull()
		}
	}
	ha["min_containers"] = types.Int64Null()
	ha["max_containers"] = types.Int64Null()
	if a == nil {
		return nullScaling(true), nullScaling(false)
	}
	if z := a.Vertical; z != nil {
		if z.CPUMode != nil {
			va["cpu_mode"] = types.StringValue(*z.CPUMode)
		}
		if z.SwapEnabled != nil {
			va["swap_enabled"] = types.BoolValue(*z.SwapEnabled)
		}
		va["start_cpu_core_count"] = ival(z.StartCPUCoreCount)
		if z.MinResource != nil {
			va["min_cpu"] = ival(z.MinResource.CPUCoreCount)
			va["min_ram_gb"] = nval(z.MinResource.MemoryGBytes, round)
			va["min_disk_gb"] = nval(z.MinResource.DiskGBytes, round)
		}
		if z.MaxResource != nil {
			va["max_cpu"] = ival(z.MaxResource.CPUCoreCount)
			va["max_ram_gb"] = nval(z.MaxResource.MemoryGBytes, round)
			va["max_disk_gb"] = nval(z.MaxResource.DiskGBytes, round)
		}
		if z.MinFreeResource != nil {
			va["min_free_cpu_cores"] = nval(z.MinFreeResource.CPUCoreCount, round)
			va["min_free_cpu_percent"] = nval(z.MinFreeResource.CPUCorePercent, round)
			va["min_free_ram_gb"] = nval(z.MinFreeResource.MemoryGBytes, round)
			va["min_free_ram_percent"] = nval(z.MinFreeResource.MemoryPercent, round)
		}
	}
	if z := a.Horizontal; z != nil {
		if z.MinContainerCount != nil {
			ha["min_containers"] = types.Int64Value(int64(*z.MinContainerCount))
		}
		if z.MaxContainerCount != nil {
			ha["max_containers"] = types.Int64Value(int64(*z.MaxContainerCount))
		}
	}
	vo := types.ObjectValueMust(ScalingTypes(true), va)
	ho := types.ObjectValueMust(ScalingTypes(false), ha)
	if allNull(vo) {
		vo = nullScaling(true)
	}
	if allNull(ho) {
		ho = nullScaling(false)
	}
	return vo, ho
}
func allNull(o types.Object) bool {
	for _, v := range o.Attributes() {
		if !v.IsNull() {
			return false
		}
	}
	return true
}
func CurrentValue(a *client.Autoscaling) types.Object {
	v, h := ScalingValues(a, false)
	return types.ObjectValueMust(map[string]attr.Type{"vertical_autoscaling": types.ObjectType{AttrTypes: ScalingTypes(true)}, "horizontal_autoscaling": types.ObjectType{AttrTypes: ScalingTypes(false)}}, map[string]attr.Value{"vertical_autoscaling": v, "horizontal_autoscaling": h})
}
func scalingInput(v, h types.Object) (*client.Autoscaling, error) {
	a := &client.Autoscaling{}
	if !v.IsNull() {
		m := v.Attributes()
		f := func(k string) *float64 {
			n := m[k].(types.Number)
			if n.IsNull() {
				return nil
			}
			x, _ := n.ValueBigFloat().Float64()
			return &x
		}
		i := func(k string) *int {
			n := m[k].(types.Int64)
			if n.IsNull() {
				return nil
			}
			x := int(n.ValueInt64())
			return &x
		}
		min, max, start := i("min_cpu"), i("max_cpu"), i("start_cpu_core_count")
		z := &client.VerticalAutoscaling{MinResource: &client.ScalingResource{CPUCoreCount: min, MemoryGBytes: f("min_ram_gb"), DiskGBytes: f("min_disk_gb")}, MaxResource: &client.ScalingResource{CPUCoreCount: max, MemoryGBytes: f("max_ram_gb"), DiskGBytes: f("max_disk_gb")}, MinFreeResource: &client.ScalingMinFreeResource{CPUCoreCount: f("min_free_cpu_cores"), CPUCorePercent: f("min_free_cpu_percent"), MemoryGBytes: f("min_free_ram_gb"), MemoryPercent: f("min_free_ram_percent")}, StartCPUCoreCount: start}
		if s := m["cpu_mode"].(types.String); !s.IsNull() {
			z.CPUMode = s.ValueStringPointer()
		}
		if b := m["swap_enabled"].(types.Bool); !b.IsNull() {
			z.SwapEnabled = b.ValueBoolPointer()
		}
		a.Vertical = z
	}
	if !h.IsNull() {
		m := h.Attributes()
		z := &client.HorizontalAutoscaling{}
		if n := m["min_containers"].(types.Int64); !n.IsNull() {
			x := int(n.ValueInt64())
			z.MinContainerCount = &x
		}
		if n := m["max_containers"].(types.Int64); !n.IsNull() {
			x := int(n.ValueInt64())
			z.MaxContainerCount = &x
		}
		a.Horizontal = z
	}
	return a, nil
}
func scalingMismatch(ctx context.Context, wantV, wantH types.Object, a *client.Autoscaling) diag.Diagnostics {
	var d diag.Diagnostics
	v, h := ScalingValues(a, true)
	if !wantV.Equal(v) || !wantH.Equal(h) {
		d.AddError("Autoscaling readback mismatch", "The server customAutoscaling did not match the requested full replacement. Effective defaults are not adopted as custom overrides.")
	}
	return d
}

type numberBounds struct {
	min, max float64
	strict   bool
}

func (v numberBounds) Description(context.Context) string {
	return "must be within the documented autoscaling bounds"
}
func (v numberBounds) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v numberBounds) ValidateNumber(ctx context.Context, q validator.NumberRequest, p *validator.NumberResponse) {
	if q.ConfigValue.IsNull() || q.ConfigValue.IsUnknown() {
		return
	}
	n := q.ConfigValue.ValueBigFloat()
	low := n.Cmp(big.NewFloat(v.min))
	if low < 0 || (v.strict && low == 0) || (!math.IsInf(v.max, 1) && n.Cmp(big.NewFloat(v.max)) > 0) {
		p.Diagnostics.AddAttributeError(q.Path, "Invalid autoscaling bound", v.Description(ctx))
	}
}
