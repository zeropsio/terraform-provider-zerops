package public_http_routing

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"time"
)

func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var wait, enabled types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("wait_for_ssl"), &wait)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("ssl_enabled"), &enabled)...)
	if wait.ValueBool() && !enabled.IsNull() && !enabled.IsUnknown() && !enabled.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("wait_for_ssl"), "SSL is disabled", "wait_for_ssl=true requires ssl_enabled=true.")
	}
	var locations types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("location"), &locations)...)
	if locations.IsNull() || locations.IsUnknown() {
		return
	}
	for i, element := range locations.Elements() {
		if element.IsNull() || element.IsUnknown() {
			continue
		}
		var l Location
		resp.Diagnostics.Append(element.(types.Object).As(ctx, &l, basetypes.ObjectAsOptions{})...)
		if resp.Diagnostics.HasError() {
			return
		}
		if l.Config.IsNull() || l.Config.IsUnknown() {
			continue
		}
		auth, ok := l.Config.Attributes()["basic_auth"].(types.Object)
		if !ok || auth.IsNull() || auth.IsUnknown() {
			continue
		}
		attrs := auth.Attributes()
		enabled, _ := attrs["enabled"].(types.Bool)
		users, _ := attrs["users"].(types.List)
		if enabled.ValueBool() && !users.IsNull() && !users.IsUnknown() && len(users.Elements()) == 0 {
			resp.Diagnostics.AddAttributeError(path.Root("location").AtListIndex(i).AtName("config").AtName("basic_auth").AtName("users"), "Empty basic-auth users", "At least one user is required when basic authentication is enabled.")
		}
	}
}

func (r *Resource) observeSSL(ctx context.Context, m *Model) error {
	if !m.WaitForSSL.ValueBool() {
		return nil
	}
	if !m.SSLEnabled.ValueBool() {
		return fmt.Errorf("wait_for_ssl requires ssl_enabled=true")
	}
	duration, err := time.ParseDuration(m.SSLTimeout.ValueString())
	if err != nil || duration <= 0 {
		return fmt.Errorf("ssl_timeout must be a positive Go duration")
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	var domains []string
	if d := m.Domains.ElementsAs(ctx, &domains, false); d.HasError() {
		return fmt.Errorf("SSL domains are unknown")
	}
	if err := r.data.API.TriggerHTTPDomainsDNSCheck(ctx, m.ID.ValueString()); err != nil {
		return err
	}
	v, err := waiter.WaitHTTPSSL(ctx, r.data.API, m.ID.ValueString(), domains)
	if v.ID != "" {
		if readErr := m.read(ctx, v); readErr != nil && err == nil {
			err = readErr
		}
	}
	return err
}
