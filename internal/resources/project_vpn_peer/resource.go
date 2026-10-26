package project_vpn_peer

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	"net/netip"
	"strings"
	"time"
)

type configResource struct{ data *providerdata.ProviderData }
type model struct {
	ID           types.String   `tfsdk:"id"`
	ProjectID    types.String   `tfsdk:"project_id"`
	Timeouts     timeouts.Value `tfsdk:"timeouts"`
	PublicKey    types.String   `tfsdk:"public_key"`
	IPv4Address  types.String   `tfsdk:"ipv4_address"`
	IPv6Address  types.String   `tfsdk:"ipv6_address"`
	Setup        types.String   `tfsdk:"setup"`
	SetupLinux   types.String   `tfsdk:"setup_linux"`
	SetupMacOS   types.String   `tfsdk:"setup_macos"`
	SetupWindows types.String   `tfsdk:"setup_windows"`
}

func NewResource() resource.Resource { return &configResource{} }
func (*configResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_vpn_peer"
}
func (r *configResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (*configResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Registers a public WireGuard peer only. No private key is generated, accepted or retained, and no tunnel connectivity is asserted. Setup strings contain public network configuration.", Attributes: map[string]schema.Attribute{
		"id":         schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"project_id": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"public_key": schema.StringAttribute{Required: true, Validators: []validator.String{keyValidator{}}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}, "ipv4_address": schema.StringAttribute{Computed: true},
		"ipv6_address":  schema.StringAttribute{Computed: true},
		"setup":         schema.StringAttribute{Computed: true},
		"setup_linux":   schema.StringAttribute{Computed: true},
		"setup_macos":   schema.StringAttribute{Computed: true},
		"setup_windows": schema.StringAttribute{Computed: true},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func validKey(key string) bool {
	b, e := base64.StdEncoding.DecodeString(key)
	return e == nil && len(b) == 32 && base64.StdEncoding.EncodeToString(b) == key
}

type keyValidator struct{}

func (keyValidator) Description(context.Context) string {
	return "canonical standard-base64 32-byte WireGuard public key"
}
func (v keyValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (keyValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if !validKey(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid public key", "Expected a canonical standard-base64 32-byte WireGuard public key.")
	}
}
func mapPeer(m *model, p client.VPNPeer) error {
	if p.PublicKey != m.PublicKey.ValueString() {
		return fmt.Errorf("VPN peer returned a different public key")
	}
	a, e := netip.ParseAddr(p.IPv4.AssignedIPAddress)
	if e != nil || !a.Is4() {
		return fmt.Errorf("VPN peer missing valid IPv4 address")
	}
	b, e := netip.ParseAddr(p.IPv6.AssignedIPAddress)
	if e != nil || !b.Is6() {
		return fmt.Errorf("VPN peer missing valid IPv6 address")
	}
	m.IPv4Address = types.StringValue(p.IPv4.AssignedIPAddress)
	m.IPv6Address = types.StringValue(p.IPv6.AssignedIPAddress)
	m.Setup = types.StringValue(p.Setup)
	m.SetupLinux = types.StringValue(p.SetupLinux)
	m.SetupMacOS = types.StringValue(p.SetupMacOS)
	m.SetupWindows = types.StringValue(p.SetupWindows)
	return nil
}
func (r *configResource) write(ctx context.Context, m *model) error {
	p, e := r.data.API.CreateVPNPeer(ctx, m.ProjectID.ValueString(), m.PublicKey.ValueString())
	if e != nil {
		return e
	}
	// Accepted registration establishes identity before validating its readback.
	m.ID = types.StringValue(m.ProjectID.ValueString() + "/" + m.PublicKey.ValueString())
	m.IPv4Address, m.IPv6Address = types.StringNull(), types.StringNull()
	m.Setup, m.SetupLinux, m.SetupMacOS, m.SetupWindows = types.StringNull(), types.StringNull(), types.StringNull(), types.StringNull()
	return mapPeer(m, p)
}
func (r *configResource) load(ctx context.Context, m *model) (bool, error) {
	p, e := r.data.API.GetVPNPeer(ctx, m.ProjectID.ValueString(), m.PublicKey.ValueString())
	if client.IsNotFound(e, "projectNotFound", "vpnPublicKeyNotFound") {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	m.ID = types.StringValue(m.ProjectID.ValueString() + "/" + m.PublicKey.ValueString())
	return true, mapPeer(m, p)
}
func (r *configResource) matches(ctx context.Context, m *model) (bool, error) {
	p, e := r.data.API.GetVPNPeer(ctx, m.ProjectID.ValueString(), m.PublicKey.ValueString())
	if e != nil {
		return false, e
	}
	return true, mapPeer(m, p)
}

func (r *configResource) apply(ctx context.Context, m *model, save func(context.Context, *model) diag.Diagnostics, diags *diag.Diagnostics) {
	duration, d := m.Timeouts.Create(ctx, 30*time.Minute)
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
	err = r.write(ctx, m)
	if err != nil {
		if !m.ID.IsNull() && !m.ID.IsUnknown() {
			diags.Append(save(ctx, m)...)
		}
		if e, ok := client.AsAPIError(err); ok && e.HTTPStatus == 400 && e.Code == "invalidUserInput" {
			diags.AddAttributeError(path.Root("public_key"), "Invalid VPN public key", "The server rejected the WireGuard public key.")
			return
		}
		diags.AddError("Writing project configuration", err.Error())
		return
	}
	m.ID = types.StringValue(m.ProjectID.ValueString() + "/" + m.PublicKey.ValueString())
	diags.Append(save(ctx, m)...)
	err = waiter.WaitProjectConfig(ctx, m.ProjectID.ValueString(), func(ctx context.Context) (bool, error) { return r.matches(ctx, m) })
	if err != nil {
		diags.AddError("Verifying project configuration", err.Error())
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
	r.apply(ctx, &m, func(ctx context.Context, m *model) diag.Diagnostics { return resp.State.Set(ctx, m) }, &resp.Diagnostics)
}
func (r *configResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("timeouts"), &m.Timeouts)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
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
		resp.Diagnostics.AddError("Reading project before VPN peer deletion", err.Error())
		return
	}
	err = r.data.API.DeleteVPNPeer(ctx, m.ProjectID.ValueString(), m.PublicKey.ValueString())
	if client.IsNotFound(err, "projectNotFound", "vpnPublicKeyNotFound") {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Deleting VPN peer", err.Error())
		return
	}
	if err = waiter.WaitNotFound(ctx, m.ID.ValueString(), func(ctx context.Context) error {
		_, e := r.data.API.GetVPNPeer(ctx, m.ProjectID.ValueString(), m.PublicKey.ValueString())
		return e
	}, []string{"projectNotFound", "vpnPublicKeyNotFound"}, waiter.WithIntervals(5*time.Second, 5*time.Second, 5*time.Second, time.Minute)); err != nil {
		resp.Diagnostics.AddError("Waiting for VPN peer removal", err.Error())
	}
}
func (r *configResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	m := model{Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
	project, key, ok := strings.Cut(req.ID, "/")
	if !ok || project == "" || !validKey(key) {
		resp.Diagnostics.AddError("Invalid import ID", "Expected project_id/public_key, with the entire canonical standard-base64 public key after the first slash.")
		return
	}
	m.ProjectID = types.StringValue(project)
	m.PublicKey = types.StringValue(key)
	exists, err := r.load(ctx, &m)
	if err != nil {
		resp.Diagnostics.AddError("Importing project configuration", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError("Cannot import absent configuration", "The project or peer does not exist.")
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
