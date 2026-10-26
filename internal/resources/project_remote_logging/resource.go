package project_remote_logging

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
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
	ID           types.String   `tfsdk:"id"`
	ProjectID    types.String   `tfsdk:"project_id"`
	Timeouts     timeouts.Value `tfsdk:"timeouts"`
	SyslogConfig types.String   `tfsdk:"syslog_config"`
	Certificate  types.String   `tfsdk:"certificate"`
	Type         types.String   `tfsdk:"type"`
}

func NewResource() resource.Resource { return &configResource{} }
func (*configResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_remote_logging"
}
func (r *configResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (*configResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Owns remote logging configuration for one project. Delete clears the snippet, certificate and forwarding type. Configuration and certificate are sensitive state-retained inputs; only type is readable.", Attributes: map[string]schema.Attribute{
		"id":            schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id":    schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"syslog_config": schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "syslog-ng snippet. Stored in Terraform state; remote content drift cannot be detected."},
		"certificate":   schema.StringAttribute{Optional: true, Computed: true, Sensitive: true, Default: stringdefault.StaticString("")},
		"type":          schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func (r *configResource) write(ctx context.Context, m *model, reset bool) (*client.Process, error) {
	var typ *string
	if reset {
		m.SyslogConfig = types.StringValue("")
		m.Certificate = types.StringValue("")
		m.Type = types.StringNull()
	}
	if !m.Type.IsNull() {
		v := m.Type.ValueString()
		typ = &v
	}
	p, e := r.data.API.SetRemoteLogging(ctx, m.ProjectID.ValueString(), client.RemoteLoggingWrite{SyslogConfig: m.SyslogConfig.ValueString(), Certificate: m.Certificate.ValueString(), Type: typ})
	return &p, e
}
func (r *configResource) load(ctx context.Context, m *model) (bool, error) {
	p, e := r.data.API.GetProject(ctx, m.ProjectID.ValueString())
	if client.IsNotFound(e, "projectNotFound") {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	m.ID = m.ProjectID
	m.Type = types.StringPointerValue(p.LogForwardingType)
	return true, nil
}
func (r *configResource) matches(ctx context.Context, m *model) (bool, error) {
	p, e := r.data.API.GetProject(ctx, m.ProjectID.ValueString())
	if e != nil {
		return false, e
	}
	return types.StringPointerValue(p.LogForwardingType).Equal(m.Type), nil
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
		if e, ok := client.AsAPIError(err); ok && e.HTTPStatus == 400 && e.Code == "projectInvalidSyslogFile" {
			diags.AddAttributeError(path.Root("syslog_config"), "Invalid syslog-ng configuration", "The server rejected syslog_config. Parser output is omitted because it can contain logging credentials.")
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
	if m.SyslogConfig.IsNull() {
		resp.Diagnostics.AddWarning("Logging configuration unavailable", "Imported syslog_config and certificate cannot be recovered. Supply syslog_config before applying; remote content drift cannot be detected.")
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
	if _, err = r.data.API.GetProject(ctx, m.ProjectID.ValueString()); client.IsNotFound(err, "projectNotFound") {
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Reading project before reset", err.Error())
		return
	}
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
	if m.SyslogConfig.IsNull() {
		resp.Diagnostics.AddWarning("Logging configuration unavailable", "Imported syslog_config and certificate cannot be recovered. Supply syslog_config before applying; remote content drift cannot be detected.")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
