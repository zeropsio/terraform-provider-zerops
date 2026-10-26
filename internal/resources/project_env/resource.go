package project_env

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

type Resource struct{ data *providerdata.ProviderData }
type Model struct {
	ID        types.String   `tfsdk:"id"`
	ProjectID types.String   `tfsdk:"project_id"`
	Key       types.String   `tfsdk:"key"`
	Content   types.String   `tfsdk:"content"`
	Sensitive types.Bool     `tfsdk:"sensitive"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, q resource.MetadataRequest, p *resource.MetadataResponse) {
	p.TypeName = q.ProviderTypeName + "_project_env"
}
func (r *Resource) Configure(_ context.Context, q resource.ConfigureRequest, p *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(q, p)
}
func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, p *resource.SchemaResponse) {
	p.Schema = schema.Schema{MarkdownDescription: "A USER project environment variable. Changes do not restart services. Content is always sensitive in Terraform state. With non-sudo credentials, masked refresh retains known content and cannot detect remote content drift. Importing masked secrets requires sudo credentials.", Attributes: map[string]schema.Attribute{
		"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical environment row ID, not process ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id": schema.StringAttribute{Required: true, MarkdownDescription: "Parent project ID.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"key":        schema.StringAttribute{Required: true, MarkdownDescription: "Environment key. SYSTEM isolation keys and reserved prefixes cannot be managed.", Validators: []validator.String{validators.EnvKey(), keyValidator{}}},
		"content":    schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Single-line ASCII content, at most 8192 characters. Stored in state even when masked remotely.", Validators: []validator.String{contentValidator{}}},
		"sensitive":  schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Mask content in the remote API."},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func FindUser(rows []client.Env, key string) (client.Env, error) {
	var found []client.Env
	for _, row := range rows {
		if strings.EqualFold(row.Key, key) && row.Type == "USER" {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		return client.Env{}, fmt.Errorf("expected one USER environment row for key %q, found %d", key, len(found))
	}
	return found[0], nil
}
func (r *Resource) read(ctx context.Context, m *Model, d *diag.Diagnostics) error {
	row, e := r.data.API.GetProjectEnv(ctx, m.ID.ValueString())
	if e != nil {
		return e
	}
	if row.Type != "USER" {
		return fmt.Errorf("environment row %s is not USER-owned; SYSTEM rows belong to zerops_project", row.ID)
	}
	value := row.Content
	if row.Sensitive && row.Content == "REDACTED" {
		value, e = r.data.API.RevealProjectEnv(ctx, row.ID)
		if e != nil {
			a, ok := client.AsAPIError(e)
			if !ok || a.HTTPStatus != 403 || a.Code != "notInSudoMode" {
				return fmt.Errorf("cannot reveal sensitive project environment content: %s", safeError(e))
			}
			if m.Content.IsNull() || m.Content.IsUnknown() {
				return fmt.Errorf("import of masked project environment content requires sudo credentials")
			}
			value = m.Content.ValueString()
			tflog.Warn(ctx, "Masked project environment refresh retained known content; remote-content drift detection requires sudo", map[string]any{"id": row.ID})
			d.AddWarning("Masked environment content retained", "The API requires sudo to reveal content. The known state value was retained; remote content drift cannot be detected.")
		}
	}
	m.ID = types.StringValue(row.ID)
	m.ProjectID = types.StringValue(row.ProjectID)
	m.Key = types.StringValue(row.Key)
	m.Content = types.StringValue(value)
	m.Sensitive = types.BoolValue(row.Sensitive)
	return nil
}
func op(ctx context.Context, m Model, kind string, d *diag.Diagnostics) (context.Context, context.CancelFunc) {
	var t time.Duration
	var ds diag.Diagnostics
	switch kind {
	case "create":
		t, ds = m.Timeouts.Create(ctx, 30*time.Minute)
	case "update":
		t, ds = m.Timeouts.Update(ctx, 20*time.Minute)
	default:
		t, ds = m.Timeouts.Delete(ctx, 20*time.Minute)
	}
	d.Append(ds...)
	if t <= 0 {
		d.AddError("Invalid timeout", "Timeout must be positive.")
	}
	return context.WithTimeout(ctx, t)
}
func (m Model) write() client.EnvWrite {
	return client.EnvWrite{Key: m.Key.ValueString(), Content: m.Content.ValueString(), Sensitive: m.Sensitive.ValueBool()}
}
func (r *Resource) Create(ctx context.Context, q resource.CreateRequest, p *resource.CreateResponse) {
	var m Model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := op(ctx, m, "create", &p.Diagnostics)
	defer cancel()
	if p.Diagnostics.HasError() {
		return
	}
	unlock, e := r.data.Locks.Lock(ctx, locks.ProjectEnvKey(m.ProjectID.ValueString()))
	if e != nil {
		p.Diagnostics.AddError("Locking project environment", e.Error())
		return
	}
	defer unlock()
	proc, e := r.data.API.CreateProjectEnv(ctx, m.ProjectID.ValueString(), m.write())
	if e != nil {
		p.Diagnostics.AddError("Creating project environment", safeError(e))
		return
	}
	_, e = waiter.WaitProcess(ctx, r.data.API, proc.ID)
	if e != nil {
		p.Diagnostics.AddError("Waiting for project environment", e.Error())
		return
	}
	rows, e := r.data.API.ListProjectEnv(ctx, m.ProjectID.ValueString())
	if e != nil {
		p.Diagnostics.AddError("Discovering environment ID", e.Error())
		return
	}
	row, e := FindUser(rows, m.Key.ValueString())
	if e != nil {
		p.Diagnostics.AddError("Discovering environment ID", e.Error())
		return
	}
	m.ID = types.StringValue(row.ID)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
	if e = r.read(ctx, &m, &p.Diagnostics); e != nil {
		p.Diagnostics.AddError("Reading created environment", e.Error())
		return
	}
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func (r *Resource) Read(ctx context.Context, q resource.ReadRequest, p *resource.ReadResponse) {
	var m Model
	p.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	e := r.read(ctx, &m, &p.Diagnostics)
	if client.IsNotFound(e, "projectEnvNotFound") {
		p.State.RemoveResource(ctx)
		return
	}
	if e != nil {
		p.Diagnostics.AddError("Reading project environment", e.Error())
		return
	}
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func (r *Resource) Update(ctx context.Context, q resource.UpdateRequest, p *resource.UpdateResponse) {
	var m Model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := op(ctx, m, "update", &p.Diagnostics)
	defer cancel()
	if p.Diagnostics.HasError() {
		return
	}
	unlock, e := r.data.Locks.Lock(ctx, locks.ProjectEnvKey(m.ProjectID.ValueString()))
	if e != nil {
		p.Diagnostics.AddError("Locking project environment", e.Error())
		return
	}
	defer unlock()
	row, e := r.data.API.GetProjectEnv(ctx, m.ID.ValueString())
	if e != nil {
		p.Diagnostics.AddError("Reading environment ownership", e.Error())
		return
	}
	if row.Type != "USER" {
		p.Diagnostics.AddError("Invalid environment ownership", "SYSTEM environment rows belong to zerops_project.")
		return
	}
	proc, e := r.data.API.UpdateProjectEnv(ctx, m.ID.ValueString(), m.write())
	if e != nil {
		p.Diagnostics.AddError("Updating project environment", safeError(e))
		return
	}
	_, e = waiter.WaitProcess(ctx, r.data.API, proc.ID)
	if e == nil {
		e = r.read(ctx, &m, &p.Diagnostics)
	}
	if e != nil {
		p.Diagnostics.AddError("Reading updated environment", e.Error())
		return
	}
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func (r *Resource) Delete(ctx context.Context, q resource.DeleteRequest, p *resource.DeleteResponse) {
	var m Model
	p.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := op(ctx, m, "delete", &p.Diagnostics)
	defer cancel()
	if p.Diagnostics.HasError() {
		return
	}
	unlock, e := r.data.Locks.Lock(ctx, locks.ProjectEnvKey(m.ProjectID.ValueString()))
	if e != nil {
		p.Diagnostics.AddError("Locking project environment", e.Error())
		return
	}
	defer unlock()
	row, e := r.data.API.GetProjectEnv(ctx, m.ID.ValueString())
	if client.IsNotFound(e, "projectEnvNotFound") {
		return
	}
	if e != nil {
		p.Diagnostics.AddError("Reading environment ownership", e.Error())
		return
	}
	if row.Type != "USER" {
		p.Diagnostics.AddError("Invalid environment ownership", "SYSTEM environment rows cannot be deleted.")
		return
	}
	proc, e := r.data.API.DeleteProjectEnv(ctx, m.ID.ValueString())
	if client.IsNotFound(e, "projectEnvNotFound") {
		return
	}
	if e == nil {
		_, e = waiter.WaitProcess(ctx, r.data.API, proc.ID)
	}
	if e == nil {
		e = waiter.WaitNotFound(ctx, m.ID.ValueString(), func(ctx context.Context) error { _, e := r.data.API.GetProjectEnv(ctx, m.ID.ValueString()); return e }, []string{"projectEnvNotFound"})
	}
	if e != nil {
		p.Diagnostics.AddError("Deleting project environment", e.Error())
	}
}
func (r *Resource) ImportState(ctx context.Context, q resource.ImportStateRequest, p *resource.ImportStateResponse) {
	var m Model
	m.Content = types.StringNull()
	if strings.Contains(q.ID, "/") {
		parts := strings.Split(q.ID, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			p.Diagnostics.AddError("Invalid import ID", "Expected environment ID or <project_id>/<key>.")
			return
		}
		rows, e := r.data.API.ListProjectEnv(ctx, parts[0])
		if e != nil {
			p.Diagnostics.AddError("Looking up environment", e.Error())
			return
		}
		row, e := FindUser(rows, parts[1])
		if e != nil {
			p.Diagnostics.AddError("Looking up environment", e.Error())
			return
		}
		m.ID = types.StringValue(row.ID)
	} else {
		m.ID = types.StringValue(q.ID)
	}
	if e := r.read(ctx, &m, &p.Diagnostics); e != nil {
		p.Diagnostics.AddError("Importing project environment", e.Error())
		return
	}
	m.Timeouts = timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}

// Only the client's structured, sanitized error rendering is safe for secret writes.
func safeError(err error) string {
	if api, ok := client.AsAPIError(err); ok {
		return api.Error()
	}
	return "Project environment request failed; content and untrusted error details were omitted."
}
