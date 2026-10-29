// Package provider exposes the fully registered Zerops Terraform Plugin Framework provider.
//
// It is the supported entry point for programs that embed the provider, such as the
// Pulumi bridge (github.com/zeropsio/pulumi-zerops). Resource and data source
// implementations stay internal; this package only composes them.
package provider

import (
	"errors"
	"strings"
	"unicode"

	framework "github.com/hashicorp/terraform-plugin-framework/provider"

	internal "github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/registry"
)

// Option customizes the provider returned by New.
type Option func(*internal.Options)

// WithUserAgent identifies an embedding product in the User-Agent header of every
// Zerops API request, for example "pulumi-zerops/0.1.0". The header then reads
// "<product> terraform-provider-zerops/<version>". Without this option the provider
// identifies itself with the Terraform CLI version, as when Terraform runs it.
//
// WithUserAgent panics if product is empty or contains whitespace or control characters,
// because a malformed product token is a programming error in the embedding binary.
func WithUserAgent(product string) Option {
	if err := validateProduct(product); err != nil {
		panic(err)
	}
	return func(o *internal.Options) { o.Product = product }
}

// New returns the Zerops provider with every resource and data source registered.
// version is the Terraform provider version reported to Terraform and in the User-Agent.
func New(version string, opts ...Option) framework.Provider {
	var o internal.Options
	for _, opt := range opts {
		opt(&o)
	}
	return internal.NewWithOptions(version, registry.New(), o)()
}

func validateProduct(product string) error {
	if product == "" {
		return errors.New("provider: WithUserAgent product must not be empty")
	}
	if strings.IndexFunc(product, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errors.New("provider: WithUserAgent product must not contain whitespace or control characters")
	}
	return nil
}
