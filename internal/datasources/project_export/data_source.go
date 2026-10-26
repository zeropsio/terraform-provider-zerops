package project_export

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
)

type source struct{ data *providerdata.ProviderData }

func NewDataSource() datasource.DataSource { return &source{} }
func (d *source) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_export"
}
func (d *source) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.data = providerdata.FromDataSourceConfigure(req, resp)
}
func (d *source) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Exports project YAML for inspection and migration. Exported YAML may contain secrets.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Project ID.", Computed: true}, "project_id": schema.StringAttribute{MarkdownDescription: "Project to export.", Required: true}, "reveal": schema.BoolAttribute{MarkdownDescription: "Reveal secrets; requires a sudo token. Defaults to false.", Optional: true}, "yaml": schema.StringAttribute{MarkdownDescription: "Exported YAML.", Computed: true, Sensitive: true}}}
}

type model struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	Reveal    types.Bool   `tfsdk:"reveal"`
	YAML      types.String `tfsdk:"yaml"`
}

func (d *source) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.data == nil {
		resp.Diagnostics.AddError("Provider not configured", "The provider has not been configured.")
		return
	}
	if m.ProjectID.IsUnknown() || m.Reveal.IsUnknown() {
		return
	}
	if m.ProjectID.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid project ID", "Project ID must not be empty.")
		return
	}
	yaml, err := d.data.API.ExportProject(ctx, m.ProjectID.ValueString(), m.Reveal.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Exporting project", err.Error())
		return
	}
	m.ID = m.ProjectID
	m.YAML = types.StringValue(yaml)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
