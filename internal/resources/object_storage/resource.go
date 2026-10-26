package object_storage

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	ID         types.String         `tfsdk:"id"`
	ProjectID  types.String         `tfsdk:"project_id"`
	Hostname   types.String         `tfsdk:"hostname"`
	Quota      types.Int64          `tfsdk:"quota_gb"`
	CDNEnabled types.Bool           `tfsdk:"cdn_enabled"`
	Policy     types.String         `tfsdk:"policy"`
	Raw        jsontypes.Normalized `tfsdk:"raw_policy"`
	Bucket     types.String         `tfsdk:"bucket_name"`
	URL        types.String         `tfsdk:"api_url"`
	Access     types.String         `tfsdk:"access_key_id"`
	Secret     types.String         `tfsdk:"secret_access_key"`
	Timeouts   timeouts.Value       `tfsdk:"timeouts"`
}

func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_storage"
}
func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Owns an object-storage bucket service. Deletion destroys bucket data. Quota can grow or shrink subject to server usage constraints. Custom policies support native website JSON, not arbitrary AWS policy semantics. CDN enable may fail in some regions; asynchronous failures are surfaced and the bucket is retained.", Attributes: map[string]schema.Attribute{
		"id":                schema.StringAttribute{Computed: true, MarkdownDescription: "Storage service ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id":        schema.StringAttribute{Required: true, MarkdownDescription: "Parent project ID.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"hostname":          schema.StringAttribute{Required: true, MarkdownDescription: "Service hostname; distinct from generated bucket name.", Validators: []validator.String{validators.Hostname()}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"quota_gb":          schema.Int64Attribute{Required: true, MarkdownDescription: "Allocated quota in GB, from 1 to 100; supports grow and shrink.", Validators: []validator.Int64{int64validator.Between(1, 100)}},
		"cdn_enabled":       schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Enable the storage CDN through its dedicated asynchronous operation. Defaults to false. Backend enable failures are errors, not availability guarantees."},
		"policy":            schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Policy: private (default), public-read, public-objects-read, public-write, public-read-write, or custom. Custom is inferred when raw_policy is supplied.", Validators: []validator.String{stringvalidator.OneOf("private", "public-read", "public-objects-read", "public-write", "public-read-write", "custom")}},
		"raw_policy":        schema.StringAttribute{Optional: true, CustomType: jsontypes.NormalizedType{}, MarkdownDescription: "Custom policy JSON object, compared semantically. Native websiteAccess/indexDocument/errorDocument round-trip through the API; discarded fields produce an error.", Validators: []validator.String{jsonObject{}}},
		"bucket_name":       schema.StringAttribute{Computed: true, MarkdownDescription: "Generated bucket name."},
		"api_url":           schema.StringAttribute{Computed: true, MarkdownDescription: "Object-storage API URL."},
		"access_key_id":     schema.StringAttribute{Computed: true, MarkdownDescription: "Generated access key ID."},
		"secret_access_key": schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Generated secret access key, null when masked and not recoverable."},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}

type jsonObject struct{}

func (jsonObject) Description(context.Context) string               { return "must be a valid JSON object" }
func (v jsonObject) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v jsonObject) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(req.ConfigValue.ValueString()), &obj) != nil || obj == nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid custom policy", v.Description(ctx))
	}
}
func policyError(policy types.String, raw jsontypes.Normalized) error {
	if policy.IsUnknown() || raw.IsUnknown() {
		return nil
	}
	if !raw.IsNull() && !policy.IsNull() && policy.ValueString() != "custom" {
		return fmt.Errorf("raw_policy conflicts with a noncustom policy")
	}
	if policy.ValueString() == "custom" && raw.IsNull() {
		return fmt.Errorf("custom policy requires raw_policy")
	}
	return nil
}
func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m model
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if err := policyError(m.Policy, m.Raw); err != nil {
		resp.Diagnostics.AddError("Invalid storage policy", err.Error())
	}
	for _, value := range m.Timeouts.Object.Attributes() {
		if v, ok := value.(types.String); ok && !v.IsNull() && !v.IsUnknown() {
			duration, err := time.ParseDuration(v.ValueString())
			if err == nil && duration <= 0 {
				resp.Diagnostics.AddError("Invalid timeout", "Timeout durations must be positive.")
			}
		}
	}
}
func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var m, config model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Optional+Computed planning may already have merged the old policy.
	// Derive an omitted policy from configuration, never from that prior value.
	if config.Policy.IsNull() {
		switch {
		case config.Raw.IsUnknown():
			m.Policy = types.StringUnknown()
		case !config.Raw.IsNull():
			m.Policy = types.StringValue("custom")
		default:
			m.Policy = types.StringValue("private")
		}
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &m)...)
	}
}
func semanticEqual(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// Only canonical default projections identify private storage on import.
// Additional fields must remain custom, even when they grant no access.
func canonicalPrivatePolicy(raw string) bool {
	var policy map[string]any
	if json.Unmarshal([]byte(raw), &policy) != nil || policy == nil {
		return false
	}
	if len(policy) == 2 && policy["Version"] == "2012-10-17" {
		statements, ok := policy["Statement"].([]any)
		return ok && len(statements) == 0
	}
	access, ok := policy["websiteAccess"].(bool)
	if !ok || access || policy["indexDocument"] != nil || policy["errorDocument"] != nil {
		return false
	}
	for key := range policy {
		switch key {
		case "websiteAccess", "indexDocument", "errorDocument":
		default:
			return false
		}
	}
	return true
}
func (r *Resource) refresh(ctx context.Context, m *model, d *diag.Diagnostics, verify bool) (bool, error) {
	s, err := r.data.API.GetService(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "serviceStackNotFound") {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if s.ServiceStackTypeID != "object_storage" && s.Base != "object-storage" {
		return true, fmt.Errorf("service is not an object-storage stack")
	}
	info, err := r.data.API.GetObjectStorage(ctx, s.ID)
	if err != nil {
		return true, err
	}
	env, err := r.data.API.ListServiceEnv(ctx, s.ID)
	if err != nil {
		return true, err
	}
	values := map[string]client.Env{}
	for _, e := range env {
		if e.Type == "SYSTEM" {
			values[e.Key] = e
		}
	}
	quota, err := strconv.ParseInt(values["quotaGBytes"].Content, 10, 64)
	if err != nil {
		return true, fmt.Errorf("invalid SYSTEM quotaGBytes: expected an integer")
	}
	if verify && quota != m.Quota.ValueInt64() {
		return true, fmt.Errorf("object-storage quota mismatch after completed write")
	}
	if verify && !m.Raw.IsNull() && !m.Raw.IsUnknown() && !semanticEqual(m.Raw.ValueString(), info.RawPolicy) {
		return true, fmt.Errorf("custom policy mismatch after completed write: server discarded or changed configured fields; configured policy JSON must round-trip semantically")
	}
	m.ID = types.StringValue(s.ID)
	if s.CDNEnabled == nil {
		return true, fmt.Errorf("storage response missing cdnEnabled")
	}
	m.CDNEnabled = types.BoolValue(*s.CDNEnabled)
	m.ProjectID = types.StringValue(s.ProjectID)
	m.Hostname = types.StringValue(s.Name)
	m.Quota = types.Int64Value(quota)
	m.Bucket = types.StringValue(values["bucketName"].Content)
	m.URL = types.StringValue(values["apiUrl"].Content)
	m.Access = types.StringValue(values["accessKeyId"].Content)
	m.Secret = types.StringNull()
	secret, ok := values["secretAccessKey"]
	if ok {
		if secret.Sensitive {
			value, e := r.data.API.RevealUserData(ctx, secret.ID)
			if e == nil {
				m.Secret = types.StringValue(value)
			} else if apierr, ok := client.AsAPIError(e); !ok || apierr.HTTPStatus != 403 || apierr.Code != "notInSudoMode" {
				return true, e
			}
		} else if secret.Content != "REDACTED" {
			m.Secret = types.StringValue(secret.Content)
		}
	}
	if m.Policy.IsNull() || m.Policy.IsUnknown() {
		if canonicalPrivatePolicy(info.RawPolicy) {
			m.Policy = types.StringValue("private")
			m.Raw = jsontypes.NewNormalizedNull()
		} else {
			m.Policy = types.StringValue("custom")
			m.Raw = jsontypes.NewNormalizedValue(info.RawPolicy)
			d.AddWarning("Original policy cannot be recovered", "Only canonical default private projections can be identified reliably. Imported observed policy JSON as custom; original public policy enums may be indistinguishable. Review configuration.")
		}
	} else if m.Policy.ValueString() == "custom" {
		m.Raw = jsontypes.NewNormalizedValue(info.RawPolicy)
	}
	return true, nil
}
func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.CDNEnabled.IsUnknown() || m.CDNEnabled.IsNull() {
		resp.Diagnostics.AddError("Unknown CDN configuration", "cdn_enabled must be known before applying.")
		return
	}
	duration, d := m.Timeouts.Create(ctx, 10*time.Minute)
	resp.Diagnostics.Append(d...)
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}
	policy := m.Policy.ValueString()
	cdn := false
	var raw *string
	if !m.Raw.IsNull() {
		v := m.Raw.ValueString()
		raw = &v
	}
	s, err := r.data.API.CreateObjectStorage(ctx, m.ProjectID.ValueString(), client.StorageCreate{Name: m.Hostname.ValueString(), DiskGBytes: int(m.Quota.ValueInt64()), Policy: &policy, RawPolicy: raw, CDNEnabled: &cdn})
	if err != nil {
		resp.Diagnostics.AddError("Creating storage failed", err.Error())
		return
	}
	m.ID = types.StringValue(s.ID)
	// Retain the created identity for cleanup without persisting planned unknown
	// outputs if readiness or readback verification fails before refresh maps them.
	m.Bucket = types.StringNull()
	m.URL = types.StringNull()
	m.Access = types.StringNull()
	m.Secret = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	unlock, lockErr := r.data.Locks.Lock(ctx, locks.ServiceKey(s.ID))
	if lockErr != nil {
		resp.Diagnostics.AddError("Lock failed", lockErr.Error())
		return
	}
	defer unlock()
	if s.Process != nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, s.Process.ID)
	}
	if err == nil {
		_, err = waiter.WaitServiceSettled(ctx, r.data.API, s.ID, []string{"ACTIVE"}, nil, nil)
	}
	if err == nil && m.CDNEnabled.ValueBool() {
		err = r.setCDN(ctx, s.ID, true)
		if err != nil {
			_, readErr := r.refresh(ctx, &m, &resp.Diagnostics, false)
			if readErr != nil {
				resp.Diagnostics.AddError("Reading storage after CDN failure failed", readErr.Error())
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		}
	}
	if err == nil {
		_, err = r.refresh(ctx, &m, &resp.Diagnostics, true)
	}
	if err != nil {
		resp.Diagnostics.AddError("Creating storage failed", err.Error())
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
	exists, err := r.refresh(ctx, &m, &resp.Diagnostics, false)
	if err != nil {
		resp.Diagnostics.AddError("Reading storage failed", err.Error())
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
	var prior model
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.CDNEnabled.IsUnknown() || m.CDNEnabled.IsNull() {
		resp.Diagnostics.AddError("Unknown CDN configuration", "cdn_enabled must be known before applying.")
		return
	}
	duration, d := m.Timeouts.Update(ctx, 10*time.Minute)
	resp.Diagnostics.Append(d...)
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Lock failed", err.Error())
		return
	}
	defer unlock()
	policy := m.Policy.ValueString()
	var raw *string
	if !m.Raw.IsNull() {
		v := m.Raw.ValueString()
		raw = &v
	}
	p, err := r.data.API.UpdateObjectStorage(ctx, m.ID.ValueString(), client.StorageWrite{DiskGBytes: int(m.Quota.ValueInt64()), Policy: &policy, RawPolicy: raw, CDNEnabled: nil})
	if err == nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, p.ID)
	}
	if err == nil && !m.CDNEnabled.Equal(prior.CDNEnabled) {
		err = r.setCDN(ctx, m.ID.ValueString(), m.CDNEnabled.ValueBool())
		if err != nil {
			_, readErr := r.refresh(ctx, &m, &resp.Diagnostics, false)
			if readErr != nil {
				resp.Diagnostics.AddError("Reading storage after CDN failure failed", readErr.Error())
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		}
	}
	if err == nil {
		_, err = r.refresh(ctx, &m, &resp.Diagnostics, true)
	}
	if err != nil {
		resp.Diagnostics.AddError("Updating storage failed", err.Error())
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
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Lock failed", err.Error())
		return
	}
	defer unlock()
	// Prove absence with a read rather than attempting an unownable mutation.
	_, err = r.data.API.GetService(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "serviceStackNotFound") {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading storage before deletion", err.Error())
		return
	}
	p, err := r.data.API.DeleteService(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "serviceStackNotFound") {
		return
	}
	if err == nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, p.ID)
	}
	if err == nil {
		err = waiter.WaitNotFound(ctx, m.ID.ValueString(), func(ctx context.Context) error { _, e := r.data.API.GetService(ctx, m.ID.ValueString()); return e }, []string{"serviceStackNotFound"})
	}
	if err != nil {
		resp.Diagnostics.AddError("Deleting storage failed", err.Error())
	}
}
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	m := model{ID: types.StringValue(req.ID), Policy: types.StringNull(), Raw: jsontypes.NewNormalizedNull(), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	exists, err := r.refresh(ctx, &m, &resp.Diagnostics, false)
	if err != nil || !exists {
		if err == nil {
			err = fmt.Errorf("storage service does not exist")
		}
		resp.Diagnostics.AddError("Import failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *Resource) setCDN(ctx context.Context, id string, enabled bool) error {
	p, err := r.data.API.SetStorageCDN(ctx, id, enabled)
	if err == nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, p.ID, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute))
	}
	if err == nil {
		_, err = waiter.WaitStorageCDN(ctx, r.data.API, id, enabled)
	}
	return err
}
