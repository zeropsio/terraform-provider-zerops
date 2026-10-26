package provider

import (
	"context"
	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"testing"
)

func TestUnknownConfigurationDefersWhenSupported(t *testing.T) {
	p := New("test", Registry{})()
	var response framework.ConfigureResponse
	p.Configure(context.Background(), framework.ConfigureRequest{
		Config:             providerConfig(t, p, map[string]any{"token": tftypes.UnknownValue}),
		ClientCapabilities: framework.ConfigureProviderClientCapabilities{DeferralAllowed: true},
	}, &response)
	if response.Diagnostics.HasError() || response.Deferred == nil || response.Deferred.Reason != framework.DeferredReasonProviderConfigUnknown || response.ResourceData != nil {
		t.Fatalf("configuration was not deferred safely: %v, %#v", response.Diagnostics, response.Deferred)
	}
}
