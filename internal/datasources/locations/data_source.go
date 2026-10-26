package locations

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"sort"
)

type source struct{ data *providerdata.ProviderData }

func NewDataSource() datasource.DataSource { return &source{} }
func (d *source) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_locations"
}
func (d *source) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.data = providerdata.FromDataSourceConfigure(req, resp)
}
func (d *source) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads the available locations.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Synthetic data source identity.", Computed: true}, "organization_id": schema.StringAttribute{MarkdownDescription: "Organization ID; defaults to the provider organization.", Optional: true, Computed: true}, "locations": schema.ListNestedAttribute{MarkdownDescription: "Available locations.", Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Id.", Computed: true}, "name": schema.StringAttribute{MarkdownDescription: "Name.", Computed: true}, "ping_url": schema.StringAttribute{MarkdownDescription: "Ping url.", Computed: true}}}}}}
}

type item struct {
	ID      types.String `tfsdk:"id"`
	Name    types.String `tfsdk:"name"`
	PingURL types.String `tfsdk:"ping_url"`
}
type model struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Items          []item       `tfsdk:"locations"`
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
	if m.OrganizationID.IsUnknown() {
		return
	}
	org := m.OrganizationID.ValueString()
	if m.OrganizationID.IsNull() {
		org = d.data.OrganizationID
	}
	if org == "" {
		resp.Diagnostics.AddError("Invalid organization ID", "Organization ID must not be empty.")
		return
	}
	m.OrganizationID = types.StringValue(org)
	settings, err := d.data.API.Settings(ctx, org)
	items := settings.Locations
	if err != nil {
		resp.Diagnostics.AddError("Reading locations", err.Error())
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	m.ID = types.StringValue(org)
	m.Items = make([]item, 0, len(items))
	for _, v := range items {
		m.Items = append(m.Items, item{ID: types.StringValue(v.ID), Name: types.StringValue(v.Name), PingURL: types.StringValue(v.PingURL)})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
