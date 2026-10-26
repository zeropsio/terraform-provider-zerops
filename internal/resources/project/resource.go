package project

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

type Resource struct{ data *providerdata.ProviderData }
type Model struct {
	ID               types.String   `tfsdk:"id"`
	OrganizationID   types.String   `tfsdk:"organization_id"`
	Name             types.String   `tfsdk:"name"`
	Description      types.String   `tfsdk:"description"`
	Tags             types.Set      `tfsdk:"tags"`
	Mode             types.String   `tfsdk:"mode"`
	PublicIPv4Shared types.Bool     `tfsdk:"public_ipv4_shared"`
	EnvIsolation     types.String   `tfsdk:"env_isolation"`
	SSHIsolation     types.String   `tfsdk:"ssh_isolation"`
	Location         types.String   `tfsdk:"location"`
	MaxCreditLimit   types.String   `tfsdk:"max_credit_limit"`
	Status           types.String   `tfsdk:"status"`
	PublicIPv4       types.String   `tfsdk:"public_ipv4"`
	PublicIPv6       types.String   `tfsdk:"public_ipv6"`
	Subdomain        types.String   `tfsdk:"zerops_subdomain_host"`
	Created          types.String   `tfsdk:"created"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, q resource.MetadataRequest, p *resource.MetadataResponse) {
	p.TypeName = q.ProviderTypeName + "_project"
}
func (r *Resource) Configure(_ context.Context, q resource.ConfigureRequest, p *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(q, p)
}
func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, p *resource.SchemaResponse) {
	s := func(d string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: d}
	}
	p.Schema = schema.Schema{MarkdownDescription: "A Zerops project. Credit limits are retained in state because the API cannot read them; removing configuration does not clear a cap. An externally DELETING project is forgotten before purge finishes, so recreation may overlap deletion.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Project ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}, "organization_id": schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Organization ID; defaults to the provider organization.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}},
		"name":               schema.StringAttribute{Required: true, MarkdownDescription: "Project name (not unique).", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"description":        schema.StringAttribute{Optional: true, MarkdownDescription: "Description; omission clears it."},
		"tags":               schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, Default: setdefault.StaticValue(types.SetValueMust(types.StringType, nil)), MarkdownDescription: "Project tags.", Validators: []validator.Set{setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))}},
		"mode":               schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("LIGHT"), MarkdownDescription: "LIGHT or SERIOUS. Upgrades happen in place; downgrades replace the project.", Validators: []validator.String{stringvalidator.OneOf("LIGHT", "SERIOUS")}},
		"public_ipv4_shared": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Enable shared public IPv4."},
		"env_isolation":      schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Environment isolation tokens; defaults to server settings.", Validators: []validator.String{validators.Isolation(false)}, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"ssh_isolation":      schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "SSH isolation tokens; defaults to server settings.", Validators: []validator.String{validators.Isolation(true)}, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"location":           schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Location catalog ID; defaults to server location.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}},
		"max_credit_limit":   schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Positive decimal credit cap. Unreadable remotely, retained in Terraform state, and cannot be cleared after being set.", Validators: []validator.String{validators.PositiveDecimalString()}, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"status":             s("Raw project status."), "public_ipv4": s("Public IPv4, if allocated."), "public_ipv6": s("Public IPv6, if allocated."), "zerops_subdomain_host": s("Zerops project subdomain host."), "created": s("Creation time in RFC3339 format."),
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func (r *Resource) ModifyPlan(ctx context.Context, q resource.ModifyPlanRequest, p *resource.ModifyPlanResponse) {
	if q.Plan.Raw.IsNull() {
		return
	}
	var m Model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	var configuredOrganization types.String
	p.Diagnostics.Append(q.Config.GetAttribute(ctx, path.Root("organization_id"), &configuredOrganization)...)
	if m.OrganizationID.IsUnknown() && configuredOrganization.IsNull() && r.data != nil {
		m.OrganizationID = types.StringValue(r.data.OrganizationID)
	}
	if !q.State.Raw.IsNull() {
		var old Model
		p.Diagnostics.Append(q.State.Get(ctx, &old)...)
		if old.Mode.ValueString() == "SERIOUS" && m.Mode.ValueString() == "LIGHT" {
			p.RequiresReplace = append(p.RequiresReplace, path.Root("mode"))
		}
	}
	p.Diagnostics.Append(p.Plan.Set(ctx, &m)...)
}
func pointer(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}
func credit(v types.String) (*float64, error) {
	if pointer(v) == nil {
		return nil, nil
	}
	f, e := strconv.ParseFloat(v.ValueString(), 64)
	return &f, e
}
func (m *Model) update(ctx context.Context) (client.ProjectUpdate, error) {
	var tags []string
	d := m.Tags.ElementsAs(ctx, &tags, false)
	if d.HasError() {
		return client.ProjectUpdate{}, fmt.Errorf("invalid project tags")
	}
	c, e := credit(m.MaxCreditLimit)
	return client.ProjectUpdate{Name: m.Name.ValueString(), Description: pointer(m.Description), TagList: tags, PublicIPv4Shared: m.PublicIPv4Shared.ValueBool(), MaxCreditLimit: c}, e
}
func Map(ctx context.Context, m *Model, p client.Project, env []client.Env) diag.Diagnostics {
	m.ID = types.StringValue(p.ID)
	m.OrganizationID = types.StringValue(p.ClientID)
	m.Name = types.StringValue(p.Name)
	m.Description = types.StringPointerValue(p.Description)
	m.Mode = types.StringValue(p.Mode)
	m.Status = types.StringValue(p.Status)
	m.PublicIPv4Shared = types.BoolValue(p.PublicIPv4Shared)
	m.PublicIPv4 = types.StringPointerValue(p.PublicIPv4)
	m.PublicIPv6 = types.StringPointerValue(p.PublicIPv6)
	m.Location = types.StringValue(p.PrimaryInstanceLocation.ID)
	m.Subdomain = types.StringValue(p.ZeropsSubdomainHost)
	m.Created = types.StringValue(p.Created.Format(time.RFC3339))
	var d diag.Diagnostics
	m.Tags, d = types.SetValueFrom(ctx, types.StringType, p.TagList)
	if m.MaxCreditLimit.IsUnknown() {
		m.MaxCreditLimit = types.StringNull()
	}
	m.EnvIsolation = types.StringNull()
	m.SSHIsolation = types.StringNull()
	for _, e := range env {
		if e.Type == "SYSTEM" {
			switch e.Key {
			case "envIsolation":
				m.EnvIsolation = types.StringValue(e.Content)
			case "sshIsolation":
				m.SSHIsolation = types.StringValue(e.Content)
			}
		}
	}
	return d
}
func (r *Resource) read(ctx context.Context, m *Model) error {
	p, e := r.data.API.GetProject(ctx, m.ID.ValueString())
	if e != nil {
		return e
	}
	if p.Status == "DELETING" {
		return &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}
	}
	env, e := r.data.API.ListProjectEnv(ctx, p.ID)
	if e != nil {
		return e
	}
	d := Map(ctx, m, p, env)
	if d.HasError() {
		return fmt.Errorf("mapping project state failed")
	}
	return nil
}
func (r *Resource) Read(ctx context.Context, q resource.ReadRequest, p *resource.ReadResponse) {
	var m Model
	p.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	e := r.read(ctx, &m)
	if client.IsNotFound(e, "projectNotFound") {
		p.State.RemoveResource(ctx)
		return
	}
	if e != nil {
		p.Diagnostics.AddError("Reading project", e.Error())
		return
	}
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func operation(ctx context.Context, m Model, op string, d *diag.Diagnostics) (context.Context, context.CancelFunc) {
	var t time.Duration
	var ds diag.Diagnostics
	switch op {
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
func (r *Resource) Create(ctx context.Context, q resource.CreateRequest, p *resource.CreateResponse) {
	var m Model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := operation(ctx, m, "create", &p.Diagnostics)
	defer cancel()
	if p.Diagnostics.HasError() {
		return
	}
	v, e := m.update(ctx)
	if e != nil {
		p.Diagnostics.AddError("Creating project", e.Error())
		return
	}
	obj, e := r.data.API.CreateProject(ctx, m.OrganizationID.ValueString(), client.ProjectCreate{Name: v.Name, Description: v.Description, TagList: v.TagList, PublicIPv4Shared: v.PublicIPv4Shared, MaxCreditLimit: v.MaxCreditLimit, Mode: pointer(m.Mode), Location: pointer(m.Location), EnvIsolation: pointer(m.EnvIsolation), SSHIsolation: pointer(m.SSHIsolation), UserRoles: []client.ProjectUserRole{}})
	if e != nil {
		p.Diagnostics.AddError("Creating project", e.Error())
		return
	}
	m.ID = types.StringValue(obj.ID)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
	_, e = waiter.WaitProjectActive(ctx, r.data.API, obj.ID)
	if e == nil {
		e = r.read(ctx, &m)
	}
	if e != nil {
		p.Diagnostics.AddError("Waiting for project", e.Error())
		return
	}
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func ProjectUpdateError(err error, nullCredit bool) string {
	if a, ok := client.AsAPIError(err); ok && a.HTTPStatus == 500 && a.Code == "500" && nullCredit {
		return "project probably has a credit limit set outside Terraform; set max_credit_limit"
	}
	return err.Error()
}
func (r *Resource) Update(ctx context.Context, q resource.UpdateRequest, p *resource.UpdateResponse) {
	var m, old Model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	p.Diagnostics.Append(q.State.Get(ctx, &old)...)
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := operation(ctx, m, "update", &p.Diagnostics)
	defer cancel()
	if p.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	if !m.Name.Equal(old.Name) || !m.Description.Equal(old.Description) || !m.Tags.Equal(old.Tags) || !m.PublicIPv4Shared.Equal(old.PublicIPv4Shared) || !m.MaxCreditLimit.Equal(old.MaxCreditLimit) {
		v, e := m.update(ctx)
		if e != nil {
			p.Diagnostics.AddError("Updating project", e.Error())
			return
		}
		_, e = r.data.API.UpdateProject(ctx, id, v)
		if e != nil {
			p.Diagnostics.AddError("Updating project", ProjectUpdateError(e, v.MaxCreditLimit == nil))
			return
		}
		// The PUT is committed even if a later mutation or read fails. Credit
		// cannot be recovered from the API, so retain it immediately.
		written := old
		written.Name = m.Name
		written.Description = m.Description
		written.Tags = m.Tags
		written.PublicIPv4Shared = m.PublicIPv4Shared
		written.MaxCreditLimit = m.MaxCreditLimit
		p.Diagnostics.Append(p.State.Set(ctx, &written)...)
		if p.Diagnostics.HasError() {
			return
		}
	}
	if !m.Mode.Equal(old.Mode) {
		proc, e := r.data.API.UpgradeProject(ctx, id, m.Mode.ValueString())
		if e == nil && proc != nil {
			_, e = waiter.WaitProcess(ctx, r.data.API, proc.ID)
		}
		if e == nil {
			_, e = waiter.WaitProjectActive(ctx, r.data.API, id)
		}
		if e != nil {
			p.Diagnostics.AddError("Upgrading project", e.Error())
			return
		}
	}
	if !m.EnvIsolation.Equal(old.EnvIsolation) || !m.SSHIsolation.Equal(old.SSHIsolation) {
		unlock, e := r.data.Locks.Lock(ctx, locks.ProjectEnvKey(id))
		if e != nil {
			p.Diagnostics.AddError("Locking project environment", e.Error())
			return
		}
		defer unlock()
		env, e := r.data.API.ListProjectEnv(ctx, id)
		if e != nil {
			p.Diagnostics.AddError("Reading isolation", e.Error())
			return
		}
		for _, setting := range []struct {
			key        string
			value, old types.String
		}{{"envIsolation", m.EnvIsolation, old.EnvIsolation}, {"sshIsolation", m.SSHIsolation, old.SSHIsolation}} {
			if setting.value.Equal(setting.old) || setting.value.IsUnknown() {
				continue
			}
			found := false
			for _, row := range env {
				if row.Type == "SYSTEM" && row.Key == setting.key {
					found = true
					proc, err := r.data.API.UpdateProjectEnv(ctx, row.ID, client.EnvWrite{Key: row.Key, Content: setting.value.ValueString(), Sensitive: false})
					if err == nil {
						_, err = waiter.WaitProcess(ctx, r.data.API, proc.ID)
					}
					if err != nil {
						p.Diagnostics.AddError("Updating isolation", err.Error())
						return
					}
					actual, err := r.data.API.GetProjectEnv(ctx, row.ID)
					if err != nil || actual.Content != setting.value.ValueString() {
						p.Diagnostics.AddError("Verifying isolation", "Updated SYSTEM row did not match desired isolation.")
						return
					}
				}
			}
			if !found {
				p.Diagnostics.AddError("Updating isolation", "SYSTEM isolation row was not found.")
				return
			}
		}
	}
	if e := r.read(ctx, &m); e != nil {
		p.Diagnostics.AddError("Reading updated project", e.Error())
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
	ctx, cancel := operation(ctx, m, "delete", &p.Diagnostics)
	defer cancel()
	if p.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	// An absent object cannot supply the metadata required by guarded DELETE.
	_, e := r.data.API.GetProject(ctx, id)
	if client.IsNotFound(e, "projectNotFound") {
		return
	}
	if e != nil {
		p.Diagnostics.AddError("Reading project before deletion", e.Error())
		return
	}
	proc, e := r.data.API.DeleteProject(ctx, id)
	if client.IsNotFound(e, "projectNotFound") {
		return
	}
	if e == nil {
		_, e = waiter.WaitProcess(ctx, r.data.API, proc.ID)
	}
	if e == nil {
		e = waiter.WaitNotFound(ctx, id, func(ctx context.Context) error { _, e := r.data.API.GetProject(ctx, id); return e }, []string{"projectNotFound"})
	}
	if e != nil {
		p.Diagnostics.AddError("Deleting project", e.Error())
	}
}
func (r *Resource) ImportState(ctx context.Context, q resource.ImportStateRequest, p *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), q, p)
}
