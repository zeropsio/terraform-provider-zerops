package organization

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
	resp.TypeName = req.ProviderTypeName + "_organization"
}
func (d *source) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.data = providerdata.FromDataSourceConfigure(req, resp)
}
func (d *source) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads an organization membership by organization ID.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Organization ID; defaults to the provider organization.", Optional: true, Computed: true}, "name": schema.StringAttribute{MarkdownDescription: "Organization account name.", Computed: true}, "role": schema.StringAttribute{MarkdownDescription: "Membership role.", Computed: true}}}
}

type model struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Role types.String `tfsdk:"role"`
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
	if m.ID.IsUnknown() {
		return
	}
	id := m.ID.ValueString()
	if m.ID.IsNull() {
		id = d.data.OrganizationID
	}
	if id == "" {
		resp.Diagnostics.AddError("Invalid organization ID", "Organization ID must not be empty.")
		return
	}
	orgs, err := d.data.API.ListOrganizations(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading organization", err.Error())
		return
	}
	found := false
	for _, org := range orgs {
		if org.ID == id {
			m.ID = types.StringValue(org.ID)
			m.Name = types.StringValue(org.Name)
			m.Role = types.StringValue(org.RoleCode)
			found = true
			break
		}
	}
	if !found {
		resp.Diagnostics.AddError("Organization not found", "No membership matches the requested organization ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
