// Package service_git_integration implements the OAuth-dependent connector.
// Registered after its production OAuth lifecycle acceptance passed (Terraform and OpenTofu).
package service_git_integration

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
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
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"regexp"
	"time"
)

type Resource struct{ data *providerdata.ProviderData }
type model struct {
	ID            types.String   `tfsdk:"id"`
	ServiceID     types.String   `tfsdk:"service_id"`
	Provider      types.String   `tfsdk:"git_provider"`
	Repository    types.String   `tfsdk:"repository_full_name"`
	Event         types.String   `tfsdk:"event_type"`
	Branch        types.String   `tfsdk:"branch_name"`
	Tag           types.String   `tfsdk:"tag_regex"`
	Active        types.Bool     `tfsdk:"is_active"`
	Setup         types.String   `tfsdk:"zerops_yaml_setup"`
	User          types.String   `tfsdk:"user_id"`
	Authorization types.Bool     `tfsdk:"authorization_required"`
	Webhook       types.Int64    `tfsdk:"webhook_id"`
	Timeouts      timeouts.Value `tfsdk:"timeouts"`
}

func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_git_integration"
}
func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	nonempty := []validator.String{stringvalidator.LengthAtLeast(1)}
	resp.Schema = schema.Schema{MarkdownDescription: "Owns a service GitHub/GitLab connector, not its OAuth authorization. Requires prior console consent for the bearer user. Reconciliation never triggers a build. Delete removes the connector and its webhook.", Attributes: map[string]schema.Attribute{
		"id":                   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"service_id":           schema.StringAttribute{Required: true, Validators: nonempty, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"git_provider":         schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("github", "gitlab")}},
		"repository_full_name": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`), "Use owner/repository without URL or credentials.")}},
		"event_type":           schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("BRANCH", "TAG")}},
		"branch_name":          schema.StringAttribute{Optional: true, Validators: nonempty}, "tag_regex": schema.StringAttribute{Optional: true, Validators: nonempty},
		"is_active":         schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
		"zerops_yaml_setup": schema.StringAttribute{Optional: true, Validators: nonempty},
		"user_id":           schema.StringAttribute{Computed: true}, "authorization_required": schema.BoolAttribute{Computed: true}, "webhook_id": schema.Int64Attribute{Computed: true},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func validate(m model) error {
	if m.Event.IsUnknown() || m.Branch.IsUnknown() || m.Tag.IsUnknown() {
		return nil
	}
	switch m.Event.ValueString() {
	case "BRANCH":
		if m.Branch.IsNull() || !m.Tag.IsNull() {
			return fmt.Errorf("BRANCH requires branch_name and forbids tag_regex")
		}
	case "TAG":
		if m.Tag.IsNull() || !m.Branch.IsNull() {
			return fmt.Errorf("TAG requires tag_regex and forbids branch_name")
		}
		if _, err := regexp.Compile(m.Tag.ValueString()); err != nil {
			return fmt.Errorf("tag_regex must be a valid regular expression")
		}
	}
	return nil
}
func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m model
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if err := validate(m); err != nil {
		resp.Diagnostics.AddError("Invalid Git integration", err.Error())
	}
}
func ptr(v types.String) *string {
	if v.IsNull() {
		return nil
	}
	s := v.ValueString()
	return &s
}
func (m model) input() client.GitIntegrationStatus {
	g := &client.GitIntegration{RepositoryFullName: m.Repository.ValueString(), EventType: m.Event.ValueString(), BranchName: ptr(m.Branch), TagRegex: ptr(m.Tag), IsActive: m.Active.ValueBool(), ZeropsYamlSetup: ptr(m.Setup)}
	if m.Provider.ValueString() == "github" {
		return client.GitIntegrationStatus{GithubIntegration: g}
	}
	return client.GitIntegrationStatus{GitlabIntegration: g}
}
func (m *model) read(v client.GitIntegrationStatus) error {
	g := v.GithubIntegration
	p := "github"
	if g == nil {
		g = v.GitlabIntegration
		p = "gitlab"
	}
	if g == nil || (v.GithubIntegration != nil && v.GitlabIntegration != nil) {
		return fmt.Errorf("invalid Git integration status: expected exactly one provider")
	}
	m.Provider = types.StringValue(p)
	m.Repository = types.StringValue(g.RepositoryFullName)
	m.Event = types.StringValue(g.EventType)
	m.Branch = types.StringPointerValue(g.BranchName)
	m.Tag = types.StringPointerValue(g.TagRegex)
	m.Active = types.BoolValue(g.IsActive)
	m.Setup = types.StringPointerValue(g.ZeropsYamlSetup)
	m.User = types.StringValue(g.UserID)
	m.Authorization = types.BoolValue(g.AuthorizationRequired)
	m.Webhook = types.Int64PointerValue(g.WebhookID)
	return nil
}
func (r *Resource) write(ctx context.Context, m *model, d *diag.Diagnostics, save func()) {
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ServiceID.ValueString()))
	if err != nil {
		d.AddError("Git integration failed", err.Error())
		return
	}
	defer unlock()
	want := m.input()
	p, err := r.data.API.SetGitIntegration(ctx, m.ServiceID.ValueString(), want)
	if err != nil {
		d.AddError("Git integration failed", err.Error())
		return
	}
	m.ID = m.ServiceID
	save()
	if p != nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, p.ID)
	}
	if err == nil {
		var got client.GitIntegrationStatus
		got, err = waiter.WaitGitIntegration(ctx, r.data.API, m.ID.ValueString(), &want)
		if err == nil {
			err = m.read(got)
		}
	}
	if err != nil {
		d.AddError("Git integration failed", err.Error())
		return
	}
	save()
}
func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	duration, ds := m.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(ds...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	r.write(ctx, &m, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &m)...) })
}
func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	duration, ds := m.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(ds...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	r.write(ctx, &m, &resp.Diagnostics, func() { resp.Diagnostics.Append(resp.State.Set(ctx, &m)...) })
}
func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.data.API.GetGitIntegration(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "noExternalRepositoryIntegration", "serviceStackNotFound") {
		resp.State.RemoveResource(ctx)
		return
	}
	if err == nil {
		err = m.read(got)
	}
	if err != nil {
		resp.Diagnostics.AddError("Read Git integration failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	duration, ds := m.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(ds...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Delete Git integration failed", err.Error())
		return
	}
	defer unlock()
	_, err = r.data.API.GetService(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "serviceStackNotFound") {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading service before deleting Git integration failed", err.Error())
		return
	}
	p, err := r.data.API.SetGitIntegration(ctx, m.ID.ValueString(), client.GitIntegrationStatus{})
	if client.IsNotFound(err, "serviceStackNotFound") {
		return
	}
	if err == nil && p != nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, p.ID)
	}
	if err == nil {
		_, err = waiter.WaitGitIntegration(ctx, r.data.API, m.ID.ValueString(), nil)
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete Git integration failed", err.Error())
	}
}
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_id"), req.ID)...)
}
