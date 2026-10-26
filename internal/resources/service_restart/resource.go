package service_restart

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"time"
)

type Resource struct{ data *providerdata.ProviderData }
type model struct {
	ID          types.String   `tfsdk:"id"`
	ServiceID   types.String   `tfsdk:"service_id"`
	Triggers    types.Map      `tfsdk:"triggers"`
	LastRestart types.String   `tfsdk:"last_restart"`
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
}

func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_restart"
}
func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Version: 0, MarkdownDescription: "Explicit restart invocation. Changing triggers replaces this resource and restarts once; unchanged applies do nothing. Restart starts a STOPPED service and may conflict with a sibling service desired_state=STOPPED. Delete is state-only and leaves the service running. Import is unsupported because triggers cannot be recovered. Do not put secrets in triggers.", Attributes: map[string]schema.Attribute{
		"id":           schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"service_id":   schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"triggers":     schema.MapAttribute{Required: true, ElementType: types.StringType, PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()}},
		"last_restart": schema.StringAttribute{Computed: true, MarkdownDescription: "Returned stack.restart process ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true})}}
}
func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || req.State.Raw.IsNull() {
		return
	}
	var plan, prior model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.ServiceID.Equal(prior.ServiceID) || !plan.Triggers.Equal(prior.Triggers) {
		plan.ID = types.StringUnknown()
		plan.LastRestart = types.StringUnknown()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
	}
}
func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.ServiceID.IsUnknown() || m.ServiceID.IsNull() || m.Triggers.IsUnknown() || m.Triggers.IsNull() {
		resp.Diagnostics.AddError("Unknown restart configuration", "Service identity and triggers must be known before applying.")
		return
	}
	for _, trigger := range m.Triggers.Elements() {
		if trigger.IsUnknown() {
			resp.Diagnostics.AddError("Unknown restart trigger", "Trigger values must be known before applying.")
			return
		}
	}
	d, diags := m.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ServiceID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Lock failed", err.Error())
		return
	}
	defer unlock()
	s, err := r.data.API.GetService(ctx, m.ServiceID.ValueString())
	if err == nil && s.IsSystem {
		resp.Diagnostics.AddError("Invalid service", "System services cannot be restarted by this resource.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading service failed", err.Error())
		return
	}
	baseline, err := waiter.SnapshotProcessIDs(ctx, r.data.API, m.ServiceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading processes failed", err.Error())
		return
	}
	p, err := r.data.API.RestartService(ctx, m.ServiceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Restarting service failed", err.Error())
		return
	}
	m.ID = types.StringValue(p.ID)
	m.LastRestart = m.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	_, err = waiter.WaitProcess(ctx, r.data.API, p.ID, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute))
	if err == nil {
		_, err = waiter.WaitServiceSettled(ctx, r.data.API, m.ServiceID.ValueString(), []string{"ACTIVE"}, baseline, []string{p.ID}, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute))
	}
	if err != nil {
		resp.Diagnostics.AddError("Restarting service failed", err.Error())
	}
}
func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.data.API.GetService(ctx, m.ServiceID.ValueString())
	if client.IsNotFound(err, "serviceStackNotFound") {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading restart service failed", err.Error())
	}
}
func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	}
}
func (r *Resource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {}
