package regions

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"sort"
	"strings"
)

type source struct{ data *providerdata.ProviderData }

func NewDataSource() datasource.DataSource { return &source{} }
func (d *source) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_regions"
}
func (d *source) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.data = providerdata.FromDataSourceConfigure(req, resp)
}
func (d *source) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads the available regions.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Synthetic data source identity.", Computed: true}, "regions": schema.ListNestedAttribute{MarkdownDescription: "Available regions.", Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"name": schema.StringAttribute{MarkdownDescription: "Name.", Computed: true}, "is_default": schema.BoolAttribute{MarkdownDescription: "Default region.", Computed: true}, "address": schema.StringAttribute{MarkdownDescription: "Address.", Computed: true}}}}}}
}

type item struct {
	Name      types.String `tfsdk:"name"`
	IsDefault types.Bool   `tfsdk:"is_default"`
	Address   types.String `tfsdk:"address"`
}
type model struct {
	ID    types.String `tfsdk:"id"`
	Items []item       `tfsdk:"regions"`
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

	items, err := d.data.API.Regions(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading regions", err.Error())
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	m.ID = types.StringValue(strings.TrimRight(d.data.Endpoint, "/"))
	m.Items = make([]item, 0, len(items))
	for _, v := range items {
		m.Items = append(m.Items, item{Name: types.StringValue(v.Name), IsDefault: types.BoolValue(v.IsDefault), Address: types.StringValue(v.Address)})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
