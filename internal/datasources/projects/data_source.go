package projects

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"sort"
	"strings"
)

type source struct{ data *providerdata.ProviderData }

func NewDataSource() datasource.DataSource { return &source{} }
func (d *source) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_projects"
}
func (d *source) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.data = providerdata.FromDataSourceConfigure(req, resp)
}
func (d *source) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Lists projects with client-side status filtering.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Resolved organization ID.", Computed: true}, "organization_id": schema.StringAttribute{MarkdownDescription: "Organization ID; defaults to the provider organization.", Optional: true, Computed: true}, "name_contains": schema.StringAttribute{MarkdownDescription: "Filter project names by substring.", Optional: true}, "statuses": schema.SetAttribute{MarkdownDescription: "Raw statuses filtered client-side; empty means all.", Optional: true, ElementType: types.StringType}, "projects": schema.ListNestedAttribute{MarkdownDescription: "Matching projects sorted by ID.", Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Id.", Computed: true}, "name": schema.StringAttribute{MarkdownDescription: "Name.", Computed: true}, "description": schema.StringAttribute{MarkdownDescription: "Description.", Computed: true}, "mode": schema.StringAttribute{MarkdownDescription: "Mode.", Computed: true}, "status": schema.StringAttribute{MarkdownDescription: "Status.", Computed: true}, "location": schema.StringAttribute{MarkdownDescription: "Location.", Computed: true}, "tags": schema.SetAttribute{MarkdownDescription: "Full project tags.", Computed: true, ElementType: types.StringType}}}}}}
}

type item struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Mode        types.String `tfsdk:"mode"`
	Status      types.String `tfsdk:"status"`
	Location    types.String `tfsdk:"location"`
	Tags        types.Set    `tfsdk:"tags"`
}
type model struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	NameContains   types.String `tfsdk:"name_contains"`
	Statuses       types.Set    `tfsdk:"statuses"`
	Projects       []item       `tfsdk:"projects"`
}

func filterProjects(items []client.Project, name string, statuses []string) []client.Project {
	out := make([]client.Project, 0, len(items))
	for _, p := range items {
		if !strings.Contains(p.Name, name) {
			continue
		}
		allowed := len(statuses) == 0
		for _, s := range statuses {
			if s == p.Status {
				allowed = true
				break
			}
		}
		if allowed {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
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
	if m.NameContains.IsUnknown() || m.Statuses.IsUnknown() {
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
	var statuses []string
	resp.Diagnostics.Append(m.Statuses.ElementsAs(ctx, &statuses, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for _, s := range statuses {
		if s == "" {
			resp.Diagnostics.AddError("Invalid status", "Statuses must be nonempty strings.")
			return
		}
	}
	items, err := client.CollectPages(ctx, func(ctx context.Context, q client.PageQuery) (client.Page[client.Project], error) {
		return d.data.API.ListProjects(ctx, org, client.ProjectQuery{PageQuery: q, NameContains: m.NameContains.ValueString()})
	})
	if err != nil {
		resp.Diagnostics.AddError("Listing projects", err.Error())
		return
	}
	m.ID = types.StringValue(org)
	m.Projects = make([]item, 0, len(items))
	for _, p := range filterProjects(items, m.NameContains.ValueString(), statuses) {
		tags, diags := types.SetValueFrom(ctx, types.StringType, p.TagList)
		resp.Diagnostics.Append(diags...)
		location := types.StringNull()
		if p.PrimaryInstanceLocation.ID != "" {
			location = types.StringValue(p.PrimaryInstanceLocation.ID)
		}
		m.Projects = append(m.Projects, item{ID: types.StringValue(p.ID), Name: types.StringValue(p.Name), Description: types.StringPointerValue(p.Description), Mode: types.StringValue(p.Mode), Status: types.StringValue(p.Status), Location: location, Tags: tags})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
