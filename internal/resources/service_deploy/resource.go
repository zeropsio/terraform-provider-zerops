// Package service_deploy implements immutable deployment invocations.
package service_deploy

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/validators"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"gopkg.in/yaml.v3"
)

type deployResource struct{ data *providerdata.ProviderData }
type model struct {
	ID          types.String   `tfsdk:"id"`
	ServiceID   types.String   `tfsdk:"service_id"`
	ArchivePath types.String   `tfsdk:"archive_path"`
	SourceDir   types.String   `tfsdk:"source_dir"`
	Git         types.Object   `tfsdk:"git"`
	YAML        types.String   `tfsdk:"zerops_yaml"`
	Setup       types.String   `tfsdk:"setup"`
	Build       types.Bool     `tfsdk:"build"`
	Triggers    types.Map      `tfsdk:"triggers"`
	SourceHash  types.String   `tfsdk:"source_hash"`
	GitCommit   types.String   `tfsdk:"git_commit"`
	Status      types.String   `tfsdk:"status"`
	Active      types.Bool     `tfsdk:"active"`
	ProcessID   types.String   `tfsdk:"process_id"`
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
}
type gitModel struct {
	URL   types.String `tfsdk:"url"`
	Ref   types.String `tfsdk:"ref"`
	Mode  types.String `tfsdk:"mode"`
	Token types.String `tfsdk:"auth_token"`
}

func NewResource() resource.Resource { return &deployResource{} }
func (r *deployResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_deploy"
}
func (r *deployResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerdata.FromResourceConfigure(req, resp)
}
func (r *deployResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	input := func(desc string, required bool) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Required: required, Optional: !required, PlanModifiers: replace, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}}
	}
	resp.Schema = schema.Schema{MarkdownDescription: "An immutable deployment invocation. Destroy only forgets this invocation; it never stops or rolls back the service. Import is unsupported because source inputs cannot be reconstructed. Server Git mode builds branch HEAD at build time, not a pinned commit, and requires a repository zerops.yml.", Attributes: map[string]schema.Attribute{
		"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "App-version identifier."},
		"service_id":   input("Target runtime service ID.", true),
		"archive_path": input("Local tar.gz archive; exactly one source is required.", false),
		"source_dir":   input("Local source directory, deterministically archived excluding .git directories.", false),
		"git": schema.SingleNestedAttribute{Optional: true, MarkdownDescription: "Git source; provider mode fetches the exact planned commit and supports runtime-only ZEROPS_GIT_TOKEN when auth_token is absent. Supply that environment variable separately during plan and apply. Server mode supports public branches only and ignores the environment token.", PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()}, Attributes: map[string]schema.Attribute{
			"url": input("HTTPS repository URL without embedded credentials.", true), "ref": input("Branch, tag or full commit SHA; server mode only accepts branches.", true),
			"mode":       schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("provider"), MarkdownDescription: "Fetch mode: provider (pinned archive) or server (public branch HEAD).", Validators: []validator.String{stringvalidator.OneOf("provider", "server")}, PlanModifiers: replace},
			"auth_token": schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Provider-side Git authentication token, never passed to the API but stored in Terraform plan/state. Prefer runtime-only ZEROPS_GIT_TOKEN; an explicit auth_token takes precedence.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: replace}}},
		"zerops_yaml": schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Raw deployment YAML, at most 50 KiB; may contain secrets.", PlanModifiers: replace},
		"setup":       schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "YAML setup name; defaults to the target service hostname.", PlanModifiers: replace, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"build":       schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), MarkdownDescription: "Build before deploy; false uploads a ready-to-run archive.", PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
		"triggers":    schema.MapAttribute{Optional: true, Computed: true, ElementType: types.StringType, Default: mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{})), MarkdownDescription: "Nonsecret replacement triggers.", PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()}},
		"source_hash": schema.StringAttribute{Computed: true, MarkdownDescription: "Plan-time archive SHA-256 or resolved Git commit. Content changes replace the invocation."},
		"git_commit":  schema.StringAttribute{Computed: true, MarkdownDescription: "Resolved planned Git commit; server mode does not prove this is the built commit."},
		"status":      schema.StringAttribute{Computed: true, MarkdownDescription: "Raw app-version status, or PURGED when history was removed."},
		"active":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this invocation is the current active service release."},
		"process_id":  schema.StringAttribute{Computed: true, MarkdownDescription: "Build/deploy process identifier."},
	}, Blocks: map[string]schema.Block{"timeouts": validators.TimeoutsBlock(ctx, timeouts.Opts{Create: true})}}
}
func validateYAML(s string) error {
	if len(s) == 0 || len(s) > 50*1024 {
		return fmt.Errorf("zerops_yaml must contain raw YAML of at most 50 KiB")
	}
	var v map[string]any
	if yaml.Unmarshal([]byte(s), &v) != nil || len(v) == 0 {
		return fmt.Errorf("zerops_yaml must be a valid YAML mapping")
	}
	return nil
}
func (r *deployResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m model
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	known := !m.ArchivePath.IsUnknown() && !m.SourceDir.IsUnknown() && !m.Git.IsUnknown()
	n := 0
	for _, v := range []bool{!m.ArchivePath.IsNull(), !m.SourceDir.IsNull(), !m.Git.IsNull()} {
		if v {
			n++
		}
	}
	if known && n != 1 {
		resp.Diagnostics.AddError("Invalid deployment source", "Exactly one of archive_path, source_dir, or git is required.")
	}
	if !m.YAML.IsUnknown() {
		if e := validateYAML(m.YAML.ValueString()); e != nil {
			resp.Diagnostics.AddError("Invalid deployment YAML", e.Error())
		}
	}
	if !m.Git.IsNull() && !m.Git.IsUnknown() {
		var g gitModel
		resp.Diagnostics.Append(m.Git.As(ctx, &g, basetypes.ObjectAsOptions{})...)
		if g.Mode.ValueString() == "server" {
			if !g.Token.IsNull() || (!m.Build.IsNull() && !m.Build.IsUnknown() && !m.Build.ValueBool()) {
				resp.Diagnostics.AddError("Unsupported server Git inputs", "Server mode rejects auth_token and build=false; use provider mode instead.")
			}
		}
	}
}
func (r *deployResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	hash := ""
	var e error
	m.GitCommit = types.StringNull()
	if m.Git.IsUnknown() {
		m.GitCommit = types.StringUnknown()
	}
	switch {
	case !m.SourceDir.IsNull() && !m.SourceDir.IsUnknown():
		hash, e = directoryHash(m.SourceDir.ValueString())
	case !m.ArchivePath.IsNull() && !m.ArchivePath.IsUnknown():
		hash, e = archiveHash(m.ArchivePath.ValueString())
	case !m.Git.IsNull() && !m.Git.IsUnknown():
		var g gitModel
		resp.Diagnostics.Append(m.Git.As(ctx, &g, basetypes.ObjectAsOptions{})...)
		if !g.URL.IsUnknown() && !g.Ref.IsUnknown() && !g.Mode.IsUnknown() && !g.Token.IsUnknown() {
			hash, e = resolveGit(ctx, g.URL.ValueString(), g.Ref.ValueString(), g.Mode.ValueString(), runtimeGitToken(g.Mode.ValueString(), g.Token))
			m.GitCommit = types.StringValue(hash)
		} else {
			m.GitCommit = types.StringUnknown()
		}
	}
	if e != nil {
		resp.Diagnostics.AddError("Cannot resolve deployment source", e.Error())
		return
	}
	if hash != "" {
		m.SourceHash = types.StringValue(hash)
	} else {
		m.SourceHash = types.StringUnknown()
	}
	if !req.State.Raw.IsNull() {
		var old model
		resp.Diagnostics.Append(req.State.Get(ctx, &old)...)
		if hash != "" && !old.SourceHash.IsNull() && !old.SourceHash.IsUnknown() && old.SourceHash.ValueString() != hash {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("source_hash"))
		}
	}
	if m.Setup.IsUnknown() && !m.ServiceID.IsUnknown() && r.data != nil {
		var svc client.Service
		svc, e = r.data.API.GetService(ctx, m.ServiceID.ValueString())
		if e != nil {
			resp.Diagnostics.AddError("Cannot resolve setup", e.Error())
			return
		}
		m.Setup = types.StringValue(svc.Name)
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &m)...)
}
func (r *deployResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	duration, d := m.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	if duration <= 0 {
		resp.Diagnostics.AddError("Invalid create timeout", "The create timeout must be positive.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	fail := func(e error) { resp.Diagnostics.AddError("Deployment failed", e.Error()) }
	svc, e := r.data.API.GetService(ctx, m.ServiceID.ValueString())
	if e != nil {
		fail(e)
		return
	}
	if m.Setup.IsUnknown() || m.Setup.IsNull() {
		m.Setup = types.StringValue(svc.Name)
	}
	unlock, e := r.data.Locks.Lock(ctx, locks.ServiceKey(svc.ID))
	if e != nil {
		fail(e)
		return
	}
	defer unlock()
	baseline, e := waiter.SnapshotProcessIDs(ctx, r.data.API, svc.ID)
	if e != nil {
		fail(e)
		return
	}
	var g gitModel
	if !m.Git.IsNull() {
		resp.Diagnostics.Append(m.Git.As(ctx, &g, basetypes.ObjectAsOptions{})...)
		if resp.Diagnostics.HasError() {
			return
		}
		if m.GitCommit.IsUnknown() || m.GitCommit.IsNull() {
			commit, err := resolveGit(ctx, g.URL.ValueString(), g.Ref.ValueString(), g.Mode.ValueString(), runtimeGitToken(g.Mode.ValueString(), g.Token))
			if err != nil {
				fail(err)
				return
			}
			m.GitCommit = types.StringValue(commit)
			m.SourceHash = types.StringValue(commit)
		}
		if g.Mode.ValueString() == "server" && (!g.Token.IsNull() || !m.Build.ValueBool()) {
			fail(fmt.Errorf("server mode rejects auth_token and build=false"))
			return
		}
	}
	if e = validateYAML(m.YAML.ValueString()); e != nil {
		fail(e)
		return
	}
	if !m.Git.IsNull() && g.Mode.ValueString() == "server" {
		if _, e = resolveGit(ctx, g.URL.ValueString(), g.Ref.ValueString(), "server", ""); e != nil {
			fail(e)
			return
		}
		p, e := r.data.API.TriggerGitPipeline(ctx, svc.ID, client.GitPipelineRequest{BuildFromGit: g.URL.ValueString() + "@" + g.Ref.ValueString(), Setup: m.Setup.ValueString(), ZeropsYaml: m.YAML.ValueString()})
		if e != nil {
			fail(e)
			return
		}
		if p.Process == nil {
			fail(fmt.Errorf("pipeline returned no process"))
			return
		}
		id := p.AppVersionID
		if id == "" && p.Process.AppVersion != nil {
			id = p.Process.AppVersion.ID
		}
		if id == "" && p.AppVersion != nil {
			id = p.AppVersion.ID
		}
		if id == "" {
			id = p.Process.AppVersionID
		}
		m.ID = types.StringValue(id)
		m.ProcessID = types.StringValue(p.Process.ID)
	} else {
		source := m.ArchivePath.ValueString()
		dir := m.SourceDir.ValueString()
		if !m.Git.IsNull() {
			dir, e = os.MkdirTemp("", "zerops-git-")
			if e != nil {
				fail(e)
				return
			}
			defer os.RemoveAll(dir)
			if e = materializeGit(ctx, dir, g.URL.ValueString(), m.GitCommit.ValueString(), runtimeGitToken(g.Mode.ValueString(), g.Token)); e != nil {
				fail(e)
				return
			}
		}
		if dir != "" {
			f, e := createSourceArchive(dir)
			if e != nil {
				fail(e)
				return
			}
			source = f.Name()
			defer os.Remove(source)
			e = writeDirectory(f, dir)
			ce := f.Close()
			if e != nil {
				fail(e)
				return
			}
			if ce != nil {
				fail(ce)
				return
			}
		}
		hash, e := archiveHash(source)
		if e != nil {
			fail(e)
			return
		}
		if m.Git.IsNull() && !m.SourceHash.IsUnknown() && hash != m.SourceHash.ValueString() {
			fail(fmt.Errorf("source changed after planning; create a new plan"))
			return
		}
		if m.Git.IsNull() {
			m.SourceHash = types.StringValue(hash)
		}
		f, e := os.Open(source)
		if e != nil {
			fail(e)
			return
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil {
			fail(e)
			return
		}
		upload, e := r.data.API.CreateAppVersion(ctx, svc.ID, "terraform")
		if e != nil {
			fail(e)
			return
		}
		m.ID = types.StringValue(upload.ID)
		m.Status = types.StringValue(upload.Status)
		m.Active = types.BoolValue(false)
		m.ProcessID = types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		if e = r.data.API.UploadArchive(ctx, upload.UploadURL, f, st.Size()); e != nil {
			fail(e)
			return
		}
		p, e := r.data.API.DeployAppVersion(ctx, upload.ID, client.DeployRequest{ZeropsYaml: m.YAML.ValueString(), Setup: m.Setup.ValueString()}, m.Build.ValueBool())
		if e != nil {
			fail(e)
			return
		}
		m.ProcessID = types.StringValue(p.ID)
	}
	m.Status = types.StringValue("PENDING")
	m.Active = types.BoolValue(false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if m.ID.ValueString() == "" || m.ProcessID.ValueString() == "" {
		fail(fmt.Errorf("deployment response omitted app-version or process ID"))
		return
	}
	if !m.Git.IsNull() && g.Mode.ValueString() == "server" {
		app, e := r.data.API.GetAppVersion(ctx, m.ID.ValueString())
		if e != nil {
			fail(e)
			return
		}
		branch, _ := app.PublicGitSource["branchName"].(string)
		if branch != g.Ref.ValueString() {
			fail(fmt.Errorf("server selected a different Git branch; refusing fallback deployment"))
			return
		}
	}
	if _, e = waiter.WaitDeploy(ctx, r.data.API, m.ID.ValueString(), m.ProcessID.ValueString(), svc.ID, baseline); e != nil {
		fail(e)
		return
	}
	r.refresh(ctx, &m, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *deployResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var old, plan model
	resp.Diagnostics.Append(req.State.Get(ctx, &old)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Every deployment input requires replacement; only local timeouts update.
	old.Timeouts = plan.Timeouts
	resp.Diagnostics.Append(resp.State.Set(ctx, &old)...)
}
func (r *deployResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {}
func (r *deployResource) refresh(ctx context.Context, m *model, ds *diag.Diagnostics) {
	app, e := r.data.API.GetAppVersion(ctx, m.ID.ValueString())
	if client.IsNotFound(e, "appVersionNotFound") {
		m.Status = types.StringValue("PURGED")
		m.Active = types.BoolValue(false)
		return
	}
	if e != nil {
		ds.AddError("Cannot read deployment", e.Error())
		return
	}
	m.Status = types.StringValue(app.Status)
	svc, e := r.data.API.GetService(ctx, m.ServiceID.ValueString())
	if client.IsNotFound(e, "serviceStackNotFound") {
		m.Active = types.BoolValue(false)
		ds.AddWarning("Deployment parent removed", "The parent service no longer exists; this historical invocation remains in state.")
		return
	}
	if e != nil {
		ds.AddError("Cannot read deployment service", e.Error())
		return
	}
	active := svc.ActiveAppVersionID
	if svc.ActiveAppVersion != nil {
		active = svc.ActiveAppVersion.ID
	}
	m.Active = types.BoolValue(active == m.ID.ValueString())
}
func (r *deployResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.refresh(ctx, &m, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
