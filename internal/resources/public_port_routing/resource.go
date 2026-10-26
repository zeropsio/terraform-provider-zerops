package public_port_routing

import (
	"context"
	"fmt"
	"net/netip"
	"reflect"
	"sort"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
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

type Model struct {
	ID               types.String   `tfsdk:"id"`
	ServiceID        types.String   `tfsdk:"service_id"`
	PublicPort       types.Int64    `tfsdk:"public_port"`
	InternalPort     types.Int64    `tfsdk:"internal_port"`
	Protocol         types.String   `tfsdk:"protocol"`
	PublicIPType     types.String   `tfsdk:"public_ip_type"`
	FirewallPolicy   types.String   `tfsdk:"firewall_policy"`
	FirewallIPRanges types.Set      `tfsdk:"firewall_ip_ranges"`
	IsSynced         types.Bool     `tfsdk:"is_synced"`
	ForceSync        types.Bool     `tfsdk:"force_sync"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Version: 0, MarkdownDescription: "IPv4 or IPv6 public port routing. IPv4 requires a dedicated project IPv4 allocation; shared IPv4 is insufficient. Mutations synchronize all staged port routes in the project; foreign changes are refused unless explicitly forced.", Attributes: map[string]schema.Attribute{
		"id":                 schema.StringAttribute{Computed: true, MarkdownDescription: "Stable routing ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"service_id":         schema.StringAttribute{Required: true, MarkdownDescription: "Target service ID. Deploy a declared backend port before creating routing.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"public_port":        schema.Int64Attribute{Required: true, MarkdownDescription: "Public port, 10–65435 except reserved 80 and 443.", Validators: []validator.Int64{validators.PublicPort()}},
		"internal_port":      schema.Int64Attribute{Required: true, MarkdownDescription: "Declared backend port.", Validators: []validator.Int64{validators.Port()}},
		"protocol":           schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Declared backend protocol: tcp or udp.", Default: stringdefault.StaticString("tcp"), Validators: []validator.String{stringvalidator.OneOf("tcp", "udp")}},
		"public_ip_type":     schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "IP_V6 (default) or IP_V4. IPv4 requires an existing dedicated project IPv4 allocation, not the shared IPv4 flag.", Default: stringdefault.StaticString("IP_V6"), Validators: []validator.String{stringvalidator.OneOf("IP_V4", "IP_V6")}},
		"firewall_policy":    schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Firewall BLACKLIST or WHITELIST policy.", Default: stringdefault.StaticString("BLACKLIST"), Validators: []validator.String{stringvalidator.OneOf("BLACKLIST", "WHITELIST")}},
		"firewall_ip_ranges": schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, MarkdownDescription: "Firewall IPv4/IPv6 addresses or CIDRs. Host addresses are sent as /32 or /128 CIDRs while retaining configured spelling in state. Import has no configured spelling and reads API CIDRs; use canonical CIDRs in configuration for an exact import match. Caller IP is never implicitly added.", Default: setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{})), Validators: []validator.Set{setvalidator.ValueStringsAre(validators.CIDROrIP())}},
		"is_synced":          schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the staged rule has been applied. Unsynced owned rules reconcile on apply."},
		"force_sync":         schema.BoolAttribute{Optional: true, MarkdownDescription: "Override provider routing_force_sync. Null inherits; false refuses foreign staged changes."},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func (m *Model) project(ctx context.Context, api client.API) (string, error) {
	s, e := api.GetService(ctx, m.ServiceID.ValueString())
	return s.ProjectID, e
}
func (m *Model) write(ctx context.Context) (client.PortRouteWrite, error) {
	var ranges []string
	d := m.FirewallIPRanges.ElementsAs(ctx, &ranges, false)
	if d.HasError() {
		return client.PortRouteWrite{}, fmt.Errorf("invalid firewall ranges: %v", d)
	}
	for i, value := range ranges {
		ranges[i] = canonicalRange(value)
	}
	if ranges == nil {
		ranges = []string{}
	}
	return client.PortRouteWrite{PublicIPType: m.PublicIPType.ValueString(), PublicPort: int(m.PublicPort.ValueInt64()), InternalPort: int(m.InternalPort.ValueInt64()), InternalProtocol: m.Protocol.ValueString(), FirewallPolicy: m.FirewallPolicy.ValueString(), FirewallIPRanges: ranges, FirewallAllowMyIP: false}, nil
}
func (m *Model) read(ctx context.Context, v client.PortRoute) error {
	if v.PublicIPType != "IP_V6" && v.PublicIPType != "IP_V4" {
		return fmt.Errorf("route %s uses unsupported public IP type %s; expected IP_V4 or IP_V6", v.ID, v.PublicIPType)
	}
	m.ID = types.StringValue(v.ID)
	m.ServiceID = types.StringValue(v.ServiceStackID)
	m.PublicPort = types.Int64Value(int64(v.PublicPort))
	m.InternalPort = types.Int64Value(int64(v.InternalPort))
	m.Protocol = types.StringValue(v.InternalProtocol)
	m.PublicIPType = types.StringValue(v.PublicIPType)
	m.FirewallPolicy = types.StringValue(v.FirewallPolicy)
	m.IsSynced = types.BoolValue(v.IsSynced && !v.DeleteOnSync)
	var d diag.Diagnostics
	if v.FirewallIPRanges == nil {
		v.FirewallIPRanges = []string{}
	}
	// Development requires CIDR notation. Retain a configured bare address spelling
	// when the API's /32 or /128 represents the same host, including on refresh.
	if !m.FirewallIPRanges.IsNull() && !m.FirewallIPRanges.IsUnknown() {
		var prior []string
		d = m.FirewallIPRanges.ElementsAs(ctx, &prior, false)
		if d.HasError() {
			return fmt.Errorf("decoding prior firewall: %v", d)
		}
		spelling := make(map[string]string, len(prior))
		for _, value := range prior {
			spelling[canonicalRange(value)] = value
		}
		for i, value := range v.FirewallIPRanges {
			if original, ok := spelling[canonicalRange(value)]; ok {
				v.FirewallIPRanges[i] = original
			}
		}
	}
	m.FirewallIPRanges, d = types.SetValueFrom(ctx, types.StringType, v.FirewallIPRanges)
	if d.HasError() {
		return fmt.Errorf("decoding firewall: %v", d)
	}
	return nil
}
func (m *Model) verify(ctx context.Context, v client.PortRoute, desired client.PortRouteWrite) error {
	if !v.IsSynced || v.DeleteOnSync {
		return fmt.Errorf("route %s remains staged (isSynced=%t deleteOnSync=%t)", v.ID, v.IsSynced, v.DeleteOnSync)
	}
	actual := v.PortRouteWrite
	for i, value := range actual.FirewallIPRanges {
		actual.FirewallIPRanges[i] = canonicalRange(value)
	}
	sort.Strings(actual.FirewallIPRanges)
	sort.Strings(desired.FirewallIPRanges)
	if len(actual.FirewallIPRanges) == 0 {
		actual.FirewallIPRanges = []string{}
	}
	if !reflect.DeepEqual(actual, desired) {
		return fmt.Errorf("route %s synced but desired port/firewall fields did not round-trip", v.ID)
	}
	return nil
}

func canonicalRange(value string) string {
	if address, err := netip.ParseAddr(value); err == nil {
		return netip.PrefixFrom(address, address.BitLen()).String()
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked().String()
	}
	return value // Schema validation diagnoses malformed input.
}

type Resource struct{ data *providerdata.ProviderData }

func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_port_routing"
}
func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
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
	v, err := r.data.API.GetPortRoute(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "publicPortRoutingNotFound") {
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
	unlock, err := r.data.Locks.Lock(ctx, locks.ProjectPortKey(project))
	if err != nil {
		resp.Diagnostics.AddError("Locking routing", err.Error())
		return
	}
	defer unlock()
	items, err := routing.PortInventory(ctx, r.data.API, project, "")
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
	v, err := r.data.API.CreatePortRoute(ctx, m.ServiceID.ValueString(), desired)
	if err != nil {
		resp.Diagnostics.AddError("Staging routing", portMutationError(err))
		return
	}
	m.ID = types.StringValue(v.ID)
	// Persist the staged identity even if the subsequent project sync fails.
	if err = m.read(ctx, v); err != nil {
		resp.Diagnostics.AddError("Reading staged routing", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if err = routing.Sync(ctx, r.data.API, project, false); err != nil {
		resp.Diagnostics.AddError("Syncing staged route "+v.ID, err.Error())
		return
	}
	v, err = r.data.API.GetPortRoute(ctx, v.ID)
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
	unlock, err := r.data.Locks.Lock(ctx, locks.ProjectPortKey(project))
	if err != nil {
		resp.Diagnostics.AddError("Locking routing", err.Error())
		return
	}
	defer unlock()
	items, err := routing.PortInventory(ctx, r.data.API, project, m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Inspecting staged routing", err.Error())
		return
	}
	routing.Gate(items, routing.EffectiveForce(m.ForceSync, r.data.RoutingForceSync), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.data.API.GetPortRoute(ctx, m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading owned routing", err.Error())
		return
	}
	if v.DeleteOnSync {
		v, err = r.data.API.RestorePortRoute(ctx, v.ID)
		if err == nil {
			v, err = r.data.API.GetPortRoute(ctx, m.ID.ValueString())
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
	desired, err := m.write(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Encoding routing", err.Error())
		return
	}
	_, err = r.data.API.UpdatePortRoute(ctx, m.ID.ValueString(), desired)
	if err != nil {
		resp.Diagnostics.AddError("Staging routing", portMutationError(err))
		return
	}
	if err = routing.Sync(ctx, r.data.API, project, false); err != nil {
		resp.Diagnostics.AddError("Syncing staged route "+m.ID.ValueString(), err.Error())
		return
	}
	v, err = r.data.API.GetPortRoute(ctx, m.ID.ValueString())
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
	_, err := r.data.API.GetPortRoute(ctx, m.ID.ValueString())
	if client.IsNotFound(err, "publicPortRoutingNotFound") {
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
	unlock, err := r.data.Locks.Lock(ctx, locks.ProjectPortKey(project))
	if err != nil {
		resp.Diagnostics.AddError("Locking routing", err.Error())
		return
	}
	defer unlock()
	items, err := routing.PortInventory(ctx, r.data.API, project, m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Inspecting staged routing", err.Error())
		return
	}
	routing.Gate(items, routing.EffectiveForce(m.ForceSync, r.data.RoutingForceSync), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	err = r.data.API.DeletePortRoute(ctx, m.ID.ValueString())
	if err != nil && !client.IsNotFound(err, "publicPortRoutingNotFound") {
		resp.Diagnostics.AddError("Staging routing deletion", err.Error())
		return
	}
	if err = routing.Sync(ctx, r.data.API, project, false); err != nil {
		resp.Diagnostics.AddError("Syncing staged deletion "+m.ID.ValueString(), err.Error())
		return
	}
	err = waiter.WaitNotFound(ctx, m.ID.ValueString(), func(ctx context.Context) error { _, e := r.data.API.GetPortRoute(ctx, m.ID.ValueString()); return e }, []string{"publicPortRoutingNotFound"})
	if err != nil {
		resp.Diagnostics.AddError("Verifying routing deletion", err.Error())
	}
}

func portMutationError(err error) string {
	if e, ok := client.AsAPIError(err); ok && e.HTTPStatus == 400 && e.Code == "publicIpTypeNotSupported" {
		return "publicIpTypeNotSupported: IPv4 port routing requires a dedicated project IPv4 allocation. The shared IPv4 flag is insufficient. Configure zerops_project_dedicated_ipv4 or select IP_V6; no allocation or fallback is performed automatically."
	}
	return err.Error()
}
