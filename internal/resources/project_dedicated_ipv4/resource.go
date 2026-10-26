// Package project_dedicated_ipv4 manages a non-releasing dedicated IPv4 allocation.
package project_dedicated_ipv4

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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

type Resource struct{ data *providerdata.ProviderData }
type model struct {
	ID        types.String   `tfsdk:"id"`
	ProjectID types.String   `tfsdk:"project_id"`
	Address   types.String   `tfsdk:"address"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func NewResource() resource.Resource { return &Resource{} }
func (*Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_dedicated_ipv4"
}
func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (*Resource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Requests or adopts a billed dedicated project IPv4 address. Destroy only removes Terraform ownership: the address remains billed until its project is deleted. A shared IPv4 flag is not a dedicated allocation.", Attributes: map[string]schema.Attribute{
		"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Project ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Project to adopt or allocate an IPv4 address for. Only one resource may own the allocation.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"address":    schema.StringAttribute{Computed: true, MarkdownDescription: "Dedicated IPv4 address returned by the project API."},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func (r *Resource) load(ctx context.Context, m *model) (bool, error) {
	p, err := r.data.API.GetProject(ctx, m.ProjectID.ValueString())
	if client.IsNotFound(err, "projectNotFound") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	address, err := waiter.DedicatedIPv4Address(p)
	if err != nil {
		return false, err
	}
	if address == "" {
		return false, nil
	}
	m.ID = m.ProjectID
	m.Address = types.StringValue(address)
	return true, nil
}
func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	id := m.ProjectID.ValueString()
	unlock, err := r.data.Locks.Lock(ctx, "project:"+id+":config")
	if err != nil {
		resp.Diagnostics.AddError("Locking project", err.Error())
		return
	}
	defer unlock()
	// A missing project is not an absent allocation and must not trigger a request.
	project, err := r.data.API.GetProject(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Reading dedicated IPv4 project", err.Error())
		return
	}
	address, err := waiter.DedicatedIPv4Address(project)
	if err != nil {
		resp.Diagnostics.AddError("Reading dedicated IPv4", err.Error())
		return
	}
	if address != "" {
		m.ID = m.ProjectID
		m.Address = types.StringValue(address)
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		return
	}
	process, err := r.data.API.RequestDedicatedIPv4(ctx, id)
	if err != nil {
		detail := err.Error()
		if e, ok := client.AsAPIError(err); ok && e.HTTPStatus == 400 {
			switch e.Code {
			case "publicIpRequestForbidden":
				detail = "publicIpRequestForbidden: Dedicated IPv4 requires the organization's first account top-up. No allocation was accepted."
			case "publicIpRequestOnlyOne":
				detail = "publicIpRequestOnlyOne: The project already has an allocation or pending allocation. Resolve the allocation conflict before retrying; the provider does not replay this request."
			}
		}
		resp.Diagnostics.AddError("Requesting dedicated IPv4", detail)
		return
	}
	m.ID = m.ProjectID
	m.Address = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err = waiter.WaitProcess(ctx, r.data.API, process.ID, waiter.WithIntervals(5*time.Second, 5*time.Second, 0, 0)); err != nil {
		resp.Diagnostics.AddError("Waiting for dedicated IPv4 process", err.Error())
		return
	}
	address, err = waiter.WaitDedicatedIPv4(ctx, r.data.API, id)
	if err != nil {
		resp.Diagnostics.AddError("Waiting for dedicated IPv4 address", err.Error())
		return
	}
	m.Address = types.StringValue(address)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	exists, err := r.load(ctx, &m)
	if err != nil {
		resp.Diagnostics.AddError("Reading dedicated IPv4", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		resp.Diagnostics.AddWarning("Dedicated IPv4 allocation absent", "The project or its dedicated IPv4 allocation is absent. Recreating this resource can allocate and bill for another address.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (*Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	// Only timeouts can update; retain the refreshed allocation readback rather
	// than writing the plan's unknown computed address into final state.
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("address"), &m.Address)...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	}
}
func (*Resource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Dedicated IPv4 is not released", "Dedicated IPv4 is not released; it remains billed on this project until the project is deleted.")
	resp.State.RemoveResource(ctx)
}
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Import requires a project ID with an existing dedicated IPv4 allocation.")
		return
	}
	unlock, err := r.data.Locks.Lock(ctx, "project:"+req.ID+":config")
	if err != nil {
		resp.Diagnostics.AddError("Locking project", err.Error())
		return
	}
	defer unlock()
	m := model{ProjectID: types.StringValue(req.ID), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	exists, err := r.load(ctx, &m)
	if err != nil {
		resp.Diagnostics.AddError("Importing dedicated IPv4", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError("Dedicated IPv4 allocation absent", "Import requires an existing dedicated IPv4 address; import never requests an allocation.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
