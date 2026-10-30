package service

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	v "github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

type Resource struct{ data *providerdata.ProviderData }
type Model struct {
	ID           types.String   `tfsdk:"id"`
	ProjectID    types.String   `tfsdk:"project_id"`
	Hostname     types.String   `tfsdk:"hostname"`
	Type         types.String   `tfsdk:"type"`
	Start        types.Bool     `tfsdk:"start_without_code"`
	EnvIsolation types.String   `tfsdk:"env_isolation"`
	SSHIsolation types.String   `tfsdk:"ssh_isolation"`
	Vertical     types.Object   `tfsdk:"vertical_autoscaling"`
	Horizontal   types.Object   `tfsdk:"horizontal_autoscaling"`
	Desired      types.String   `tfsdk:"desired_state"`
	Current      types.Object   `tfsdk:"current_autoscaling"`
	Status       types.String   `tfsdk:"status"`
	Base         types.String   `tfsdk:"base"`
	Created      types.String   `tfsdk:"created"`
	Timeouts     timeouts.Value `tfsdk:"timeouts"`
}

func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, q resource.MetadataRequest, p *resource.MetadataResponse) {
	p.TypeName = q.ProviderTypeName + "_service"
}
func (r *Resource) Configure(_ context.Context, q resource.ConfigureRequest, p *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(q, p)
}
func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, p *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	p.Schema = schema.Schema{MarkdownDescription: "A runtime or managed Zerops service. Deployments and generated subdomain access are separate resources. Lifecycle is unmanaged unless desired_state is configured. Managed scaling defaults to and must meet provider floors: PostgreSQL disk 2 GB (postgresql_min_disk_gb), ClickHouse RAM 0.375 GB (ZEROPS_CLICKHOUSE_MIN_RAM_GB), and Elasticsearch RAM 2 GB (ZEROPS_ELASTICSEARCH_MIN_RAM_GB).", Attributes: map[string]schema.Attribute{
		"id":                   schema.StringAttribute{Computed: true, MarkdownDescription: "Service stack ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id":           schema.StringAttribute{Required: true, MarkdownDescription: "Parent project ID.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: replace},
		"hostname":             schema.StringAttribute{Required: true, MarkdownDescription: "Service hostname (1–40 lowercase alphanumeric characters).", Validators: []validator.String{v.Hostname()}, PlanModifiers: replace},
		"type":                 schema.StringAttribute{Required: true, MarkdownDescription: "Service type; base contains the canonical server spelling. Dual-mode managed types require explicit :single@ or :ha@ (for example postgresql:single@16); unqualified shorthand is rejected.", Validators: []validator.String{v.ServiceType()}, PlanModifiers: []planmodifier.String{serviceTypeRequiresReplace()}},
		"start_without_code":   schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Create-only runtime startup hint. Changing it does not deploy or start an existing service."},
		"env_isolation":        schema.StringAttribute{Optional: true, MarkdownDescription: "Create-only environment isolation tokens; the server validates supported modes.", Validators: []validator.String{v.Isolation(false)}, PlanModifiers: replace},
		"ssh_isolation":        schema.StringAttribute{Optional: true, MarkdownDescription: "Create-only SSH isolation tokens; the server validates supported modes.", Validators: []validator.String{v.Isolation(true)}, PlanModifiers: replace},
		"vertical_autoscaling": ScalingSchema(true, false), "horizontal_autoscaling": ScalingSchema(false, false),
		"desired_state":       schema.StringAttribute{Optional: true, MarkdownDescription: "RUNNING or STOPPED; omitted leaves lifecycle unmanaged. Deploying later can start a STOPPED service and produce drift.", Validators: []validator.String{stringvalidator.OneOf("RUNNING", "STOPPED")}},
		"current_autoscaling": CurrentSchema(), "status": schema.StringAttribute{Computed: true, MarkdownDescription: "Raw service status."}, "base": schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical server service type.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}, "created": schema.StringAttribute{Computed: true, MarkdownDescription: "Creation timestamp in RFC3339.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
	}, Blocks: map[string]schema.Block{"timeouts": v.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func (m *Model) refresh(s client.Service, d *diag.Diagnostics) {
	// Deployments set the run base from zerops.yml (live smoke: create "nodejs@22" →
	// ubuntu/nodejs@22, deploy with run.base nodejs@22 → alpine/nodejs@22). base is
	// therefore informational; only a configured type change replaces the service.
	m.ID = types.StringValue(s.ID)
	m.ProjectID = types.StringValue(s.ProjectID)
	m.Hostname = types.StringValue(s.Name)
	m.Base = types.StringValue(s.Base)
	m.Status = types.StringValue(s.Status)
	m.Created = types.StringValue(s.Created.Format(time.RFC3339))
	m.Current = CurrentValue(s.CurrentAutoscaling)
	vv, hh := ScalingValues(s.CustomAutoscaling, true)
	m.Vertical = vv
	m.Horizontal = hh
	if !m.Desired.IsNull() {
		switch s.Status {
		case "ACTIVE":
			m.Desired = types.StringValue("RUNNING")
		case "STOPPED":
			m.Desired = types.StringValue("STOPPED")
		}
	}
}
func (r *Resource) Read(ctx context.Context, q resource.ReadRequest, p *resource.ReadResponse) {
	var m Model
	p.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	s, e := r.data.API.GetService(ctx, m.ID.ValueString())
	if client.IsNotFound(e, "serviceStackNotFound") {
		p.State.RemoveResource(ctx)
		return
	}
	if e != nil {
		p.Diagnostics.AddError("Reading service", e.Error())
		return
	}
	m.refresh(s, &p.Diagnostics)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func (r *Resource) Create(ctx context.Context, q resource.CreateRequest, p *resource.CreateResponse) {
	var m Model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Create(ctx, 30*time.Minute)
	p.Diagnostics.Append(d...)
	if duration <= 0 {
		p.Diagnostics.AddError("Invalid timeout", "create timeout must be positive")
	}
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	a, e := scalingInput(m.Vertical, m.Horizontal)
	if e != nil {
		p.Diagnostics.AddError("Autoscaling configuration", e.Error())
		return
	}
	out, e := r.data.API.CreateService(ctx, m.ProjectID.ValueString(), client.ServiceCreate{Name: m.Hostname.ValueString(), ServiceStackVersionName: m.Type.ValueString(), StartWithoutCode: m.Start.ValueBoolPointer(), CustomAutoscaling: a, EnvIsolation: m.EnvIsolation.ValueStringPointer(), SSHIsolation: m.SSHIsolation.ValueStringPointer()})
	if e != nil {
		p.Diagnostics.AddError("Creating service", e.Error())
		return
	}
	m.ID = types.StringValue(out.ID)
	m.Base = types.StringValue(out.Base)
	m.Status = types.StringValue(out.Status)
	m.Created = types.StringValue(out.Created.Format(time.RFC3339))
	m.Current = CurrentValue(out.CurrentAutoscaling)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
	unlock, e := r.data.Locks.Lock(ctx, locks.ServiceKey(out.ID))
	if e != nil {
		p.Diagnostics.AddError("Locking service", e.Error())
		return
	}
	defer unlock()
	// A newly-created ID has no pre-mutation process history. Every discovered process is relevant.
	baseline := waiter.ProcessIDs{}
	ids := []string{}
	if out.Process != nil {
		ids = append(ids, out.Process.ID)
		if _, e = waiter.WaitProcess(ctx, r.data.API, out.Process.ID); e != nil {
			p.Diagnostics.AddError("Waiting for service creation", e.Error())
			return
		}
	}
	s, e := r.data.API.GetService(ctx, out.ID)
	if e != nil {
		p.Diagnostics.AddError("Reading created service", e.Error())
		return
	}
	expected := "ACTIVE"
	category := s.ServiceTypeCategory
	if category == "" {
		category = s.ServiceStackTypeInfo.Category
	}
	if category == "" {
		settings, err := r.data.API.Settings(ctx, r.data.OrganizationID)
		if err != nil {
			p.Diagnostics.AddError("Resolving service category", err.Error())
			return
		}
		for _, t := range settings.ServiceTypes {
			if t.ID == s.ServiceStackTypeID {
				category = t.Category
				if t.IsRuntime {
					category = "RUNTIME"
				}
				break
			}
		}
	}
	if category == "" {
		p.Diagnostics.AddError("Unknown service category", "Cannot determine runtime/managed readiness from the service or catalog.")
		return
	}
	if (strings.EqualFold(category, "RUNTIME") || strings.EqualFold(category, "USER")) && !m.Start.ValueBool() {
		expected = "READY_TO_DEPLOY"
	}
	s, e = waiter.WaitServiceSettled(ctx, r.data.API, out.ID, []string{expected}, baseline, ids)
	if e == nil {
		s, e = r.lifecycle(ctx, m, s)
	}
	if e != nil {
		if s.ID != "" {
			m.refresh(s, &p.Diagnostics)
			p.Diagnostics.Append(p.State.Set(ctx, &m)...)
		}
		p.Diagnostics.AddError("Waiting for service readiness", e.Error())
		return
	}
	p.Diagnostics.Append(scalingMismatch(ctx, m.Vertical, m.Horizontal, s.CustomAutoscaling)...)
	m.refresh(s, &p.Diagnostics)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func (r *Resource) lifecycle(ctx context.Context, m Model, s client.Service) (client.Service, error) {
	if m.Desired.IsNull() {
		return s, nil
	}
	if s.Status == "READY_TO_DEPLOY" {
		return s, fmt.Errorf("desired_state cannot be managed for READY_TO_DEPLOY service %s: deploy code or create with start_without_code=true first", s.ID)
	}
	expected := "ACTIVE"
	if m.Desired.ValueString() == "STOPPED" {
		expected = "STOPPED"
	}
	if s.Status == expected {
		return s, nil
	}
	baseline, e := waiter.SnapshotProcessIDs(ctx, r.data.API, s.ID)
	if e != nil {
		return s, e
	}
	var proc client.Process
	if expected == "STOPPED" {
		proc, e = r.data.API.StopService(ctx, s.ID)
	} else {
		proc, e = r.data.API.StartService(ctx, s.ID)
	}
	if e != nil {
		return s, e
	}
	if _, e = waiter.WaitProcess(ctx, r.data.API, proc.ID); e != nil {
		return s, e
	}
	return waiter.WaitServiceSettled(ctx, r.data.API, s.ID, []string{expected}, baseline, []string{proc.ID})
}
func (r *Resource) Update(ctx context.Context, q resource.UpdateRequest, p *resource.UpdateResponse) {
	var m, old Model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	p.Diagnostics.Append(q.State.Get(ctx, &old)...)
	if p.Diagnostics.HasError() {
		return
	}
	// Qualifying an existing shorthand changes state only. Concurrent scaling or
	// lifecycle changes still follow the normal update path.
	if canonicalTypeMigration(old.Type, m.Type, old.Base) && m.Vertical.Equal(old.Vertical) && m.Horizontal.Equal(old.Horizontal) && m.Desired.Equal(old.Desired) && m.Start.Equal(old.Start) {
		m.ID, m.Base, m.Status, m.Created, m.Current = old.ID, old.Base, old.Status, old.Created, old.Current
		p.Diagnostics.Append(p.State.Set(ctx, &m)...)
		return
	}
	duration, d := m.Timeouts.Update(ctx, 20*time.Minute)
	p.Diagnostics.Append(d...)
	if duration <= 0 {
		p.Diagnostics.AddError("Invalid timeout", "update timeout must be positive")
	}
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	unlock, e := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ID.ValueString()))
	if e != nil {
		p.Diagnostics.AddError("Locking service", e.Error())
		return
	}
	defer unlock()
	if !m.Start.Equal(old.Start) {
		p.Diagnostics.AddWarning("Create-only startup hint", "start_without_code changed in state only; no deployment or lifecycle request is made for this hint.")
	}
	if !m.Vertical.Equal(old.Vertical) || !m.Horizontal.Equal(old.Horizontal) {
		a, err := scalingInput(m.Vertical, m.Horizontal)
		if err != nil {
			p.Diagnostics.AddError("Autoscaling configuration", err.Error())
			return
		}
		baseline, err := waiter.SnapshotProcessIDs(ctx, r.data.API, m.ID.ValueString())
		if err != nil {
			p.Diagnostics.AddError("Snapshotting service processes", err.Error())
			return
		}
		proc, err := r.data.API.SetAutoscaling(ctx, m.ID.ValueString(), client.AutoscalingWrite{CustomAutoscaling: a})
		if err != nil {
			p.Diagnostics.AddError("Updating autoscaling", fmt.Sprintf("%s. CPU mode changes may require waiting for the server cooldown before applying again.", err))
			return
		}
		if proc != nil {
			if _, err = waiter.WaitProcess(ctx, r.data.API, proc.ID); err != nil {
				p.Diagnostics.AddError("Waiting for autoscaling", err.Error())
				return
			}
			if _, err = waiter.WaitServiceSettled(ctx, r.data.API, m.ID.ValueString(), []string{"ACTIVE", "STOPPED", "READY_TO_DEPLOY"}, baseline, []string{proc.ID}); err != nil {
				p.Diagnostics.AddError("Waiting for autoscaling readiness", err.Error())
				return
			}
		}
	}
	s, e := r.data.API.GetService(ctx, m.ID.ValueString())
	if e == nil {
		p.Diagnostics.Append(scalingMismatch(ctx, m.Vertical, m.Horizontal, s.CustomAutoscaling)...)
		if !p.Diagnostics.HasError() {
			s, e = r.lifecycle(ctx, m, s)
		}
	}
	if e != nil {
		p.Diagnostics.AddError("Updating service", e.Error())
		return
	}
	m.refresh(s, &p.Diagnostics)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func (r *Resource) Delete(ctx context.Context, q resource.DeleteRequest, p *resource.DeleteResponse) {
	var m Model
	p.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Delete(ctx, 20*time.Minute)
	p.Diagnostics.Append(d...)
	if duration <= 0 {
		p.Diagnostics.AddError("Invalid timeout", "delete timeout must be positive")
	}
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	unlock, e := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ID.ValueString()))
	if e != nil {
		p.Diagnostics.AddError("Locking service", e.Error())
		return
	}
	defer unlock()
	// Prove absence with a read rather than attempting an unownable mutation.
	_, e = r.data.API.GetService(ctx, m.ID.ValueString())
	if client.IsNotFound(e, "serviceStackNotFound") {
		return
	}
	if e != nil {
		p.Diagnostics.AddError("Reading service before deletion", e.Error())
		return
	}
	proc, e := r.data.API.DeleteService(ctx, m.ID.ValueString())
	if client.IsNotFound(e, "serviceStackNotFound") {
		return
	}
	if e == nil {
		_, e = waiter.WaitProcess(ctx, r.data.API, proc.ID)
	}
	if e == nil {
		e = waiter.WaitNotFound(ctx, m.ID.ValueString(), func(ctx context.Context) error { _, e := r.data.API.GetService(ctx, m.ID.ValueString()); return e }, []string{"serviceStackNotFound"})
	}
	if e != nil {
		p.Diagnostics.AddError("Deleting service", e.Error())
	}
}
func (r *Resource) ImportState(ctx context.Context, q resource.ImportStateRequest, p *resource.ImportStateResponse) {
	var s client.Service
	var e error
	parts := strings.Split(q.ID, "/")
	if len(parts) == 1 {
		s, e = r.data.API.GetService(ctx, q.ID)
	} else if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		s, e = r.data.API.GetServiceByName(ctx, parts[0], parts[1])
	} else {
		p.Diagnostics.AddError("Invalid import ID", "Use service_id or project_id/hostname.")
		return
	}
	if e != nil {
		p.Diagnostics.AddError("Importing service", e.Error())
		return
	}
	if s.IsSystem || strings.EqualFold(s.ServiceTypeCategory, "OBJECT_STORAGE") || s.ServiceStackTypeID == "object_storage" || s.Base == "object-storage" {
		p.Diagnostics.AddError("Unsupported service ownership", "System and object-storage stacks cannot be adopted by zerops_service.")
		return
	}
	m := Model{Type: types.StringValue(s.Base), Start: types.BoolValue(false), EnvIsolation: types.StringNull(), SSHIsolation: types.StringNull(), Desired: types.StringNull(), Base: types.StringNull(), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	m.refresh(s, &p.Diagnostics)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
