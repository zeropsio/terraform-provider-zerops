package service_backup_config

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Resource struct{ data *providerdata.ProviderData }
type model struct {
	ID        types.String   `tfsdk:"id"`
	ServiceID types.String   `tfsdk:"service_id"`
	Period    types.String   `tfsdk:"backup_period"`
	Retention types.Object   `tfsdk:"retention_policy"`
	Default   types.Object   `tfsdk:"default_retention_policy"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

var policyNames = []string{"max_total_files", "max_total_gib", "min_daily", "max_daily", "min_weekly", "max_weekly", "min_monthly", "max_monthly"}
var protectedTagPattern = regexp.MustCompile(`^[a-zA-Z0-9:_-]{1,24}$`)

func policyTypes() map[string]attr.Type {
	m := map[string]attr.Type{"protected_tags": types.ListType{ElemType: types.StringType}}
	for _, n := range policyNames {
		m[n] = types.Int64Type
	}
	return m
}
func policySchema(computed bool) map[string]schema.Attribute {
	m := map[string]schema.Attribute{"protected_tags": schema.ListAttribute{Optional: !computed, Computed: computed, ElementType: types.StringType, MarkdownDescription: "Required when retention_policy is present. Each tag is 1–24 ASCII identifier characters; total length at most 256 bytes."}}
	for _, n := range policyNames {
		a := schema.Int64Attribute{Optional: !computed, Computed: computed, MarkdownDescription: "Required when retention_policy is present."}
		if !computed {
			switch n {
			case "max_total_files":
				a.Validators = []validator.Int64{int64validator.Between(0, 100)}
			case "max_total_gib":
				a.Validators = []validator.Int64{int64validator.AtLeast(1)}
			default:
				a.Validators = []validator.Int64{int64validator.AtLeast(0)}
			}
		}
		m[n] = a
	}
	return m
}
func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_backup_config"
}
func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Version: 0, MarkdownDescription: "Owns backup scheduling and custom retention for a backup-capable managed service. Delete disables the schedule and clears the custom retention policy, falling back to server defaults; it never deletes backup files or the service. This resource does not create or restore backups.", Attributes: map[string]schema.Attribute{
		"id":                       schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"service_id":               schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"backup_period":            schema.StringAttribute{Optional: true, MarkdownDescription: "Five-field cron schedule (minute precision); null disables scheduling.", Validators: []validator.String{cronValidator{}}},
		"default_retention_policy": schema.SingleNestedAttribute{Computed: true, Attributes: policySchema(true), MarkdownDescription: "Current server defaults; never hardcoded."},
	}, Blocks: map[string]schema.Block{"retention_policy": schema.SingleNestedBlock{Attributes: policySchema(false)}, "timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func policyValue(p *client.BackupRetentionPolicy) types.Object {
	if p == nil {
		return types.ObjectNull(policyTypes())
	}
	tags := make([]attr.Value, len(p.ProtectedTags))
	for i, s := range p.ProtectedTags {
		tags[i] = types.StringValue(s)
	}
	values := []int64{p.MaxTotalFiles, p.MaxTotalGiB, p.MinDaily, p.MaxDaily, p.MinWeekly, p.MaxWeekly, p.MinMonthly, p.MaxMonthly}
	m := map[string]attr.Value{"protected_tags": types.ListValueMust(types.StringType, tags)}
	for i, n := range policyNames {
		m[n] = types.Int64Value(values[i])
	}
	return types.ObjectValueMust(policyTypes(), m)
}
func policyInput(v types.Object) (*client.BackupRetentionPolicy, bool) {
	if v.IsUnknown() {
		return nil, false
	}
	if v.IsNull() {
		return nil, true
	}
	a := v.Attributes()
	ints := make([]int64, len(policyNames))
	for i, n := range policyNames {
		x := a[n].(types.Int64)
		if x.IsUnknown() || x.IsNull() {
			return nil, false
		}
		ints[i] = x.ValueInt64()
	}
	tags := a["protected_tags"].(types.List)
	if tags.IsUnknown() || tags.IsNull() {
		return nil, false
	}
	out := make([]string, 0, len(tags.Elements()))
	for _, e := range tags.Elements() {
		s := e.(types.String)
		if s.IsUnknown() || s.IsNull() {
			return nil, false
		}
		out = append(out, s.ValueString())
	}
	return &client.BackupRetentionPolicy{MaxTotalFiles: ints[0], MaxTotalGiB: ints[1], MinDaily: ints[2], MaxDaily: ints[3], MinWeekly: ints[4], MaxWeekly: ints[5], MinMonthly: ints[6], MaxMonthly: ints[7], ProtectedTags: out}, true
}
func desired(m model) (client.BackupConfigWrite, bool) {
	p, known := policyInput(m.Retention)
	v := client.BackupConfigWrite{RetentionPolicy: p}
	if m.Period.IsUnknown() || m.ServiceID.IsUnknown() || m.ServiceID.IsNull() {
		return v, false
	}
	if !m.Period.IsNull() {
		s := m.Period.ValueString()
		v.BackupPeriod = &s
	}
	return v, known
}
func mapRead(m *model, v client.BackupConfig) {
	m.Period = types.StringNull()
	if v.BackupPeriod != nil {
		m.Period = types.StringValue(*v.BackupPeriod)
	}
	m.Retention = policyValue(v.RetentionPolicy)
	m.Default = policyValue(v.DefaultRetentionPolicy)
}
func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m model
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Required children on an optional protocol single block are validated even
	// when the block is absent. Enforce conditional requiredness here instead.
	if !m.Retention.IsNull() && !m.Retention.IsUnknown() {
		for name, value := range m.Retention.Attributes() {
			if value.IsNull() {
				resp.Diagnostics.AddAttributeError(path.Root("retention_policy").AtName(name), "Missing retention setting", "Every retention_policy child must be supplied when the block is present.")
			}
		}
		if resp.Diagnostics.HasError() {
			return
		}
	}
	p, known := policyInput(m.Retention)
	if !known || p == nil {
		return
	}
	if p.MinDaily > 100 || p.MinWeekly > 100 || p.MinMonthly > 100 || p.MinDaily+p.MinWeekly+p.MinMonthly > 100 {
		resp.Diagnostics.AddAttributeError(path.Root("retention_policy"), "Invalid retention policy", "The sum of daily, weekly and monthly minima must not exceed 100.")
	}
	total := 0
	for _, s := range p.ProtectedTags {
		total += len(s)
		if !protectedTagPattern.MatchString(s) {
			resp.Diagnostics.AddAttributeError(path.Root("retention_policy").AtName("protected_tags"), "Invalid protected tag", "Tags must contain 1–24 ASCII letters, digits, colons, underscores or hyphens.")
		}
	}
	if total > 256 {
		resp.Diagnostics.AddAttributeError(path.Root("retention_policy").AtName("protected_tags"), "Invalid protected tags", "Total tag length must not exceed 256 bytes.")
	}
}
func absentDuringDelete(err error, deleting bool) bool {
	return deleting && client.IsNotFound(err, "serviceStackNotFound")
}
func (r *Resource) apply(ctx context.Context, m *model, d *diag.Diagnostics, save func(), deleting bool) {
	want, known := desired(*m)
	if !known {
		d.AddError("Unknown backup configuration", "Configuration must be known before applying.")
		return
	}
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ServiceID.ValueString()))
	if err != nil {
		d.AddError("Lock failed", err.Error())
		return
	}
	defer unlock()
	s, err := r.data.API.GetService(ctx, m.ServiceID.ValueString())
	if absentDuringDelete(err, deleting) {
		return
	}
	if err == nil && s.IsSystem {
		d.AddError("Invalid backup service", "System services cannot own backup configuration.")
		return
	}
	if err != nil {
		d.AddError("Reading backup service failed", err.Error())
		return
	}
	p, err := r.data.API.SetBackupConfig(ctx, m.ServiceID.ValueString(), want)
	if absentDuringDelete(err, deleting) {
		return
	}
	if err != nil {
		if api, ok := client.AsAPIError(err); ok && api.Code == "backupUnsupportedStackType" {
			d.AddAttributeError(path.Root("service_id"), "Unsupported backup service", "This service type does not support backup configuration.")
		} else if ok && api.Code == "invalidUserInput" {
			attribute := "backup_period"
			if fields, ok := api.Meta.(map[string]any); ok {
				if _, exists := fields["retentionPolicy"]; exists {
					attribute = "retention_policy"
				}
			}
			d.AddAttributeError(path.Root(attribute), "Invalid backup configuration", "The server rejected the backup schedule or retention policy; review the configured values.")
		} else {
			d.AddError("Writing backup configuration failed", err.Error())
		}
		return
	}
	m.ID = m.ServiceID
	save()
	if p != nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, p.ID, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute))
	}
	if err == nil {
		var v client.BackupConfig
		v, err = waiter.WaitBackupConfig(ctx, r.data.API, m.ServiceID.ValueString(), want)
		if err == nil {
			mapRead(m, v)
			save()
		}
	}
	if err != nil {
		d.AddError("Writing backup configuration failed", err.Error())
	}
}
func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, diags := m.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	r.apply(ctx, &m, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &m)...) }, false)
}
func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, diags := m.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	r.apply(ctx, &m, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &m)...) }, false)
}
func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.data.API.GetBackupConfig(ctx, m.ServiceID.ValueString())
	if client.IsNotFound(err, "serviceStackNotFound") {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading backup configuration failed", err.Error())
		return
	}
	mapRead(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, diags := m.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	m.Period = types.StringNull()
	m.Retention = types.ObjectNull(policyTypes())
	r.apply(ctx, &m, &resp.Diagnostics, func() {}, true)
}
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected service ID.")
		return
	}
	v, err := r.data.API.GetBackupConfig(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Import failed", err.Error())
		return
	}
	m := model{ID: types.StringValue(req.ID), ServiceID: types.StringValue(req.ID), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	mapRead(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

type cronValidator struct{}

func (cronValidator) Description(context.Context) string {
	return "must be a strict five-field cron expression (interval at least one minute)"
}
func (v cronValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v cronValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := validateCron(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid backup period", v.Description(ctx))
	}
}
func validateCron(s string) error {
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return fmt.Errorf("expected five fields")
	}
	bounds := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, f := range fields {
		if i == 3 {
			for j, n := range []string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"} {
				f = strings.ReplaceAll(strings.ToUpper(f), n, strconv.Itoa(j+1))
			}
		}
		if i == 4 {
			for j, n := range []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"} {
				f = strings.ReplaceAll(strings.ToUpper(f), n, strconv.Itoa(j))
			}
		}
		for _, item := range strings.Split(f, ",") {
			step := strings.Split(item, "/")
			if len(step) > 2 {
				return fmt.Errorf("invalid step")
			}
			if len(step) == 2 {
				n, e := strconv.Atoi(step[1])
				if e != nil || n < 1 || n > bounds[i][1]-bounds[i][0]+1 {
					return fmt.Errorf("invalid step")
				}
			}
			if step[0] == "*" {
				continue
			}
			r := strings.Split(step[0], "-")
			if len(r) > 2 {
				return fmt.Errorf("invalid range")
			}
			prev := -1
			for _, x := range r {
				n, e := strconv.Atoi(x)
				if e != nil || n < bounds[i][0] || n > bounds[i][1] || n < prev {
					return fmt.Errorf("invalid field")
				}
				prev = n
			}
		}
	}
	return nil
}
