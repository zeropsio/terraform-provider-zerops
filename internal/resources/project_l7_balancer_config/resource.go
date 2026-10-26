package project_l7_balancer_config

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
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
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"time"
)

type configResource struct{ data *providerdata.ProviderData }
type model struct {
	ID        types.String   `tfsdk:"id"`
	ProjectID types.String   `tfsdk:"project_id"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
	Values    types.Map      `tfsdk:"values"`
	Defaults  types.Map      `tfsdk:"defaults"`
}

func NewResource() resource.Resource { return &configResource{} }
func (*configResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_l7_balancer_config"
}
func (r *configResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (*configResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Authoritatively owns every L7 balancer override for a project. Do not share ownership with another resource or operator. Delete resets all overrides to server defaults.", Attributes: map[string]schema.Attribute{
		"id":         schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"values":     schema.MapAttribute{Required: true, ElementType: types.StringType, Validators: []validator.Map{mapvalidator.KeysAre(stringvalidator.LengthAtLeast(1)), mapvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))}, MarkdownDescription: "Complete authoritative override map. Omitted keys reset to defaults; keys and values must be nonempty."},
		"defaults":   schema.MapAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Live server defaults, not configured overrides."},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func l7Maps(v client.L7BalancerConfig) (map[string]string, map[string]string, error) {
	if v.Values == nil {
		return nil, nil, fmt.Errorf("missing balancer values")
	}
	current := map[string]string{}
	defaults := map[string]string{}
	for _, v := range v.Values {
		if v.Name == "" {
			return nil, nil, fmt.Errorf("empty balancer key")
		}
		if _, ok := defaults[v.Name]; ok {
			return nil, nil, fmt.Errorf("duplicate balancer key")
		}
		defaults[v.Name] = v.Default
		if v.Current != nil {
			current[v.Name] = *v.Current
		}
	}
	return current, defaults, nil
}
func (r *configResource) write(ctx context.Context, m *model, reset bool) (*client.Process, error) {
	remote, e := r.data.API.GetL7BalancerConfig(ctx, m.ProjectID.ValueString())
	if e != nil {
		return nil, e
	}
	current, defaults, e := l7Maps(remote)
	if e != nil {
		return nil, e
	}
	values := map[string]string{}
	d := m.Values.ElementsAs(ctx, &values, false)
	if d.HasError() {
		return nil, fmt.Errorf("invalid override map")
	}
	out := map[string]*string{}
	if reset {
		for k := range current {
			out[k] = nil
		}
		for k := range values {
			out[k] = nil
		}
		m.Values = types.MapValueMust(types.StringType, map[string]attr.Value{})
	} else {
		for k, v := range values {
			if k == "" || v == "" {
				return nil, fmt.Errorf("values requires nonempty keys and values")
			}
			if _, ok := defaults[k]; !ok {
				return nil, &client.APIError{HTTPStatus: 400, Code: "validation"}
			}
			out[k] = &v
		}
	}
	m.Defaults, _ = types.MapValueFrom(ctx, types.StringType, defaults)
	p, e := r.data.API.SetL7BalancerConfig(ctx, m.ProjectID.ValueString(), out)
	return &p, e
}
func (r *configResource) load(ctx context.Context, m *model) (bool, error) {
	v, e := r.data.API.GetL7BalancerConfig(ctx, m.ProjectID.ValueString())
	if client.IsNotFound(e, "projectNotFound") {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	current, defaults, e := l7Maps(v)
	if e != nil {
		return false, e
	}
	m.ID = m.ProjectID
	m.Values, _ = types.MapValueFrom(ctx, types.StringType, current)
	m.Defaults, _ = types.MapValueFrom(ctx, types.StringType, defaults)
	return true, nil
}
func (r *configResource) matches(ctx context.Context, m *model) (bool, error) {
	v, e := r.data.API.GetL7BalancerConfig(ctx, m.ProjectID.ValueString())
	if e != nil {
		return false, e
	}
	current, _, e := l7Maps(v)
	if e != nil {
		return false, e
	}
	actual, d := types.MapValueFrom(ctx, types.StringType, current)
	if d.HasError() {
		return false, fmt.Errorf("invalid balancer readback")
	}
	return actual.Equal(m.Values), nil
}

func (r *configResource) apply(ctx context.Context, m *model, save func(context.Context, *model) diag.Diagnostics, diags *diag.Diagnostics, create bool) {
	var duration time.Duration
	var d diag.Diagnostics
	if create {
		duration, d = m.Timeouts.Create(ctx, 30*time.Minute)
	} else {
		duration, d = m.Timeouts.Update(ctx, 20*time.Minute)
	}
	diags.Append(d...)
	if diags.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	unlock, err := r.data.Locks.Lock(ctx, "project:"+m.ProjectID.ValueString()+":config")
	if err != nil {
		diags.AddError("Locking project configuration", err.Error())
		return
	}
	defer unlock()
	process, err := r.write(ctx, m, false)
	if err != nil {
		if e, ok := client.AsAPIError(err); ok && e.HTTPStatus == 400 && e.Code == "validation" {
			diags.AddAttributeError(path.Root("values"), "Invalid balancer overrides", "The server rejected the override keys or values. Only names supported by the project's current balancer configuration are accepted.")
			return
		}
		diags.AddError("Writing project configuration", err.Error())
		return
	}
	m.ID = types.StringValue(m.ProjectID.ValueString())
	diags.Append(save(ctx, m)...)
	if process != nil {
		if _, err = waiter.WaitProcess(ctx, r.data.API, process.ID, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute)); err != nil {
			diags.AddError("Waiting for project configuration", err.Error())
			return
		}
	}
	err = waiter.WaitProjectConfig(ctx, m.ProjectID.ValueString(), func(ctx context.Context) (bool, error) { return r.matches(ctx, m) })
	if err != nil {
		diags.AddError("Verifying project configuration", err.Error())
		return
	}
	_, err = r.load(ctx, m)
	if err != nil {
		diags.AddError("Reading project configuration", err.Error())
		return
	}
	diags.Append(save(ctx, m)...)
}
func (r *configResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &m, func(ctx context.Context, m *model) diag.Diagnostics { return resp.State.Set(ctx, m) }, &resp.Diagnostics, true)
}
func (r *configResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &m, func(ctx context.Context, m *model) diag.Diagnostics { return resp.State.Set(ctx, m) }, &resp.Diagnostics, false)
}
func (r *configResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	exists, err := r.load(ctx, &m)
	if err != nil {
		resp.Diagnostics.AddError("Reading project configuration", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *configResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	unlock, err := r.data.Locks.Lock(ctx, "project:"+m.ProjectID.ValueString()+":config")
	if err != nil {
		resp.Diagnostics.AddError("Locking project configuration", err.Error())
		return
	}
	defer unlock()
	process, err := r.write(ctx, &m, true)
	if client.IsNotFound(err, "projectNotFound") {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Resetting project configuration", err.Error())
		return
	}
	if process != nil {
		if _, err = waiter.WaitProcess(ctx, r.data.API, process.ID, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute)); err != nil {
			resp.Diagnostics.AddError("Waiting for reset", err.Error())
			return
		}
	}
	if err = waiter.WaitProjectConfig(ctx, m.ProjectID.ValueString(), func(ctx context.Context) (bool, error) {
		ok, e := r.matches(ctx, &m)
		if client.IsNotFound(e, "projectNotFound") {
			return true, nil
		}
		return ok, e
	}); err != nil {
		resp.Diagnostics.AddError("Verifying reset", err.Error())
	}
}
func (r *configResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	m := model{Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a nonempty project ID.")
		return
	}
	m.ProjectID = types.StringValue(req.ID)
	exists, err := r.load(ctx, &m)
	if err != nil {
		resp.Diagnostics.AddError("Importing project configuration", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError("Cannot import absent configuration", "The project or peer does not exist.")
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
