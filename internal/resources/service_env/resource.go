package service_env

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
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

type Resource struct{ data *providerdata.ProviderData }
type model struct {
	ID        types.String   `tfsdk:"id"`
	ServiceID types.String   `tfsdk:"service_id"`
	Key       types.String   `tfsdk:"key"`
	Content   types.String   `tfsdk:"content"`
	Sensitive types.Bool     `tfsdk:"sensitive"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_env"
}
func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Owns one USER environment row. Changes do not request a restart or deployment. Sensitive content is retained in Terraform state when reveal is forbidden.", Attributes: map[string]schema.Attribute{
		"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical user-data ID, not a process ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"service_id": schema.StringAttribute{Required: true, MarkdownDescription: "Parent service ID.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"key":        schema.StringAttribute{Required: true, MarkdownDescription: "Environment variable key; rename preserves the row ID.", Validators: []validator.String{validators.EnvKey()}},
		"content":    schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Environment text, at most 8192 ASCII characters without CR or LF.", Validators: []validator.String{envContent{}}},
		"sensitive":  schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Whether the API masks this value."},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m model
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	for _, value := range m.Timeouts.Object.Attributes() {
		if v, ok := value.(types.String); ok && !v.IsNull() && !v.IsUnknown() {
			duration, err := time.ParseDuration(v.ValueString())
			if err == nil && duration <= 0 {
				resp.Diagnostics.AddError("Invalid timeout", "Timeout durations must be positive.")
			}
		}
	}
}

type envContent struct{}

func (envContent) Description(context.Context) string {
	return "must contain at most 8192 ASCII characters and no CR/LF"
}
func (v envContent) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v envContent) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	invalid := len(s) > 8192
	for _, c := range s {
		if c > 127 || c == '\r' || c == '\n' {
			invalid = true
		}
	}
	if invalid {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid environment content", v.Description(ctx))
	}
}
func owned(e client.Env) bool { return e.Type == "USER" && e.Editable && !e.Internal }
func lookup(ctx context.Context, api client.API, service, key string) (client.Env, error) {
	var found []client.Env
	for offset := 0; ; {
		p, err := api.ListUserData(ctx, service, client.UserDataQuery{PageQuery: client.PageQuery{Offset: offset, Limit: 100}, KeyContains: key})
		if err != nil {
			return client.Env{}, err
		}
		for _, e := range p.Items {
			if strings.EqualFold(e.Key, key) && owned(e) {
				found = append(found, e)
			}
		}
		if len(p.Items) == 0 || offset+len(p.Items) >= p.Total {
			break
		}
		offset += len(p.Items)
	}
	if len(found) != 1 {
		return client.Env{}, fmt.Errorf("expected exactly one editable, noninternal USER row for the key; found %d", len(found))
	}
	return found[0], nil
}
func (r *Resource) refresh(ctx context.Context, m *model, d *diag.Diagnostics) (bool, error) {
	e, err := r.data.API.GetUserData(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "userDataNotFound") {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if !owned(e) {
		return true, fmt.Errorf("refusing ownership of a non-USER, noneditable or internal row (including SYSTEM PATH)")
	}
	m.ID = types.StringValue(e.ID)
	m.ServiceID = types.StringValue(e.ServiceStackID)
	m.Key = types.StringValue(e.Key)
	m.Sensitive = types.BoolValue(e.Sensitive)
	if e.Sensitive {
		content, err := r.data.API.RevealUserData(ctx, e.ID)
		if err != nil {
			if apierr, ok := client.AsAPIError(err); ok && apierr.HTTPStatus == 403 && apierr.Code == "notInSudoMode" {
				if m.Content.IsNull() || m.Content.IsUnknown() {
					return true, fmt.Errorf("sensitive content cannot be recovered; import requires a sudo-capable token")
				}
				d.AddWarning("Sensitive content could not be refreshed", "Reveal was forbidden. Retaining the known Terraform content; external secret drift cannot be detected.")
				return true, nil
			}
			return true, err
		}
		e.Content = content
	}
	m.Content = types.StringValue(e.Content)
	return true, nil
}
func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Create(ctx, 10*time.Minute)
	resp.Diagnostics.Append(d...)
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ServiceID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Lock failed", err.Error())
		return
	}
	defer unlock()
	p, err := r.data.API.CreateUserData(ctx, m.ServiceID.ValueString(), client.EnvWrite{Key: m.Key.ValueString(), Content: m.Content.ValueString(), Sensitive: m.Sensitive.ValueBool()})
	if err == nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, p.ID)
	}
	if err == nil {
		var e client.Env
		e, err = lookup(ctx, r.data.API, m.ServiceID.ValueString(), m.Key.ValueString())
		m.ID = types.StringValue(e.ID)
	}
	if err != nil {
		resp.Diagnostics.AddError("Creating service environment failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	_, err = r.refresh(ctx, &m, &resp.Diagnostics)
	if err != nil {
		resp.Diagnostics.AddError("Reading service environment failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	exists, err := r.refresh(ctx, &m, &resp.Diagnostics)
	if err != nil {
		resp.Diagnostics.AddError("Reading service environment failed", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Update(ctx, 10*time.Minute)
	resp.Diagnostics.Append(d...)
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ServiceID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Lock failed", err.Error())
		return
	}
	defer unlock()
	p, err := r.data.API.UpdateUserData(ctx, m.ID.ValueString(), client.EnvWrite{Key: m.Key.ValueString(), Content: m.Content.ValueString(), Sensitive: m.Sensitive.ValueBool()})
	if err == nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, p.ID)
	}
	if err == nil {
		_, err = r.refresh(ctx, &m, &resp.Diagnostics)
	}
	if err != nil {
		resp.Diagnostics.AddError("Updating service environment failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Delete(ctx, 10*time.Minute)
	resp.Diagnostics.Append(d...)
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ServiceID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Lock failed", err.Error())
		return
	}
	defer unlock()
	// Prove absence before a guarded mutation needs the row's ownership metadata.
	_, err = r.data.API.GetUserData(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "userDataNotFound") {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading service environment before deletion", err.Error())
		return
	}
	p, err := r.data.API.DeleteUserData(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "userDataNotFound") {
		return
	}
	if err == nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, p.ID)
	}
	if err == nil {
		err = waiter.WaitNotFound(ctx, m.ID.ValueString(), func(ctx context.Context) error { _, e := r.data.API.GetUserData(ctx, m.ID.ValueString()); return e }, []string{"userDataNotFound"})
	}
	if err != nil {
		resp.Diagnostics.AddError("Deleting service environment failed", err.Error())
	}
}
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	m := model{ID: types.StringValue(req.ID), Content: types.StringNull(), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	if service, key, ok := strings.Cut(req.ID, "/"); ok {
		e, err := lookup(ctx, r.data.API, service, key)
		if err != nil {
			resp.Diagnostics.AddError("Import lookup failed", err.Error())
			return
		}
		m.ID = types.StringValue(e.ID)
	}
	exists, err := r.refresh(ctx, &m, &resp.Diagnostics)
	if err != nil || !exists {
		if err == nil {
			err = fmt.Errorf("user-data row does not exist")
		}
		resp.Diagnostics.AddError("Import failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
