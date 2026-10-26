package integration_token

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

type Resource struct{ data *providerdata.ProviderData }
type model struct {
	ID                types.String   `tfsdk:"id"`
	OrganizationID    types.String   `tfsdk:"organization_id"`
	Name              types.String   `tfsdk:"name"`
	RoleCode          types.String   `tfsdk:"role_code"`
	Projects          types.Set      `tfsdk:"projects"`
	CanCreateProjects types.Bool     `tfsdk:"can_create_projects"`
	CanViewFinances   types.Bool     `tfsdk:"can_view_finances"`
	CanEditFinances   types.Bool     `tfsdk:"can_edit_finances"`
	Sudo              types.Bool     `tfsdk:"sudo"`
	RotationTrigger   types.Map      `tfsdk:"rotation_trigger"`
	Token             types.String   `tfsdk:"token"`
	Timeouts          timeouts.Value `tfsdk:"timeouts"`
}
type projectModel struct {
	ProjectID types.String `tfsdk:"project_id"`
	RoleCode  types.String `tfsdk:"role_code"`
}

var projectType = types.ObjectType{AttrTypes: map[string]attr.Type{"project_id": types.StringType, "role_code": types.StringType}}

func NewResource() resource.Resource { return &Resource{} }
func (*Resource) Metadata(_ context.Context, q resource.MetadataRequest, p *resource.MetadataResponse) {
	p.TypeName = q.ProviderTypeName + "_integration_token"
}
func (r *Resource) Configure(_ context.Context, q resource.ConfigureRequest, p *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(q, p)
}
func roleValidators() []validator.String {
	return []validator.String{stringvalidator.OneOf("OWNER", "ADMIN", "BASIC_USER", "READ_ONLY", "NO_ACCESS")}
}
func (*Resource) Schema(ctx context.Context, _ resource.SchemaRequest, p *resource.SchemaResponse) {
	p.Schema = schema.Schema{MarkdownDescription: "Manages an integration token using a separate, pre-authorized administration credential. The sensitive token is stored in Terraform state: protect state and backups. Import cannot recover the secret; change rotation_trigger to regenerate it. Rotation immediately invalidates the old bearer. sudo is metadata, not a guarantee of effective sensitive-read permission.", Attributes: map[string]schema.Attribute{
		"id":              schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"organization_id": schema.StringAttribute{Optional: true, Computed: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}},
		"name":            schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"role_code":       schema.StringAttribute{Required: true, Validators: roleValidators()},
		"projects": schema.SetNestedAttribute{Required: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}}, "role_code": schema.StringAttribute{Required: true, Validators: roleValidators()}}}},
		"can_create_projects": schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "When omitted, true for ADMIN/OWNER and false otherwise. Explicit false is invalid for ADMIN/OWNER."},
		"can_view_finances":   schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
		"can_edit_finances":   schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
		"sudo":                schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
		"rotation_trigger":    schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType, Default: mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{})), MarkdownDescription: "Changing this map rotates the secret in place. Do not put secrets in this map."},
		"token":               schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Create/rotation-only secret retained across refresh. Null after import until explicit rotation."},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func privileged(role types.String) bool {
	return role.ValueString() == "ADMIN" || role.ValueString() == "OWNER"
}
func deriveCreate(role types.String, configured types.Bool) types.Bool {
	if !configured.IsNull() {
		return configured
	}
	if role.IsUnknown() || role.IsNull() {
		return types.BoolUnknown()
	}
	return types.BoolValue(privileged(role))
}
func (r *Resource) ValidateConfig(ctx context.Context, q resource.ValidateConfigRequest, p *resource.ValidateConfigResponse) {
	var m model
	p.Diagnostics.Append(q.Config.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	if privileged(m.RoleCode) && !m.CanCreateProjects.IsNull() && !m.CanCreateProjects.IsUnknown() && !m.CanCreateProjects.ValueBool() {
		p.Diagnostics.AddAttributeError(path.Root("can_create_projects"), "Invalid project creation permission", "ADMIN and OWNER require can_create_projects = true; omit it to derive the role default.")
	}
	if m.Projects.IsNull() || m.Projects.IsUnknown() {
		return
	}
	var projects []projectModel
	p.Diagnostics.Append(m.Projects.ElementsAs(ctx, &projects, false)...)
	seen := map[string]bool{}
	for _, v := range projects {
		if v.ProjectID.IsUnknown() {
			continue
		}
		id := v.ProjectID.ValueString()
		if seen[id] {
			p.Diagnostics.AddAttributeError(path.Root("projects"), "Duplicate project scope", "Each project_id must appear only once.")
		}
		seen[id] = true
	}
}
func (r *Resource) ModifyPlan(ctx context.Context, q resource.ModifyPlanRequest, p *resource.ModifyPlanResponse) {
	if q.Plan.Raw.IsNull() {
		return
	}
	var m model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	var configured types.Bool
	var org types.String
	p.Diagnostics.Append(q.Config.GetAttribute(ctx, path.Root("can_create_projects"), &configured)...)
	p.Diagnostics.Append(q.Config.GetAttribute(ctx, path.Root("organization_id"), &org)...)
	if p.Diagnostics.HasError() {
		return
	}
	m.CanCreateProjects = deriveCreate(m.RoleCode, configured)
	if org.IsNull() && r.data != nil {
		m.OrganizationID = types.StringValue(r.data.OrganizationID)
	}
	m.Token = types.StringUnknown()
	if !q.State.Raw.IsNull() {
		var old model
		p.Diagnostics.Append(q.State.Get(ctx, &old)...)
		if !m.RotationTrigger.IsUnknown() && m.RotationTrigger.Equal(old.RotationTrigger) {
			m.Token = old.Token
		}
	}
	p.Diagnostics.Append(p.Plan.Set(ctx, &m)...)
}
func (m *model) payload(ctx context.Context) (client.IntegrationTokenWrite, diag.Diagnostics) {
	var projects []projectModel
	d := m.Projects.ElementsAs(ctx, &projects, false)
	scopes := make([]client.IntegrationTokenProject, 0, len(projects))
	for _, p := range projects {
		scopes = append(scopes, client.IntegrationTokenProject{ProjectID: p.ProjectID.ValueString(), RoleCode: p.RoleCode.ValueString()})
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].ProjectID < scopes[j].ProjectID })
	return client.IntegrationTokenWrite{Name: m.Name.ValueString(), RoleCode: m.RoleCode.ValueString(), Projects: scopes, CanCreateProjects: m.CanCreateProjects.ValueBool(), CanViewFinances: m.CanViewFinances.ValueBool(), CanEditFinances: m.CanEditFinances.ValueBool(), Sudo: m.Sudo.ValueBool()}, d
}
func mapMetadata(ctx context.Context, m *model, v client.IntegrationToken) diag.Diagnostics {
	m.Name = types.StringValue(v.Name)
	m.RoleCode = types.StringValue(v.RoleCode)
	m.CanCreateProjects = types.BoolValue(v.CanCreateProjects)
	m.CanViewFinances = types.BoolValue(v.CanViewFinances)
	m.CanEditFinances = types.BoolValue(v.CanEditFinances)
	m.Sudo = types.BoolValue(v.Sudo)
	ps := make([]projectModel, 0, len(v.Projects))
	for _, p := range v.Projects {
		ps = append(ps, projectModel{types.StringValue(p.ProjectID), types.StringValue(p.RoleCode)})
	}
	var d diag.Diagnostics
	m.Projects, d = types.SetValueFrom(ctx, projectType, ps)
	return d
}
func tokenID(m *model) string { _, id, _ := strings.Cut(m.ID.ValueString(), "/"); return id }
func tokenError(d *diag.Diagnostics, action string, err error, secret bool) {
	detail := err.Error() + " Token administration requires a pre-authorized owner/session or delegated administration credential; an application token may not create unrelated tokens."
	if secret {
		detail += " This operation was not retried. If the server accepted it before a transport failure, its secret cannot be recovered with GET; inspect token metadata before taking explicit recovery action."
	}
	d.AddError(action, detail)
}
func (r *Resource) Read(ctx context.Context, q resource.ReadRequest, p *resource.ReadResponse) {
	var m model
	p.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	v, e := r.data.API.GetIntegrationToken(ctx, m.OrganizationID.ValueString(), tokenID(&m))
	if client.IsNotFound(e, "clientUserConnectionNotFound") {
		p.State.RemoveResource(ctx)
		return
	}
	if e != nil {
		tokenError(&p.Diagnostics, "Reading integration token", e, false)
		return
	}
	p.Diagnostics.Append(mapMetadata(ctx, &m, v)...)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func (r *Resource) Create(ctx context.Context, q resource.CreateRequest, p *resource.CreateResponse) {
	var m model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Create(ctx, 10*time.Minute)
	p.Diagnostics.Append(d...)
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	payload, d := m.payload(ctx)
	p.Diagnostics.Append(d...)
	if p.Diagnostics.HasError() {
		return
	}
	v, e := r.data.API.CreateIntegrationToken(ctx, m.OrganizationID.ValueString(), payload)
	if v.ID != "" {
		m.ID = types.StringValue(m.OrganizationID.ValueString() + "/" + v.ID)
		m.Token = types.StringValue(v.Token)
		p.Diagnostics.Append(p.State.Set(ctx, &m)...)
	}
	if e != nil {
		tokenError(&p.Diagnostics, "Creating integration token", e, true)
		return
	}
	if v.ID == "" || v.Token == "" {
		p.Diagnostics.AddError("Incomplete integration token response", "Create did not return both identity and secret; GET cannot recover the secret. The operation was not retried.")
		return
	}
	p.Diagnostics.Append(mapMetadata(ctx, &m, v.IntegrationToken)...)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func (r *Resource) Update(ctx context.Context, q resource.UpdateRequest, p *resource.UpdateResponse) {
	var m, old model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	p.Diagnostics.Append(q.State.Get(ctx, &old)...)
	if p.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Update(ctx, 10*time.Minute)
	p.Diagnostics.Append(d...)
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	unlock, e := r.data.Locks.Lock(ctx, "token:"+m.OrganizationID.ValueString()+":"+tokenID(&m))
	if e != nil {
		tokenError(&p.Diagnostics, "Locking integration token", e, false)
		return
	}
	defer unlock()
	desired, d := m.payload(ctx)
	p.Diagnostics.Append(d...)
	previous, d := old.payload(ctx)
	p.Diagnostics.Append(d...)
	if p.Diagnostics.HasError() {
		return
	}
	rotate := !m.RotationTrigger.Equal(old.RotationTrigger)
	m.Token = old.Token
	if !reflect.DeepEqual(desired, previous) {
		_, e = r.data.API.UpdateIntegrationToken(ctx, m.OrganizationID.ValueString(), tokenID(&m), desired)
		if e != nil {
			tokenError(&p.Diagnostics, "Updating integration token", e, false)
			return
		}
	}
	// Until regeneration succeeds the old trigger and secret still describe reality.
	trigger := m.RotationTrigger
	m.RotationTrigger = old.RotationTrigger
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
	if rotate {
		v, err := r.data.API.RegenerateIntegrationToken(ctx, m.OrganizationID.ValueString(), tokenID(&m))
		if v.Token != "" {
			m.Token = types.StringValue(v.Token)
			m.RotationTrigger = trigger
			p.Diagnostics.Append(p.State.Set(ctx, &m)...)
		}
		if err != nil {
			tokenError(&p.Diagnostics, "Rotating integration token", err, true)
			return
		}
		if v.Token == "" {
			p.Diagnostics.AddError("Missing regenerated secret", "Regeneration returned no secret. GET cannot recover it; the operation was not retried.")
			return
		}
	}
	v, e := r.data.API.GetIntegrationToken(ctx, m.OrganizationID.ValueString(), tokenID(&m))
	if e != nil {
		tokenError(&p.Diagnostics, "Reading updated integration token", e, false)
		return
	}
	p.Diagnostics.Append(mapMetadata(ctx, &m, v)...)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func (r *Resource) Delete(ctx context.Context, q resource.DeleteRequest, p *resource.DeleteResponse) {
	var m model
	p.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Delete(ctx, 10*time.Minute)
	p.Diagnostics.Append(d...)
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	unlock, e := r.data.Locks.Lock(ctx, "token:"+m.OrganizationID.ValueString()+":"+tokenID(&m))
	if e != nil {
		tokenError(&p.Diagnostics, "Locking integration token", e, false)
		return
	}
	defer unlock()
	_, e = r.data.API.GetIntegrationToken(ctx, m.OrganizationID.ValueString(), tokenID(&m))
	if client.IsNotFound(e, "clientUserConnectionNotFound") {
		return
	}
	if e != nil {
		tokenError(&p.Diagnostics, "Reading integration token before deletion", e, false)
		return
	}
	e = r.data.API.DeleteIntegrationToken(ctx, m.OrganizationID.ValueString(), tokenID(&m))
	if client.IsNotFound(e, "clientUserConnectionNotFound") {
		return
	}
	if e != nil {
		tokenError(&p.Diagnostics, "Deleting integration token", e, false)
		return
	}
	e = waiter.WaitNotFound(ctx, m.ID.ValueString(), func(ctx context.Context) error {
		_, e := r.data.API.GetIntegrationToken(ctx, m.OrganizationID.ValueString(), tokenID(&m))
		return e
	}, []string{"clientUserConnectionNotFound"})
	if e != nil {
		tokenError(&p.Diagnostics, "Verifying token deletion", e, false)
	}
}
func (r *Resource) ImportState(ctx context.Context, q resource.ImportStateRequest, p *resource.ImportStateResponse) {
	org, id, ok := strings.Cut(q.ID, "/")
	if !ok || org == "" || id == "" || strings.Contains(id, "/") {
		p.Diagnostics.AddError("Invalid import ID", "Expected organization_id/token_id.")
		return
	}
	m := model{ID: types.StringValue(q.ID), OrganizationID: types.StringValue(org), Token: types.StringNull(), RotationTrigger: types.MapValueMust(types.StringType, map[string]attr.Value{}), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	v, e := r.data.API.GetIntegrationToken(ctx, org, id)
	if e != nil {
		tokenError(&p.Diagnostics, "Importing integration token", e, false)
		return
	}
	p.Diagnostics.Append(mapMetadata(ctx, &m, v)...)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
	p.Diagnostics.AddWarning("Integration token secret unavailable", "secret cannot be recovered; change rotation_trigger to regenerate")
}

var _ resource.ResourceWithModifyPlan = (*Resource)(nil)
var _ resource.ResourceWithValidateConfig = (*Resource)(nil)
