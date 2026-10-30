package service_types

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
	resp.TypeName = req.ProviderTypeName + "_service_types"
}
func (d *source) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.data = providerdata.FromDataSourceConfigure(req, resp)
}
func (d *source) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads the organization service type catalog. Managed versions use qualified family:mode@version names; zerops_service requires this form for dual-mode managed types.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Resolved organization ID.", Computed: true}, "organization_id": schema.StringAttribute{MarkdownDescription: "Organization ID; defaults to the provider organization.", Optional: true, Computed: true}, "types": schema.ListNestedAttribute{MarkdownDescription: "Catalog types including disabled versions; not every version is creatable.", Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Id.", Computed: true}, "name": schema.StringAttribute{MarkdownDescription: "Name.", Computed: true}, "category": schema.StringAttribute{MarkdownDescription: "Category.", Computed: true}, "modes": schema.ListAttribute{MarkdownDescription: "Modes.", Computed: true, ElementType: types.StringType}, "operating_systems": schema.ListAttribute{MarkdownDescription: "Operating systems.", Computed: true, ElementType: types.StringType}, "versions": schema.ListNestedAttribute{MarkdownDescription: "Versions sorted by ID, including disabled versions.", Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Id.", Computed: true}, "name": schema.StringAttribute{MarkdownDescription: "Name.", Computed: true}, "status": schema.StringAttribute{MarkdownDescription: "Status.", Computed: true}, "version_number": schema.StringAttribute{MarkdownDescription: "Version number.", Computed: true}, "exact_version_number": schema.StringAttribute{MarkdownDescription: "Exact version number.", Computed: true}}}}}}}}}
}

type version struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Status             types.String `tfsdk:"status"`
	VersionNumber      types.String `tfsdk:"version_number"`
	ExactVersionNumber types.String `tfsdk:"exact_version_number"`
}
type item struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Category         types.String `tfsdk:"category"`
	Modes            types.List   `tfsdk:"modes"`
	OperatingSystems types.List   `tfsdk:"operating_systems"`
	Versions         []version    `tfsdk:"versions"`
}
type model struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Types          []item       `tfsdk:"types"`
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
	if err != nil {
		resp.Diagnostics.AddError("Reading service types", err.Error())
		return
	}
	items := settings.ServiceTypes
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	m.ID = types.StringValue(org)
	m.Types = make([]item, 0, len(items))
	for _, v := range items {
		modes, diags := types.ListValueFrom(ctx, types.StringType, v.Modes)
		resp.Diagnostics.Append(diags...)
		systems, diags := types.ListValueFrom(ctx, types.StringType, v.OperatingSystems)
		resp.Diagnostics.Append(diags...)
		entry := item{ID: types.StringValue(v.ID), Name: types.StringValue(v.Name), Category: types.StringValue(v.Category), Modes: modes, OperatingSystems: systems, Versions: make([]version, 0, len(v.Versions))}
		sort.Slice(v.Versions, func(i, j int) bool { return v.Versions[i].ID < v.Versions[j].ID })
		for _, ver := range v.Versions {
			entry.Versions = append(entry.Versions, version{ID: types.StringValue(ver.ID), Name: types.StringValue(ver.Name), Status: types.StringValue(ver.Status), VersionNumber: types.StringValue(ver.VersionNumber), ExactVersionNumber: types.StringPointerValue(ver.ExactVersionNumber)})
		}
		m.Types = append(m.Types, entry)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
