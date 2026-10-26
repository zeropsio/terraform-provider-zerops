package project

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/resources/project"
)

type source struct{ data *providerdata.ProviderData }

func NewDataSource() datasource.DataSource { return &source{} }
func (d *source) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}
func (d *source) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.data = providerdata.FromDataSourceConfigure(req, resp)
}
func (d *source) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Looks up exactly one project by ID or exact case-sensitive name.", Attributes: map[string]schema.Attribute{"id": schema.StringAttribute{MarkdownDescription: "Id.", Optional: true, Computed: true}, "name": schema.StringAttribute{MarkdownDescription: "Name.", Optional: true, Computed: true}, "organization_id": schema.StringAttribute{MarkdownDescription: "Organization id.", Optional: true, Computed: true}, "description": schema.StringAttribute{MarkdownDescription: "Description.", Computed: true}, "mode": schema.StringAttribute{MarkdownDescription: "Mode.", Computed: true}, "location": schema.StringAttribute{MarkdownDescription: "Location.", Computed: true}, "env_isolation": schema.StringAttribute{MarkdownDescription: "Env isolation.", Computed: true}, "ssh_isolation": schema.StringAttribute{MarkdownDescription: "Ssh isolation.", Computed: true}, "public_ipv4": schema.StringAttribute{MarkdownDescription: "Public ipv4.", Computed: true}, "public_ipv6": schema.StringAttribute{MarkdownDescription: "Public ipv6.", Computed: true}, "zerops_subdomain_host": schema.StringAttribute{MarkdownDescription: "Zerops subdomain host.", Computed: true}, "status": schema.StringAttribute{MarkdownDescription: "Status.", Computed: true}, "created": schema.StringAttribute{MarkdownDescription: "Created.", Computed: true}, "tags": schema.SetAttribute{MarkdownDescription: "Project tags.", Computed: true, ElementType: types.StringType}, "public_ipv4_shared": schema.BoolAttribute{MarkdownDescription: "Shared public IPv4 enabled.", Computed: true}}}
}

type model struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	OrganizationID   types.String `tfsdk:"organization_id"`
	Description      types.String `tfsdk:"description"`
	Mode             types.String `tfsdk:"mode"`
	Location         types.String `tfsdk:"location"`
	EnvIsolation     types.String `tfsdk:"env_isolation"`
	SSHIsolation     types.String `tfsdk:"ssh_isolation"`
	PublicIPv4       types.String `tfsdk:"public_ipv4"`
	PublicIPv6       types.String `tfsdk:"public_ipv6"`
	Subdomain        types.String `tfsdk:"zerops_subdomain_host"`
	Status           types.String `tfsdk:"status"`
	Created          types.String `tfsdk:"created"`
	Tags             types.Set    `tfsdk:"tags"`
	PublicIPv4Shared types.Bool   `tfsdk:"public_ipv4_shared"`
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
	if m.ID.IsUnknown() || m.Name.IsUnknown() || m.OrganizationID.IsUnknown() {
		return
	}
	if m.ID.IsNull() == m.Name.IsNull() {
		resp.Diagnostics.AddError("Invalid project selector", "Configure exactly one of id or name.")
		return
	}
	if (!m.ID.IsNull() && m.ID.ValueString() == "") || (!m.Name.IsNull() && m.Name.ValueString() == "") {
		resp.Diagnostics.AddError("Invalid project selector", "Configured id or name must not be empty.")
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
	var p client.Project
	var err error
	if !m.ID.IsNull() {
		p, err = d.data.API.GetProject(ctx, m.ID.ValueString())
	} else {
		p, err = d.data.API.GetProjectByName(ctx, org, m.Name.ValueString(), nil)
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading project", err.Error())
		return
	}
	if p.ClientID != org {
		resp.Diagnostics.AddError("Project organization mismatch", "The requested project does not belong to the selected organization.")
		return
	}
	env, err := d.data.API.ListProjectEnv(ctx, p.ID)
	if err != nil {
		resp.Diagnostics.AddError("Reading project environment", err.Error())
		return
	}
	var mapped project.Model
	resp.Diagnostics.Append(project.Map(ctx, &mapped, p, env)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = mapped.ID
	m.Name = mapped.Name
	m.OrganizationID = mapped.OrganizationID
	m.Description = mapped.Description
	m.Mode = mapped.Mode
	m.Location = mapped.Location
	m.EnvIsolation = mapped.EnvIsolation
	m.SSHIsolation = mapped.SSHIsolation
	m.PublicIPv4 = mapped.PublicIPv4
	m.PublicIPv6 = mapped.PublicIPv6
	m.Subdomain = mapped.Subdomain
	m.Status = mapped.Status
	m.Created = mapped.Created
	m.Tags = mapped.Tags
	m.PublicIPv4Shared = mapped.PublicIPv4Shared
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
