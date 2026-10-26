// Package service_subdomain_access manages generated HTTP access for a service.
package service_subdomain_access

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

type accessResource struct{ data *providerdata.ProviderData }
type model struct {
	ID        types.String   `tfsdk:"id"`
	ServiceID types.String   `tfsdk:"service_id"`
	URLs      types.Set      `tfsdk:"urls"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func NewResource() resource.Resource { return &accessResource{} }
func (*accessResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_subdomain_access"
}
func (r *accessResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (*accessResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	block := validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Delete: true})
	resp.Schema = schema.Schema{MarkdownDescription: "Enables Zerops-generated HTTP subdomains for an already deployed service. Declare an explicit dependency on the deployment that defines HTTP ports. Disabling access does not stop the service.", Attributes: map[string]schema.Attribute{
		"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "Service ID; identical to `service_id`.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"service_id": schema.StringAttribute{Required: true, MarkdownDescription: "Service to expose. Its deployment must declare HTTP ports. Only one access resource may own a service.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"urls":       schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "HTTPS URLs read from non-editable generated project routes targeting this service. No hostnames are guessed.", PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()}},
	}, Blocks: map[string]schema.Block{"timeouts": block}}
}

// generatedURLs deliberately derives only URLs backed by generated routes.
func generatedURLs(routes []client.HTTPRoute, serviceID string) []string {
	urls := make([]string, 0)
	for _, route := range routes {
		if route.IsEditable {
			continue
		}
		targets := false
		for _, location := range route.Locations {
			if location.ServiceStackID == serviceID {
				targets = true
				break
			}
		}
		if !targets {
			continue
		}
		for _, domain := range route.Domains {
			if domain.DomainName != "" {
				urls = append(urls, "https://"+domain.DomainName)
			}
		}
	}
	slices.Sort(urls)
	return slices.Compact(urls)
}
func (r *accessResource) routes(ctx context.Context, projectID string) ([]client.HTTPRoute, error) {
	var all []client.HTTPRoute
	for offset := 0; ; {
		page, err := r.data.API.ListHTTPRoutes(ctx, projectID, client.PageQuery{Offset: offset, Limit: 100})
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if len(page.Items) == 0 {
			if offset < page.Total {
				return nil, fmt.Errorf("HTTP route pagination ended before total %d", page.Total)
			}
			return all, nil
		}
		next := offset + len(page.Items)
		if next <= offset {
			return nil, fmt.Errorf("HTTP route pagination did not advance")
		}
		if next >= page.Total {
			return all, nil
		}
		offset = next
	}
}
func (r *accessResource) load(ctx context.Context, m *model) (bool, error) {
	service, err := r.data.API.GetService(ctx, m.ServiceID.ValueString())
	if client.IsNotFound(err, "serviceStackNotFound") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !service.SubdomainAccess {
		return false, nil
	}
	routes, err := r.routes(ctx, service.ProjectID)
	if err != nil {
		return false, err
	}
	urls, diags := types.SetValueFrom(ctx, types.StringType, generatedURLs(routes, service.ID))
	if diags.HasError() {
		return false, fmt.Errorf("converting generated URLs: %v", diags)
	}
	m.ID = m.ServiceID
	m.URLs = urls
	return true, nil
}
func (r *accessResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, diags := m.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(m.ServiceID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Locking service", err.Error())
		return
	}
	defer unlock()
	process, err := r.data.API.SetSubdomainAccess(ctx, m.ServiceID.ValueString(), true)
	if err != nil {
		if e, ok := client.AsAPIError(err); ok && e.HTTPStatus == 400 && e.Code == "serviceStackIsNotHttp" {
			resp.Diagnostics.AddError("Cannot enable subdomain access", "service has no HTTP ports; deploy first")
			return
		}
		resp.Diagnostics.AddError("Enabling subdomain access", err.Error())
		return
	}
	// Preserve ownership if waiting or readback fails after the accepted mutation.
	m.ID = m.ServiceID
	m.URLs = types.SetValueMust(types.StringType, []attr.Value{})
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if _, err = waiter.WaitProcess(ctx, r.data.API, process.ID); err != nil {
		resp.Diagnostics.AddError("Waiting for subdomain access", err.Error())
		return
	}
	exists, err := r.load(ctx, &m)
	if err != nil {
		resp.Diagnostics.AddError("Reading subdomain access", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError("Cannot enable subdomain access", "service has no HTTP ports; deploy first")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *accessResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	exists, err := r.load(ctx, &m)
	if err != nil {
		resp.Diagnostics.AddError("Reading subdomain access", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (*accessResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	}
}
func (r *accessResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, diags := m.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	id := m.ServiceID.ValueString()
	unlock, err := r.data.Locks.Lock(ctx, locks.ServiceKey(id))
	if err != nil {
		resp.Diagnostics.AddError("Locking service", err.Error())
		return
	}
	defer unlock()
	service, err := r.data.API.GetService(ctx, id)
	if client.IsNotFound(err, "serviceStackNotFound") {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading service", err.Error())
		return
	}
	if service.SubdomainAccess {
		process, disableErr := r.data.API.SetSubdomainAccess(ctx, id, false)
		if disableErr != nil {
			e, ok := client.AsAPIError(disableErr)
			if !ok || e.HTTPStatus != 400 || e.Code != "serviceStackSubdomainAccessAlreadyDisabled" {
				resp.Diagnostics.AddError("Disabling subdomain access", disableErr.Error())
				return
			}
		} else if _, err = waiter.WaitProcess(ctx, r.data.API, process.ID); err != nil {
			resp.Diagnostics.AddError("Waiting for subdomain disable", err.Error())
			return
		}
	}
	service, err = r.data.API.GetService(ctx, id)
	if client.IsNotFound(err, "serviceStackNotFound") {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Verifying subdomain disable", err.Error())
		return
	}
	if service.SubdomainAccess {
		resp.Diagnostics.AddError("Subdomain access still enabled", "The service flag remained true after disabling access.")
		return
	}
	routes, err := r.routes(ctx, service.ProjectID)
	if err != nil {
		resp.Diagnostics.AddError("Verifying generated route removal", err.Error())
		return
	}
	if len(generatedURLs(routes, id)) != 0 {
		resp.Diagnostics.AddError("Generated routes remain", "Generated HTTP routes still target the service after disabling access.")
	}
}
func (r *accessResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Import requires a service ID with subdomain access enabled.")
		return
	}
	m := model{ServiceID: types.StringValue(req.ID), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "delete": types.StringType})}}
	exists, err := r.load(ctx, &m)
	if err != nil {
		resp.Diagnostics.AddError("Importing subdomain access", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError("Subdomain access is not enabled", "Import requires an existing service with subdomainAccess=true.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
