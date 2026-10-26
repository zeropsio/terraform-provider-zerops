// Package project_import owns an entire project created from an immutable YAML recipe.
package project_import

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"gopkg.in/yaml.v3"
)

type Resource struct{ data *providerdata.ProviderData }
type Model struct {
	ID             types.String   `tfsdk:"id"`
	OrganizationID types.String   `tfsdk:"organization_id"`
	YAML           types.String   `tfsdk:"yaml"`
	ProjectID      types.String   `tfsdk:"project_id"`
	ProjectName    types.String   `tfsdk:"project_name"`
	Services       types.Map      `tfsdk:"services"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}
type recipe struct {
	Project struct {
		Name string `yaml:"name"`
	} `yaml:"project"`
	Services []map[string]any `yaml:"services"`
}

func parseRecipe(text string) (recipe, error) {
	var value recipe
	decoder := yaml.NewDecoder(strings.NewReader(text))
	if decoder.Decode(&value) != nil || strings.TrimSpace(value.Project.Name) == "" || value.Services == nil {
		return value, fmt.Errorf("YAML must contain a project mapping with a nonempty name and a services list")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return value, fmt.Errorf("YAML must contain exactly one document")
	}
	return value, nil
}

type recipeValidator struct{}

func (recipeValidator) Description(context.Context) string               { return "A project YAML creation recipe." }
func (v recipeValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (recipeValidator) ValidateString(_ context.Context, q validator.StringRequest, p *validator.StringResponse) {
	if q.ConfigValue.IsNull() || q.ConfigValue.IsUnknown() {
		return
	}
	if _, err := parseRecipe(q.ConfigValue.ValueString()); err != nil {
		p.Diagnostics.AddAttributeError(q.Path, "Invalid project recipe", err.Error())
	}
}
func NewResource() resource.Resource { return &Resource{} }
func (r *Resource) Metadata(_ context.Context, q resource.MetadataRequest, p *resource.MetadataResponse) {
	p.TypeName = q.ProviderTypeName + "_project_import"
}
func (r *Resource) Configure(_ context.Context, q resource.ConfigureRequest, p *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(q, p)
}
func (r *Resource) Schema(ctx context.Context, _ resource.SchemaRequest, p *resource.SchemaResponse) {
	p.Schema = schema.Schema{MarkdownDescription: "Exclusively owns a whole project created from a sensitive YAML recipe. YAML or organization changes replace the entire project and destroy all its data, including user-added children. Do not co-manage this project with zerops_project or its service/settings resources. Refresh checks existence and service identities only, not YAML drift. The exact recipe is stored in state; secure state. Terraform import is unsupported because project export cannot recover the original recipe.", Attributes: map[string]schema.Attribute{
		"id":              schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical project ID."},
		"organization_id": schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Organization ID; defaults to the provider organization.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}},
		"yaml":            schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Immutable project creation recipe. Any change replaces the whole project; never reconciled from export.", Validators: []validator.String{recipeValidator{}}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"project_id":      schema.StringAttribute{Computed: true, MarkdownDescription: "Created project ID."},
		"project_name":    schema.StringAttribute{Computed: true, MarkdownDescription: "Current project name."},
		"services":        schema.MapAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Observed non-system service hostname to ID map. Missing children are not recreated."},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true, Update: true, Delete: true})}}
}
func (r *Resource) ModifyPlan(ctx context.Context, q resource.ModifyPlanRequest, p *resource.ModifyPlanResponse) {
	if q.Plan.Raw.IsNull() {
		return
	}
	var m Model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	var org types.String
	p.Diagnostics.Append(q.Config.GetAttribute(ctx, path.Root("organization_id"), &org)...)
	if p.Diagnostics.HasError() {
		return
	}
	if org.IsNull() && m.OrganizationID.IsUnknown() && r.data != nil {
		m.OrganizationID = types.StringValue(r.data.OrganizationID)
	}
	if !q.State.Raw.IsNull() {
		var old Model
		p.Diagnostics.Append(q.State.Get(ctx, &old)...)
		if m.YAML.Equal(old.YAML) && m.OrganizationID.Equal(old.OrganizationID) {
			m.ID = old.ID
			m.ProjectID = old.ProjectID
			m.ProjectName = old.ProjectName
			m.Services = old.Services
		}
	}
	p.Diagnostics.Append(p.Plan.Set(ctx, &m)...)
}
func operation(ctx context.Context, m Model, create bool, d *diag.Diagnostics) (context.Context, context.CancelFunc) {
	var duration time.Duration
	var ds diag.Diagnostics
	if create {
		duration, ds = m.Timeouts.Create(ctx, 30*time.Minute)
	} else {
		duration, ds = m.Timeouts.Delete(ctx, 20*time.Minute)
	}
	d.Append(ds...)
	if duration <= 0 {
		d.AddError("Invalid timeout", "Timeout must be positive.")
	}
	return context.WithTimeout(ctx, duration)
}

// Never include untrusted API/parser text, which can echo any part of the recipe.
func safeError(d *diag.Diagnostics, operation string, err error) {
	if err == nil {
		return
	}
	detail := "The operation failed; YAML and server error details are omitted to protect recipe secrets."
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		detail = "The operation deadline expired or was canceled. The project identity is retained for cleanup."
	}
	var waiting *waiter.Error
	if errors.As(err, &waiting) {
		status := waiting.Status
		switch status {
		case "NEW", "CREATING", "PENDING", "RUNNING", "ROLLBACKING", "CANCELING", "FINISHED", "FAILED", "CANCELED", "ACTIVE", "READY_TO_DEPLOY", "DELETING":
		default:
			status = "unknown"
		}
		detail += fmt.Sprintf(" Resource ID: %q; process ID: %q; last status: %q.", waiting.ResourceID, waiting.ProcessID, status)
	}
	if api, ok := client.AsAPIError(err); ok {
		switch api.Code {
		case "projectImportInvalidYaml", "projectImportInvalidParameter", "serviceStackNameUnavailable", "notAuthorized", "notInSudoMode", "insufficientPermissions", "notAllowedForApplicationToken":
			detail += " API error code: " + api.Code + "."
		}
	}
	d.AddError(operation, detail)
}
func (r *Resource) read(ctx context.Context, m *Model) error {
	project, err := r.data.API.GetProject(ctx, m.ID.ValueString())
	if err != nil {
		return err
	}
	if project.Status == "DELETING" {
		return &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}
	}
	services := map[string]string{}
	for offset := 0; ; {
		page, err := r.data.API.ListServices(ctx, project.ID, client.ServiceQuery{PageQuery: client.PageQuery{Offset: offset, Limit: 100}})
		if err != nil {
			return err
		}
		for _, s := range page.Items {
			if !s.IsSystem {
				services[s.Name] = s.ID
			}
		}
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			break
		}
	}
	m.ProjectName = types.StringValue(project.Name)
	m.ProjectID = types.StringValue(project.ID)
	var ds diag.Diagnostics
	m.Services, ds = types.MapValueFrom(ctx, types.StringType, services)
	if ds.HasError() {
		return fmt.Errorf("mapping services")
	}
	return nil
}
func (r *Resource) Read(ctx context.Context, q resource.ReadRequest, p *resource.ReadResponse) {
	var m Model
	p.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	err := r.read(ctx, &m)
	if client.IsNotFound(err, "projectNotFound") {
		p.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		safeError(&p.Diagnostics, "Reading imported project", err)
		return
	}
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func expectedStatus(s client.Service, definition map[string]any) string {
	category := s.ServiceTypeCategory
	if category == "" {
		category = s.ServiceStackTypeInfo.Category
	}
	startWithoutCode, _ := definition["startWithoutCode"].(bool)
	if (strings.EqualFold(category, "RUNTIME") || strings.EqualFold(category, "USER")) && !startWithoutCode && s.ActiveAppVersionID == "" && definition["buildFromGit"] == nil && definition["build"] == nil && definition["deploy"] == nil {
		return "READY_TO_DEPLOY"
	}
	return "ACTIVE"
}
func (r *Resource) Create(ctx context.Context, q resource.CreateRequest, p *resource.CreateResponse) {
	var m Model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := operation(ctx, m, true, &p.Diagnostics)
	defer cancel()
	if p.Diagnostics.HasError() {
		return
	}
	recipe, err := parseRecipe(m.YAML.ValueString())
	if err != nil {
		safeError(&p.Diagnostics, "Invalid project recipe", err)
		return
	}
	if m.OrganizationID.IsUnknown() || m.OrganizationID.IsNull() {
		m.OrganizationID = types.StringValue(r.data.OrganizationID)
	}
	created, err := r.data.API.ImportProject(ctx, m.OrganizationID.ValueString(), m.YAML.ValueString())
	if err != nil {
		safeError(&p.Diagnostics, "Creating imported project (not retried)", err)
		return
	}
	m.ID = types.StringValue(created.ProjectID)
	m.ProjectID = m.ID
	m.ProjectName = types.StringValue(created.ProjectName)
	services := map[string]string{}
	for _, s := range created.ServiceStacks {
		services[s.Name] = s.ID
	}
	var ds diag.Diagnostics
	m.Services, ds = types.MapValueFrom(ctx, types.StringType, services)
	p.Diagnostics.Append(ds...)
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	// Returned failures take precedence over optimistic ACTIVE snapshots.
	for _, s := range created.ServiceStacks {
		for _, process := range s.Processes {
			_, err = waiter.WaitProcess(ctx, r.data.API, process.ID)
			if err != nil {
				safeError(&p.Diagnostics, "Waiting for imported service process", err)
				return
			}
		}
	}
	_, err = waiter.WaitProjectActive(ctx, r.data.API, created.ProjectID)
	if err != nil {
		safeError(&p.Diagnostics, "Waiting for imported project", err)
		return
	}
	for _, s := range created.ServiceStacks {
		observed, e := r.data.API.GetService(ctx, s.ID)
		if e != nil {
			safeError(&p.Diagnostics, "Reading imported service", e)
			return
		}
		var definition map[string]any
		for _, candidate := range recipe.Services {
			if candidate["hostname"] == s.Name {
				definition = candidate
				break
			}
		}
		if observed.ServiceTypeCategory == "" && observed.ServiceStackTypeInfo.Category == "" {
			settings, e := r.data.API.Settings(ctx, m.OrganizationID.ValueString())
			if e != nil {
				safeError(&p.Diagnostics, "Resolving imported service category", e)
				return
			}
			for _, serviceType := range settings.ServiceTypes {
				if serviceType.ID == observed.ServiceStackTypeID {
					observed.ServiceTypeCategory = serviceType.Category
					if serviceType.IsRuntime {
						observed.ServiceTypeCategory = "RUNTIME"
					}
					break
				}
			}
			if observed.ServiceTypeCategory == "" {
				p.Diagnostics.AddError("Unknown imported service category", "Cannot determine runtime/managed readiness from the service or catalog; project identity is retained for cleanup.")
				return
			}
		}
		ids := []string{}
		for _, process := range s.Processes {
			ids = append(ids, process.ID)
		}
		_, err = waiter.WaitServiceSettled(ctx, r.data.API, s.ID, []string{expectedStatus(observed, definition)}, waiter.ProcessIDs{}, ids)
		if err != nil {
			safeError(&p.Diagnostics, "Waiting for imported service", err)
			return
		}
	}
	if err = r.read(ctx, &m); err != nil {
		safeError(&p.Diagnostics, "Reading imported project", err)
		return
	}
	p.Diagnostics.Append(p.State.Set(ctx, &m)...)
}
func (r *Resource) Update(ctx context.Context, q resource.UpdateRequest, p *resource.UpdateResponse) {
	var m Model
	p.Diagnostics.Append(q.Plan.Get(ctx, &m)...)
	if !p.Diagnostics.HasError() {
		p.Diagnostics.Append(p.State.Set(ctx, &m)...)
	}
}
func (r *Resource) Delete(ctx context.Context, q resource.DeleteRequest, p *resource.DeleteResponse) {
	var m Model
	p.Diagnostics.Append(q.State.Get(ctx, &m)...)
	if p.Diagnostics.HasError() {
		return
	}
	ctx, cancel := operation(ctx, m, false, &p.Diagnostics)
	defer cancel()
	if p.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	// An absent project cannot supply the metadata required by guarded DELETE.
	_, err := r.data.API.GetProject(ctx, id)
	if client.IsNotFound(err, "projectNotFound") {
		return
	}
	if err != nil {
		safeError(&p.Diagnostics, "Reading imported project before deletion", err)
		return
	}
	process, err := r.data.API.DeleteProject(ctx, id)
	if client.IsNotFound(err, "projectNotFound") {
		return
	}
	if err == nil {
		_, err = waiter.WaitProcess(ctx, r.data.API, process.ID)
	}
	if err == nil {
		err = waiter.WaitNotFound(ctx, id, func(ctx context.Context) error { _, e := r.data.API.GetProject(ctx, id); return e }, []string{"projectNotFound"})
	}
	safeError(&p.Diagnostics, "Deleting imported project", err)
}
