package public_http_routing

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/routing"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

type Location struct {
	Path      types.String `tfsdk:"path"`
	Port      types.Int64  `tfsdk:"port"`
	ServiceID types.String `tfsdk:"service_id"`
	Config    types.Object `tfsdk:"config"`
}
type DomainStatus struct {
	Domain         types.String `tfsdk:"domain"`
	DNSCheckStatus types.String `tfsdk:"dns_check_status"`
	SSLStatus      types.String `tfsdk:"ssl_status"`
	CDNStatus      types.String `tfsdk:"cdn_status"`
}
type Model struct {
	ID             types.String   `tfsdk:"id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	Domains        types.Set      `tfsdk:"domains"`
	SSLEnabled     types.Bool     `tfsdk:"ssl_enabled"`
	CDNEnabled     types.Bool     `tfsdk:"cdn_enabled"`
	WaitForSSL     types.Bool     `tfsdk:"wait_for_ssl"`
	SSLTimeout     types.String   `tfsdk:"ssl_timeout"`
	Location       []Location     `tfsdk:"location"`
	DomainStatuses types.List     `tfsdk:"domain_statuses"`
	IsSynced       types.Bool     `tfsdk:"is_synced"`
	ForceSync      types.Bool     `tfsdk:"force_sync"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

var statusType = types.ObjectType{AttrTypes: map[string]attr.Type{"domain": types.StringType, "dns_check_status": types.StringType, "ssl_status": types.StringType, "cdn_status": types.StringType}}

func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Version: 0, MarkdownDescription: "Editable custom HTTP routing with optional managed SSL readiness. DNS is managed externally; the provider has no Cloudflare dependency. SSL acceptance fixtures require ZEROPS_ACC_DNS=1 and tools/cloudflare-dns.sh credentials. Foreign staged project routes are refused unless forced.", Attributes: map[string]schema.Attribute{
		"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "Stable editable routing ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id":   schema.StringAttribute{Required: true, MarkdownDescription: "Owning project ID.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"domains":      schema.SetAttribute{Required: true, ElementType: types.StringType, MarkdownDescription: "Nonempty DNS domains, optionally prefixed with *. for wildcard routing.", Validators: []validator.Set{setvalidator.SizeAtLeast(1), setvalidator.ValueStringsAre(validators.Domain())}},
		"ssl_enabled":  schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Enable managed SSL. Certificate readiness is observed only with wait_for_ssl.", Default: booldefault.StaticBool(true)},
		"cdn_enabled":  schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Enable CDN for this route.", Default: booldefault.StaticBool(false)},
		"wait_for_ssl": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Wait for every configured domain to report SSL ACTIVE after synchronization. Requires ssl_enabled and external DNS."},
		"ssl_timeout":  schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("20m"), MarkdownDescription: "Positive SSL observation timeout, capped by the operation timeout.", Validators: []validator.String{configStringValidator{"must be a positive Go duration", func(s string) bool { d, e := time.ParseDuration(s); return e == nil && d > 0 }}}},
		"location": schema.ListNestedAttribute{Required: true, MarkdownDescription: "Ordered backend locations. Deploy HTTP backend metadata before routing.", Validators: []validator.List{listvalidator.SizeAtLeast(1)}, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"path":       schema.StringAttribute{Required: true, MarkdownDescription: "URL path starting with /.", Validators: []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^/`), "must start with /")}},
			"port":       schema.Int64Attribute{Required: true, MarkdownDescription: "Declared HTTP backend port.", Validators: []validator.Int64{validators.Port()}},
			"service_id": schema.StringAttribute{Required: true, MarkdownDescription: "Backend service ID in this project.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"config":     locationConfigSchema(),
		}}},
		"domain_statuses": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Observed raw domain statuses sorted by domain; they need not be ACTIVE.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"domain":           schema.StringAttribute{Computed: true, MarkdownDescription: "Domain name."},
			"dns_check_status": schema.StringAttribute{Computed: true, MarkdownDescription: "Raw DNS check status."},
			"ssl_status":       schema.StringAttribute{Computed: true, MarkdownDescription: "Raw managed SSL status."},
			"cdn_status":       schema.StringAttribute{Computed: true, MarkdownDescription: "Raw CDN status."},
		}}},
		"is_synced":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the owned staged route has been applied. False schedules reconciliation."},
		"force_sync": schema.BoolAttribute{Optional: true, MarkdownDescription: "Override provider routing_force_sync. Null inherits; false refuses foreign staged changes."},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func (m *Model) project(_ context.Context, _ client.API) (string, error) {
	return m.ProjectID.ValueString(), nil
}
func (m *Model) write(ctx context.Context) (client.HTTPRouteWrite, error) {
	var domains []string
	d := m.Domains.ElementsAs(ctx, &domains, false)
	if d.HasError() {
		return client.HTTPRouteWrite{}, fmt.Errorf("invalid domains: %v", d)
	}
	cdn := m.CDNEnabled.ValueBool()
	v := client.HTTPRouteWrite{Domains: domains, SSLEnabled: m.SSLEnabled.ValueBool(), CDNEnabled: &cdn, Locations: make([]client.HTTPLocation, len(m.Location))}
	for i, l := range m.Location {
		v.Locations[i] = client.HTTPLocation{Path: l.Path.ValueString(), Port: int(l.Port.ValueInt64()), ServiceStackID: l.ServiceID.ValueString()}
		if !l.Config.IsNull() {
			raw, err := configWire(l.Config)
			if err != nil {
				return client.HTTPRouteWrite{}, err
			}
			v.Locations[i].Config = raw.(map[string]any)
		}
	}
	return v, nil
}
func (m *Model) read(ctx context.Context, v client.HTTPRoute) error {
	if !v.IsEditable {
		return fmt.Errorf("route %s is generated/read-only; manage service subdomain access instead", v.ID)
	}
	m.ID = types.StringValue(v.ID)
	m.ProjectID = types.StringValue(v.ProjectID)
	m.SSLEnabled = types.BoolValue(v.SSLEnabled)
	m.CDNEnabled = types.BoolValue(v.CDNEnabled)
	m.IsSynced = types.BoolValue(v.IsSynced && !v.DeleteOnSync)
	domains := make([]string, len(v.Domains))
	statuses := make([]DomainStatus, len(v.Domains))
	sort.Slice(v.Domains, func(i, j int) bool { return v.Domains[i].DomainName < v.Domains[j].DomainName })
	for i, d := range v.Domains {
		domains[i] = d.DomainName
		statuses[i] = DomainStatus{types.StringValue(d.DomainName), types.StringValue(d.DNSCheckStatus), types.StringValue(d.SSLStatus), types.StringValue(d.CDNStatus)}
	}
	var d diag.Diagnostics
	m.Domains, d = types.SetValueFrom(ctx, types.StringType, domains)
	if d.HasError() {
		return fmt.Errorf("decoding domains: %v", d)
	}
	m.DomainStatuses, d = types.ListValueFrom(ctx, statusType, statuses)
	if d.HasError() {
		return fmt.Errorf("decoding statuses: %v", d)
	}
	m.Location = make([]Location, len(v.Locations))
	for i, l := range v.Locations {
		config, err := configValue(ctx, locationConfigSchema().GetType(), normalizedConfig(l.Config))
		if err != nil {
			return err
		}
		m.Location[i] = Location{Path: types.StringValue(l.Path), Port: types.Int64Value(int64(l.Port)), ServiceID: types.StringValue(l.ServiceStackID), Config: config.(types.Object)}
	}
	return nil
}
func (m *Model) verify(ctx context.Context, v client.HTTPRoute, desired client.HTTPRouteWrite) error {
	if !v.IsSynced || v.DeleteOnSync {
		return fmt.Errorf("route %s remains staged (isSynced=%t deleteOnSync=%t)", v.ID, v.IsSynced, v.DeleteOnSync)
	}
	actual := client.HTTPRouteWrite{SSLEnabled: v.SSLEnabled, CDNEnabled: &v.CDNEnabled, Domains: make([]string, len(v.Domains)), Locations: make([]client.HTTPLocation, len(v.Locations))}
	for i, d := range v.Domains {
		actual.Domains[i] = d.DomainName
	}
	for i, l := range v.Locations {
		actual.Locations[i] = l
		actual.Locations[i].Config = normalizedConfig(l.Config)
	}
	sort.Strings(actual.Domains)
	sort.Strings(desired.Domains)
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(desired)
	if string(a) != string(b) {
		return fmt.Errorf("route %s synced but desired domains/locations/SSL/CDN did not round-trip", v.ID)
	}
	return nil
}

type Resource struct{ data *providerdata.ProviderData }

func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_http_routing"
}
func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("wait_for_ssl"), false)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ssl_timeout"), "20m")...)
}
func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var synced types.Bool
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("is_synced"), &synced)...)
	if !synced.IsNull() && !synced.IsUnknown() && !synced.ValueBool() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("is_synced"), types.BoolValue(true))...)
	}
}
func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m Model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.data.API.GetHTTPRoute(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "publicHttpRoutingNotFound") {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading routing", err.Error())
		return
	}
	if err = m.read(ctx, v); err != nil {
		resp.Diagnostics.AddError("Reading routing", err.Error())
		return
	}
	if v.DeleteOnSync {
		resp.Diagnostics.AddWarning("Owned route pending deletion", "The owned route is staged for deletion; apply will restore and reconcile it. Refresh does not mutate routing.")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m Model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(d...)
	if duration <= 0 {
		resp.Diagnostics.AddError("Invalid timeout", "Create timeout must be positive.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	project, err := m.project(ctx, r.data.API)
	if err != nil {
		resp.Diagnostics.AddError("Resolving routing project", err.Error())
		return
	}
	unlock, err := r.data.Locks.Lock(ctx, locks.ProjectHTTPKey(project))
	if err != nil {
		resp.Diagnostics.AddError("Locking routing", err.Error())
		return
	}
	defer unlock()
	items, err := routing.HTTPInventory(ctx, r.data.API, project, "")
	if err != nil {
		resp.Diagnostics.AddError("Inspecting staged routing", err.Error())
		return
	}
	routing.Gate(items, routing.EffectiveForce(m.ForceSync, r.data.RoutingForceSync), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, err := m.write(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Encoding routing", err.Error())
		return
	}
	v, err := r.data.API.CreateHTTPRoute(ctx, m.ProjectID.ValueString(), desired)
	if err != nil {
		resp.Diagnostics.AddError("Staging routing", err.Error())
		return
	}
	m.ID = types.StringValue(v.ID)
	// Persist the staged identity even if the subsequent project sync fails.
	if err = m.read(ctx, v); err != nil {
		resp.Diagnostics.AddError("Reading staged routing", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if err = routing.Sync(ctx, r.data.API, project, true); err != nil {
		resp.Diagnostics.AddError("Syncing staged route "+v.ID, err.Error())
		return
	}
	v, err = r.data.API.GetHTTPRoute(ctx, v.ID)
	if err == nil {
		err = m.verify(ctx, v, desired)
	}
	if err != nil {
		resp.Diagnostics.AddError("Verifying routing", err.Error())
		return
	}
	if err = m.read(ctx, v); err != nil {
		resp.Diagnostics.AddError("Reading routing", err.Error())
		return
	}
	unlock()
	if err = r.observeSSL(ctx, &m); err != nil {
		resp.Diagnostics.AddError("Waiting for SSL", err.Error())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m Model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(d...)
	if duration <= 0 {
		resp.Diagnostics.AddError("Invalid timeout", "Update timeout must be positive.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	project, err := m.project(ctx, r.data.API)
	if err != nil {
		resp.Diagnostics.AddError("Resolving routing project", err.Error())
		return
	}
	unlock, err := r.data.Locks.Lock(ctx, locks.ProjectHTTPKey(project))
	if err != nil {
		resp.Diagnostics.AddError("Locking routing", err.Error())
		return
	}
	defer unlock()
	v, err := r.data.API.GetHTTPRoute(ctx, m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading owned routing", err.Error())
		return
	}
	desired, err := m.write(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Encoding routing", err.Error())
		return
	}
	if err = m.verify(ctx, v, desired); err == nil {
		unlock()
		var previous types.Bool
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("wait_for_ssl"), &previous)...)
		if err = m.read(ctx, v); err == nil && !previous.ValueBool() {
			err = r.observeSSL(ctx, &m)
		}
		if err != nil {
			resp.Diagnostics.AddError("Waiting for SSL", err.Error())
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		return
	}
	items, err := routing.HTTPInventory(ctx, r.data.API, project, m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Inspecting staged routing", err.Error())
		return
	}
	routing.Gate(items, routing.EffectiveForce(m.ForceSync, r.data.RoutingForceSync), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if v.DeleteOnSync {
		v, err = r.data.API.RestoreHTTPRoute(ctx, v.ID)
		if err == nil {
			v, err = r.data.API.GetHTTPRoute(ctx, m.ID.ValueString())
		}
		if err != nil {
			resp.Diagnostics.AddError("Restoring owned routing", err.Error())
			return
		}
		if v.DeleteOnSync {
			resp.Diagnostics.AddError("Restoring owned routing", "Restore did not clear deleteOnSync; refusing to sync a deletion.")
			return
		}
	}
	_, err = r.data.API.UpdateHTTPRoute(ctx, m.ID.ValueString(), desired)
	if err != nil {
		resp.Diagnostics.AddError("Staging routing", err.Error())
		return
	}
	_, err = r.data.API.SetHTTPLocations(ctx, m.ID.ValueString(), desired.Locations)
	if err != nil {
		resp.Diagnostics.AddError("Staging routing locations", err.Error())
		return
	}
	if err = routing.Sync(ctx, r.data.API, project, true); err != nil {
		resp.Diagnostics.AddError("Syncing staged route "+m.ID.ValueString(), err.Error())
		return
	}
	v, err = r.data.API.GetHTTPRoute(ctx, m.ID.ValueString())
	if err == nil {
		err = m.verify(ctx, v, desired)
	}
	if err != nil {
		resp.Diagnostics.AddError("Verifying routing", err.Error())
		return
	}
	if err = m.read(ctx, v); err != nil {
		resp.Diagnostics.AddError("Reading routing", err.Error())
		return
	}
	unlock()
	if err = r.observeSSL(ctx, &m); err != nil {
		resp.Diagnostics.AddError("Waiting for SSL", err.Error())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m Model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(d...)
	if duration <= 0 {
		resp.Diagnostics.AddError("Invalid timeout", "Delete timeout must be positive.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	_, err := r.data.API.GetHTTPRoute(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "publicHttpRoutingNotFound") {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading routing before delete", err.Error())
		return
	}
	project, err := m.project(ctx, r.data.API)
	if err != nil {
		resp.Diagnostics.AddError("Resolving routing project", err.Error())
		return
	}
	unlock, err := r.data.Locks.Lock(ctx, locks.ProjectHTTPKey(project))
	if err != nil {
		resp.Diagnostics.AddError("Locking routing", err.Error())
		return
	}
	defer unlock()
	items, err := routing.HTTPInventory(ctx, r.data.API, project, m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Inspecting staged routing", err.Error())
		return
	}
	routing.Gate(items, routing.EffectiveForce(m.ForceSync, r.data.RoutingForceSync), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	err = r.data.API.DeleteHTTPRoute(ctx, m.ID.ValueString())
	if err != nil && !client.IsNotFound(err, "publicHttpRoutingNotFound") {
		resp.Diagnostics.AddError("Staging routing deletion", err.Error())
		return
	}
	if err = routing.Sync(ctx, r.data.API, project, true); err != nil {
		resp.Diagnostics.AddError("Syncing staged deletion "+m.ID.ValueString(), err.Error())
		return
	}
	err = waiter.WaitNotFound(ctx, m.ID.ValueString(), func(ctx context.Context) error { _, e := r.data.API.GetHTTPRoute(ctx, m.ID.ValueString()); return e }, []string{"publicHttpRoutingNotFound"})
	if err != nil {
		resp.Diagnostics.AddError("Verifying routing deletion", err.Error())
	}
}
