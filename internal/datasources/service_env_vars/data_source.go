package service_env_vars

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"strings"
)

type DataSource struct{ data *providerdata.ProviderData }
type model struct {
	ID        types.String `tfsdk:"id"`
	ServiceID types.String `tfsdk:"service_id"`
	Vars      types.Map    `tfsdk:"vars"`
}

func NewDataSource() datasource.DataSource { return &DataSource{} }
func (d *DataSource) Metadata(_ context.Context, q datasource.MetadataRequest, p *datasource.MetadataResponse) {
	p.TypeName = q.ProviderTypeName + "_service_env_vars"
}
func (d *DataSource) Configure(_ context.Context, q datasource.ConfigureRequest, p *datasource.ConfigureResponse) {
	d.data = providerdata.FromDataSourceConfigure(q, p)
}
func (d *DataSource) Schema(_ context.Context, _ datasource.SchemaRequest, p *datasource.SchemaResponse) {
	p.Schema = schema.Schema{MarkdownDescription: "All generated and user environment variables returned by the service API. Values are stored in state. Without sudo, masks are retained and this is not a plaintext credential source.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Service ID."}, "service_id": schema.StringAttribute{Required: true, MarkdownDescription: "Service to read.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}}, "vars": schema.MapAttribute{Computed: true, ElementType: types.StringType, Sensitive: true, MarkdownDescription: "Complete environment keyed by exact variable name, including generated connection strings. Server masks remain masks when plaintext is unavailable."}}}
}
func envMap(rows []client.Env) (map[string]string, bool) {
	m := make(map[string]string, len(rows))
	masked := false
	for _, row := range rows {
		m[row.Key] = row.Content
		if row.Content == "REDACTED" || (row.Content != "" && strings.Trim(row.Content, "*") == "") {
			masked = true
		}
	}
	return m, masked
}
func (d *DataSource) Read(ctx context.Context, q datasource.ReadRequest, p *datasource.ReadResponse) {
	var m model
	p.Diagnostics.Append(q.Config.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	rows, e := d.data.API.ListServiceEnv(ctx, m.ServiceID.ValueString())
	if e != nil {
		p.Diagnostics.AddError("Reading service environment", e.Error())
		return
	}
	values, masked := envMap(rows)
	if masked {
		p.Diagnostics.AddWarning("Masked environment values", "The API returned masked values. Sudo credentials are required for plaintext generated secrets; this map is not a plaintext credential source.")
	}
	var ds diag.Diagnostics
	m.Vars, ds = types.MapValueFrom(ctx, types.StringType, values)
	p.Diagnostics.Append(ds...)
	m.ID = m.ServiceID
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
