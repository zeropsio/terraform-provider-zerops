// Package provider implements Zerops provider configuration and constructor registration.
package provider

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	validators "github.com/zeropsio/terraform-provider-zerops/internal/validators"
)

const DefaultEndpoint = client.ProductionEndpoint

// Registry separates registration from the provider, allowing isolated package tests.
type Registry struct {
	Resources   []func() resource.Resource
	DataSources []func() datasource.DataSource
}

type zeropsProvider struct {
	version   string
	registry  Registry
	transport http.RoundTripper
}
type configModel struct {
	Endpoint            types.String `tfsdk:"endpoint"`
	Token               types.String `tfsdk:"token"`
	OrganizationID      types.String `tfsdk:"organization_id"`
	RoutingForceSync    types.Bool   `tfsdk:"routing_force_sync"`
	PostgresqlMinDiskGB types.Number `tfsdk:"postgresql_min_disk_gb"`
}

func New(version string, reg Registry) func() framework.Provider {
	return NewWithTransport(version, reg, nil)
}

// NewWithTransport injects a transport shared by API calls and archive uploads.
func NewWithTransport(version string, reg Registry, transport http.RoundTripper) func() framework.Provider {
	return func() framework.Provider {
		return &zeropsProvider{version: version, registry: reg, transport: transport}
	}
}

func (p *zeropsProvider) Metadata(_ context.Context, _ framework.MetadataRequest, resp *framework.MetadataResponse) {
	resp.TypeName = "zerops"
	resp.Version = p.version
}
func (p *zeropsProvider) Resources(context.Context) []func() resource.Resource {
	return p.registry.Resources
}
func (p *zeropsProvider) DataSources(context.Context) []func() datasource.DataSource {
	return p.registry.DataSources
}
func (p *zeropsProvider) Schema(_ context.Context, _ framework.SchemaRequest, resp *framework.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manage Zerops projects and services. Sensitive values are retained in Terraform state; protect your state files.", Attributes: map[string]schema.Attribute{
		"endpoint":               schema.StringAttribute{Optional: true, Description: "HTTPS API origin. Defaults to ZEROPS_ENDPOINT, then the production Prague API.", Validators: []validator.String{endpointValidator{}}},
		"token":                  schema.StringAttribute{Optional: true, Sensitive: true, Description: "Integration token. Defaults to ZEROPS_TOKEN. No sudo escalation is performed."},
		"organization_id":        schema.StringAttribute{Optional: true, Description: "Organization client ID. Defaults to ZEROPS_ORGANIZATION_ID; otherwise exactly one membership is required."},
		"routing_force_sync":     schema.BoolAttribute{Optional: true, Description: "Allow synchronizing foreign staged routing changes. Defaults to ZEROPS_ROUTING_FORCE_SYNC, then false."},
		"postgresql_min_disk_gb": schema.NumberAttribute{Optional: true, Description: "PostgreSQL disk floor in GB, with at most three decimal places. Defaults to ZEROPS_POSTGRESQL_MIN_DISK_GB, then 2. The platform currently fails to provision PostgreSQL below this floor (generic HTTP 500 error). Lower or raise it when Zerops fixes the catalog.", Validators: []validator.Number{postgresqlDiskFloorValidator{}}},
	}}
}

// ValidateEndpoint returns the normalized origin; insecure and non-origin URLs are rejected.
func ValidateEndpoint(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(value, "#") || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", fmt.Errorf("endpoint must be an absolute HTTPS origin without credentials, query, fragment, or a non-root path")
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

type endpointValidator struct{}

func (endpointValidator) Description(context.Context) string {
	return "Must be an absolute HTTPS origin."
}
func (v endpointValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (endpointValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := ValidateEndpoint(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid API endpoint", err.Error())
	}
}

func resolveString(value types.String, key, fallback string) string {
	if !value.IsNull() {
		return value.ValueString()
	}
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func (p *zeropsProvider) Configure(ctx context.Context, req framework.ConfigureRequest, resp *framework.ConfigureResponse) {
	var config configModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Unknown values must never trigger a request with guessed credentials or organization.
	for name, unknown := range map[string]bool{"endpoint": config.Endpoint.IsUnknown(), "token": config.Token.IsUnknown(), "organization_id": config.OrganizationID.IsUnknown(), "routing_force_sync": config.RoutingForceSync.IsUnknown(), "postgresql_min_disk_gb": config.PostgresqlMinDiskGB.IsUnknown()} {
		if unknown {
			if req.ClientCapabilities.DeferralAllowed {
				resp.Deferred = &framework.Deferred{Reason: framework.DeferredReasonProviderConfigUnknown}
				return
			}
			resp.Diagnostics.AddAttributeError(path.Root(name), "Unknown provider configuration", "This provider value must be known before configuring the Zerops client. Use values available before apply.")
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	configuredEndpoint := resolveString(config.Endpoint, "ZEROPS_ENDPOINT", DefaultEndpoint)
	endpoint, err := ValidateEndpoint(configuredEndpoint)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Invalid API endpoint", err.Error())
	}
	token := resolveString(config.Token, "ZEROPS_TOKEN", "")
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing API token", "Configure a nonempty token or set ZEROPS_TOKEN.")
	}
	organization := resolveString(config.OrganizationID, "ZEROPS_ORGANIZATION_ID", "")
	_, organizationInEnvironment := os.LookupEnv("ZEROPS_ORGANIZATION_ID")
	if os.Getenv("ZEROPS_SANDBOX") != "prod" && (!config.OrganizationID.IsNull() || organizationInEnvironment) && organization == "" {
		resp.Diagnostics.AddAttributeError(path.Root("organization_id"), "Invalid organization ID", "An explicitly configured organization ID (including ZEROPS_ORGANIZATION_ID) must not be empty.")
	}
	force := config.RoutingForceSync.ValueBool()
	if config.RoutingForceSync.IsNull() {
		if value, present := os.LookupEnv("ZEROPS_ROUTING_FORCE_SYNC"); present {
			force, err = strconv.ParseBool(value)
			if err != nil {
				resp.Diagnostics.AddAttributeError(path.Root("routing_force_sync"), "Invalid routing synchronization flag", "ZEROPS_ROUTING_FORCE_SYNC must be a boolean (true or false).")
			}
		}
	}
	diskFloor := 2.0
	floorNumber := config.PostgresqlMinDiskGB
	if !config.PostgresqlMinDiskGB.IsNull() {
		diskFloor, _ = config.PostgresqlMinDiskGB.ValueBigFloat().Float64()
	} else if value, present := os.LookupEnv("ZEROPS_POSTGRESQL_MIN_DISK_GB"); present {
		diskFloor, err = strconv.ParseFloat(value, 64)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("postgresql_min_disk_gb"), "Invalid PostgreSQL disk floor", "ZEROPS_POSTGRESQL_MIN_DISK_GB must be a finite number greater than or equal to 1.")
		}
		if err == nil && !math.IsInf(diskFloor, 0) && !math.IsNaN(diskFloor) {
			number, _, parseErr := big.ParseFloat(value, 10, 512, big.ToNearestEven)
			if parseErr == nil {
				floorNumber = types.NumberValue(number)
			}
		}
	}
	if !(diskFloor >= 1) || math.IsInf(diskFloor, 0) {
		resp.Diagnostics.AddAttributeError(path.Root("postgresql_min_disk_gb"), "Invalid PostgreSQL disk floor", "postgresql_min_disk_gb (or ZEROPS_POSTGRESQL_MIN_DISK_GB) must be a finite number greater than or equal to 1.")
	}
	var precision validator.NumberResponse
	validators.Precision().ValidateNumber(ctx, validator.NumberRequest{Path: path.Root("postgresql_min_disk_gb"), ConfigValue: floorNumber}, &precision)
	if precision.Diagnostics.HasError() {
		resp.Diagnostics.AddAttributeError(path.Root("postgresql_min_disk_gb"), "Invalid PostgreSQL disk floor precision", "postgresql_min_disk_gb / ZEROPS_POSTGRESQL_MIN_DISK_GB must have at most three decimal places.")
	}
	transport := p.transport
	switch os.Getenv("ZEROPS_SANDBOX") {
	case "":
	case "dev":
		if err := client.ValidateTargetEndpoint(client.TargetDev, configuredEndpoint); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Invalid development endpoint", err.Error())
		}
		transport = client.DevTransport{Domain: os.Getenv("ZEROPS_DEV_DOMAIN"), Base: transport}
	case "prod":
		if strings.TrimSpace(os.Getenv("ZEROPS_ORGANIZATION_ID")) == "" || organization != os.Getenv("ZEROPS_ORGANIZATION_ID") {
			resp.Diagnostics.AddAttributeError(path.Root("organization_id"), "Invalid production organization", "ZEROPS_SANDBOX=prod requires a nonempty ZEROPS_ORGANIZATION_ID matching the configured organization.")
		}
		if configuredEndpoint != client.ProductionEndpoint {
			resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Invalid production endpoint", "ZEROPS_SANDBOX=prod requires the exact production endpoint "+client.ProductionEndpoint+".")
		}
		if !filepath.IsAbs(os.Getenv("ZEROPS_PROD_LEDGER")) {
			resp.Diagnostics.AddError("Invalid production ledger", "ZEROPS_SANDBOX=prod requires an absolute ZEROPS_PROD_LEDGER path.")
		}
	default:
		resp.Diagnostics.AddError("Invalid sandbox configuration", "ZEROPS_SANDBOX must be unset, empty, dev, or prod.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	ctx = tflog.MaskMessageStrings(ctx, token)
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, "token", "authorization")
	cfg := client.Config{Endpoint: endpoint, Token: token, UserAgent: fmt.Sprintf("terraform-provider-zerops/%s (terraform %s)", p.version, req.TerraformVersion), Transport: transport}
	if os.Getenv("ZEROPS_SANDBOX") == "prod" {
		guard, err := client.NewProdTransport(ctx, cfg, organization, os.Getenv("ZEROPS_PROD_LEDGER"), "provider")
		if err != nil {
			resp.Diagnostics.AddError("Cannot configure production guard", err.Error())
			return
		}
		cfg.Transport, organization = guard, guard.OrgID
	}
	api, err := client.New(cfg)
	if err != nil {
		resp.Diagnostics.AddError("Cannot configure Zerops client", err.Error())
		return
	}
	if organization == "" {
		organizations, err := api.ListOrganizations(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Cannot discover organization", err.Error())
			return
		}
		if len(organizations) != 1 {
			resp.Diagnostics.AddAttributeError(path.Root("organization_id"), "Organization ID required", "Organization auto-detection requires exactly one membership. Configure organization_id explicitly.")
			return
		}
		organization = organizations[0].ID
		if organization == "" {
			resp.Diagnostics.AddError("Invalid organization membership", "The membership response has no clientId.")
			return
		}
	}
	data := &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}, OrganizationID: organization, Endpoint: endpoint, RoutingForceSync: force, PostgresqlMinDiskGB: diskFloor}
	resp.ResourceData, resp.DataSourceData = data, data
}

type postgresqlDiskFloorValidator struct{}

func (postgresqlDiskFloorValidator) Description(context.Context) string {
	return "Must be a finite number greater than or equal to 1 with at most three decimal places."
}
func (v postgresqlDiskFloorValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v postgresqlDiskFloorValidator) ValidateNumber(ctx context.Context, req validator.NumberRequest, resp *validator.NumberResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value, _ := req.ConfigValue.ValueBigFloat().Float64()
	if !(value >= 1) || math.IsInf(value, 0) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid PostgreSQL disk floor", v.Description(ctx))
	}
	validators.Precision().ValidateNumber(ctx, req, resp)
}
