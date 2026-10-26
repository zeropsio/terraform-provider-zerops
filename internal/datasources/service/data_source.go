package service

import (
	"context"
	"sort"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	svc "github.com/zeropsio/terraform-provider-zerops/internal/resources/service"
	v "github.com/zeropsio/terraform-provider-zerops/internal/validators"
)

type DataSource struct{ data *providerdata.ProviderData }
type model struct {
	ID       types.String `tfsdk:"id"`
	Project  types.String `tfsdk:"project_id"`
	Hostname types.String `tfsdk:"hostname"`
	Base     types.String `tfsdk:"base"`
	Status   types.String `tfsdk:"status"`
	Access   types.Bool   `tfsdk:"subdomain_access"`
	Current  types.Object `tfsdk:"current_autoscaling"`
	Ports    types.List   `tfsdk:"ports"`
	App      types.String `tfsdk:"active_app_version_id"`
	Created  types.String `tfsdk:"created"`
}

func NewDataSource() datasource.DataSource { return &DataSource{} }
func (d *DataSource) Metadata(_ context.Context, q datasource.MetadataRequest, p *datasource.MetadataResponse) {
	p.TypeName = q.ProviderTypeName + "_service"
}
func (d *DataSource) Configure(_ context.Context, q datasource.ConfigureRequest, p *datasource.ConfigureResponse) {
	d.data = providerdata.FromDataSourceConfigure(q, p)
}
func computedScaling(vertical bool) schema.SingleNestedAttribute {
	a := map[string]schema.Attribute{}
	for k, t := range svc.ScalingTypes(vertical) {
		switch t {
		case types.NumberType:
			a[k] = schema.NumberAttribute{Computed: true, MarkdownDescription: "Effective " + k + "."}
		case types.Int64Type:
			a[k] = schema.Int64Attribute{Computed: true, MarkdownDescription: "Effective " + k + "."}
		case types.StringType:
			a[k] = schema.StringAttribute{Computed: true, MarkdownDescription: "Effective " + k + "."}
		case types.BoolType:
			a[k] = schema.BoolAttribute{Computed: true, MarkdownDescription: "Effective " + k + "."}
		}
	}
	return schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Effective autoscaling settings.", Attributes: a}
}

var portTypes = map[string]attr.Type{"port": types.Int64Type, "protocol": types.StringType, "scheme": types.StringType, "description": types.StringType}

func (d *DataSource) Schema(_ context.Context, _ datasource.SchemaRequest, p *datasource.SchemaResponse) {
	p.Schema = schema.Schema{MarkdownDescription: "Look up a service by ID or project_id and hostname. Does not reconcile lifecycle.", Attributes: map[string]schema.Attribute{
		"id":         schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Service ID; exclusive with the project_id/hostname selector.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"project_id": schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Parent project ID; required with hostname.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"hostname":   schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Service hostname; required with project_id.", Validators: []validator.String{v.Hostname()}},
		"base":       schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical service type."}, "status": schema.StringAttribute{Computed: true, MarkdownDescription: "Raw service status."}, "subdomain_access": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether generated HTTP subdomain access is enabled."},
		"current_autoscaling":   schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Effective autoscaling, including defaults.", Attributes: map[string]schema.Attribute{"vertical_autoscaling": computedScaling(true), "horizontal_autoscaling": computedScaling(false)}},
		"ports":                 schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Declared backend ports sorted by protocol and port.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"port": schema.Int64Attribute{Computed: true, MarkdownDescription: "Backend port."}, "protocol": schema.StringAttribute{Computed: true, MarkdownDescription: "Raw protocol (tcp/udp)."}, "scheme": schema.StringAttribute{Computed: true, MarkdownDescription: "Application scheme."}, "description": schema.StringAttribute{Computed: true, MarkdownDescription: "Port description."}}}},
		"active_app_version_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Active app-version ID; null before a deployment."}, "created": schema.StringAttribute{Computed: true, MarkdownDescription: "RFC3339 creation timestamp."},
	}}
}
func (d *DataSource) ValidateConfig(ctx context.Context, q datasource.ValidateConfigRequest, p *datasource.ValidateConfigResponse) {
	var m model
	p.Diagnostics.Append(q.Config.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	if m.ID.IsUnknown() || m.Project.IsUnknown() || m.Hostname.IsUnknown() {
		return
	}
	id := !m.ID.IsNull()
	project := !m.Project.IsNull()
	name := !m.Hostname.IsNull()
	if (id && (project || name)) || (!id && (!project || !name)) {
		p.Diagnostics.AddError("Invalid service selector", "Configure either id alone, or both project_id and hostname.")
	}
}
func (d *DataSource) Read(ctx context.Context, q datasource.ReadRequest, p *datasource.ReadResponse) {
	var m model
	p.Diagnostics.Append(q.Config.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	var s client.Service
	var e error
	if !m.ID.IsNull() {
		s, e = d.data.API.GetService(ctx, m.ID.ValueString())
	} else {
		s, e = d.data.API.GetServiceByName(ctx, m.Project.ValueString(), m.Hostname.ValueString())
	}
	if e != nil {
		p.Diagnostics.AddError("Looking up service", e.Error())
		return
	}
	m.ID = types.StringValue(s.ID)
	m.Project = types.StringValue(s.ProjectID)
	m.Hostname = types.StringValue(s.Name)
	m.Base = types.StringValue(s.Base)
	m.Status = types.StringValue(s.Status)
	m.Access = types.BoolValue(s.SubdomainAccess)
	m.Current = svc.CurrentValue(s.CurrentAutoscaling)
	m.Created = types.StringValue(s.Created.Format(time.RFC3339))
	m.App = types.StringNull()
	if s.ActiveAppVersionID != "" {
		m.App = types.StringValue(s.ActiveAppVersionID)
	}
	m.Ports = portsValue(s.Ports)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func portsValue(ports []client.Port) types.List {
	ports = append([]client.Port(nil), ports...)
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Protocol != ports[j].Protocol {
			return ports[i].Protocol < ports[j].Protocol
		}
		return ports[i].Port < ports[j].Port
	})
	values := make([]attr.Value, 0, len(ports))
	for _, x := range ports {
		values = append(values, types.ObjectValueMust(portTypes, map[string]attr.Value{"port": types.Int64Value(int64(x.Port)), "protocol": types.StringValue(x.Protocol), "scheme": types.StringValue(x.Scheme), "description": types.StringValue(x.Description)}))
	}
	return types.ListValueMust(types.ObjectType{AttrTypes: portTypes}, values)
}
