// Package project_prometheus_config owns the complete primary-core custom discovery list.
package project_prometheus_config

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
)

type configModel struct {
	Targets []types.String          `tfsdk:"targets"`
	Labels  map[string]types.String `tfsdk:"labels"`
}
type model struct {
	ID            types.String   `tfsdk:"id"`
	ProjectID     types.String   `tfsdk:"project_id"`
	Configs       []configModel  `tfsdk:"configs"`
	CoreServiceID types.String   `tfsdk:"core_service_id"`
	Timeouts      timeouts.Value `tfsdk:"timeouts"`
}
type prometheusResource struct{ data *providerdata.ProviderData }

func NewResource() resource.Resource { return &prometheusResource{} }
func (*prometheusResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_prometheus_config"
}
func (r *prometheusResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (*prometheusResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Version: 0, MarkdownDescription: "Authoritatively replaces all Prometheus custom service-discovery groups on the default primary core. Use only one owner per project. Destroy clears the groups but retains the SYSTEM user-data key. This configures discovery, not scrape availability. Multi-core projects whose primary instance cannot be identified are rejected.", Attributes: map[string]schema.Attribute{
		"id":              schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id":      schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"core_service_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Discovered CORE stack identifier."},
		"configs": schema.ListNestedAttribute{Required: true, MarkdownDescription: "Complete ordered discovery groups. An empty list clears discovery.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"targets": schema.ListAttribute{Required: true, ElementType: types.StringType, Validators: []validator.List{targetsValidator{}}, MarkdownDescription: "Ordered hostnames/IP addresses with optional port 1–65535; IPv6 with port uses brackets."},
			"labels":  schema.MapAttribute{Required: true, ElementType: types.StringType, MarkdownDescription: "Discovery labels; an empty map is allowed."},
		}}},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func (m *model) configs() []client.PrometheusConfig {
	out := make([]client.PrometheusConfig, len(m.Configs))
	for i, c := range m.Configs {
		out[i] = client.PrometheusConfig{Targets: make([]string, len(c.Targets)), Labels: make(map[string]string, len(c.Labels))}
		for j, v := range c.Targets {
			out[i].Targets[j] = v.ValueString()
		}
		for k, v := range c.Labels {
			out[i].Labels[k] = v.ValueString()
		}
	}
	return out
}
func (m *model) apply(s client.PrometheusState) {
	m.ID = m.ProjectID
	m.CoreServiceID = types.StringValue(s.CoreServiceID)
	m.Configs = make([]configModel, len(s.Configs))
	for i, c := range s.Configs {
		m.Configs[i] = configModel{Targets: make([]types.String, len(c.Targets)), Labels: make(map[string]types.String, len(c.Labels))}
		for j, v := range c.Targets {
			m.Configs[i].Targets[j] = types.StringValue(v)
		}
		for k, v := range c.Labels {
			m.Configs[i].Labels[k] = types.StringValue(v)
		}
	}
}
func (r *prometheusResource) mutate(ctx context.Context, m *model, clear bool, accepted func(), diags *diag.Diagnostics) {
	unlock, e := r.data.Locks.Lock(ctx, "project:"+m.ProjectID.ValueString()+":config")
	if e != nil {
		diags.AddError("Locking project configuration", e.Error())
		return
	}
	defer unlock()
	if clear {
		if _, e = r.data.API.GetProject(ctx, m.ProjectID.ValueString()); client.IsNotFound(e, "projectNotFound") {
			return
		} else if e != nil {
			diags.AddError("Reading project before reset", e.Error())
			return
		}
	}
	desired := m.configs()
	if clear {
		desired = []client.PrometheusConfig{}
	}
	p, e := r.data.API.SetPrometheusConfig(ctx, m.ProjectID.ValueString(), desired)
	if clear && client.IsNotFound(e, "projectNotFound") {
		return
	}
	if e != nil {
		if a, ok := client.AsAPIError(e); ok && a.HTTPStatus == 400 && a.Code == "invalidPrometheusHost" {
			diags.AddError("Invalid Prometheus target", "configs targets must contain a hostname or IP with an optional port from 1 to 65535.")
			return
		}
		diags.AddError("Replacing Prometheus configuration", e.Error())
		return
	}
	m.ID = m.ProjectID
	accepted()
	if p != nil {
		if _, e = waiter.WaitProcess(ctx, r.data.API, p.ID, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute)); e != nil {
			diags.AddError("Waiting for Prometheus configuration", e.Error())
			return
		}
	}
	var state client.PrometheusState
	e = waiter.WaitProjectConfig(ctx, m.ProjectID.ValueString(), func(ctx context.Context) (bool, error) {
		var err error
		state, err = client.ReadPrometheusConfig(ctx, r.data.API, m.ProjectID.ValueString())
		if clear && client.IsNotFound(err, "projectNotFound") {
			return true, nil
		}
		return err == nil && client.EqualPrometheusConfigs(desired, state.Configs), err
	})
	if e != nil {
		diags.AddError("Verifying Prometheus configuration", e.Error())
		return
	}
	if !clear {
		m.apply(state)
	}
}
func (r *prometheusResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, ds := m.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(ds...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	r.mutate(ctx, &m, false, func() { m.CoreServiceID = types.StringNull(); resp.Diagnostics.Append(resp.State.Set(ctx, &m)...) }, &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	}
}
func (r *prometheusResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, ds := m.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(ds...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	r.mutate(ctx, &m, false, func() {}, &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	}
}
func (r *prometheusResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, e := client.ReadPrometheusConfig(ctx, r.data.API, m.ProjectID.ValueString())
	if client.IsNotFound(e, "projectNotFound") {
		resp.State.RemoveResource(ctx)
		return
	}
	if e != nil {
		resp.Diagnostics.AddError("Reading Prometheus configuration", e.Error())
		return
	}
	m.apply(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *prometheusResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, ds := m.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(ds...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	r.mutate(ctx, &m, true, func() {}, &resp.Diagnostics)
}
func (r *prometheusResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid project ID", "Import requires a nonempty project ID.")
		return
	}
	m := model{ProjectID: types.StringValue(req.ID), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	s, e := client.ReadPrometheusConfig(ctx, r.data.API, req.ID)
	if e != nil {
		resp.Diagnostics.AddError("Importing Prometheus configuration", fmt.Sprint(e))
		return
	}
	m.apply(s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
