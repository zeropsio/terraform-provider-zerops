package object_storage

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"strings"
	"testing"
)

type cdnAPI struct {
	storageAPI
	enabled, fail  bool
	calls, updates int
	write          client.StorageWrite
}

func (f *cdnAPI) GetService(context.Context, string) (client.Service, error) {
	return client.Service{ID: "s", ProjectID: "p", Name: "bucket", Base: "object-storage", Status: "ACTIVE", CDNEnabled: &f.enabled}, nil
}
func (f *cdnAPI) CreateObjectStorage(context.Context, string, client.StorageCreate) (client.ServiceProcess, error) {
	return client.ServiceProcess{Service: client.Service{ID: "s"}}, nil
}
func (f *cdnAPI) ListServiceProcesses(context.Context, string, client.PageQuery) (client.Page[client.Process], error) {
	return client.Page[client.Process]{}, nil
}
func (f *cdnAPI) SetStorageCDN(_ context.Context, _ string, enabled bool) (client.Process, error) {
	f.calls++
	if !f.fail {
		f.enabled = enabled
	}
	return client.Process{ID: "cdn"}, nil
}
func (f *cdnAPI) GetProcess(context.Context, string) (client.Process, error) {
	p := client.Process{ID: "cdn", ActionName: "stack.enableCdnStorage", Status: "FINISHED"}
	if f.fail {
		p.Status = "FAILED"
		p.Error = &client.ProcessError{Code: "internalServerError", Message: "unsafe-secret"}
	}
	return p, nil
}
func (f *cdnAPI) UpdateObjectStorage(_ context.Context, _ string, v client.StorageWrite) (client.Process, error) {
	f.updates++
	f.write = v
	return client.Process{ID: "size"}, nil
}
func cdnModel() model {
	return model{ID: types.StringUnknown(), ProjectID: types.StringValue("p"), Hostname: types.StringValue("bucket"), Quota: types.Int64Value(20), Policy: types.StringValue("private"), CDNEnabled: types.BoolValue(true), Raw: jsontypes.NewNormalizedNull(), Bucket: types.StringUnknown(), URL: types.StringUnknown(), Access: types.StringUnknown(), Secret: types.StringUnknown(), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
}
func TestStorageCDNCreateFailureRetainsBucket(t *testing.T) {
	ctx := context.Background()
	api := &cdnAPI{fail: true}
	r := Resource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}}}
	var s resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &s)
	plan := tfsdk.Plan{Schema: s.Schema}
	if d := plan.Set(ctx, cdnModel()); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("terminal CDN failure succeeded")
	}
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Detail(), "unsafe-secret") {
			t.Fatal("unsafe process error disclosed")
		}
	}
	var m model
	if d := resp.State.Get(ctx, &m); d.HasError() || m.ID.ValueString() != "s" || m.CDNEnabled.ValueBool() {
		t.Fatal(d, m)
	}
	if api.calls != 1 {
		t.Fatal("enable replayed")
	}
}

type discardedMetadataAPI struct{ cdnAPI }

func (*discardedMetadataAPI) GetObjectStorage(context.Context, string) (client.StorageInfo, error) {
	return client.StorageInfo{RawPolicy: productionCustomPolicy}, nil
}

func TestStorageCreatePolicyMismatchRetainsKnownState(t *testing.T) {
	ctx := context.Background()
	r := Resource{data: &providerdata.ProviderData{API: &discardedMetadataAPI{}, Locks: &locks.Keyed{}}}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	m := cdnModel()
	m.CDNEnabled = types.BoolValue(false)
	m.Policy = types.StringValue("custom")
	m.Raw = jsontypes.NewNormalizedValue(discardedAWSMetadataPolicy)
	plan := tfsdk.Plan{Schema: schema.Schema}
	if d := plan.Set(ctx, m); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schema.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
	if len(resp.Diagnostics) != 1 || !resp.Diagnostics.HasError() ||
		resp.Diagnostics[0].Summary() != "Creating storage failed" ||
		!strings.Contains(resp.Diagnostics[0].Detail(), "custom policy mismatch") ||
		strings.Contains(resp.Diagnostics[0].Detail(), "dev") {
		t.Fatalf("root policy mismatch not preserved: %v", resp.Diagnostics)
	}
	var got model
	if d := resp.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if got.ID.ValueString() != "s" || got.Policy.ValueString() != "custom" || !semanticEqual(got.Raw.ValueString(), discardedAWSMetadataPolicy) {
		t.Fatalf("created identity or configured policy intent lost: %+v", got)
	}
	for name, value := range map[string]types.String{
		"bucket_name":       got.Bucket,
		"api_url":           got.URL,
		"access_key_id":     got.Access,
		"secret_access_key": got.Secret,
	} {
		if value.IsUnknown() || !value.IsNull() {
			t.Errorf("unavailable generated %s must be known null, got %v", name, value)
		}
	}
}
func TestStorageCDNDisableAndImport(t *testing.T) {
	ctx := context.Background()
	api := &cdnAPI{enabled: true}
	r := Resource{data: &providerdata.ProviderData{API: api, Locks: &locks.Keyed{}}}
	var s resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &s)
	imp := resource.ImportStateResponse{State: tfsdk.State{Schema: s.Schema}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "s"}, &imp)
	if imp.Diagnostics.HasError() {
		t.Fatal(imp.Diagnostics)
	}
	var prior model
	imp.State.Get(ctx, &prior)
	if !prior.CDNEnabled.ValueBool() {
		t.Fatal("import hid true flag")
	}
	m := prior
	m.CDNEnabled = types.BoolValue(false)
	plan := tfsdk.Plan{Schema: s.Schema}
	plan.Set(ctx, m)
	resp := resource.UpdateResponse{State: imp.State}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: imp.State}, &resp)
	if resp.Diagnostics.HasError() || api.enabled || api.calls != 1 || api.write.CDNEnabled != nil {
		t.Fatal(resp.Diagnostics, api)
	}
	plan.Set(ctx, m)
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: resp.State}, &resp)
	if api.calls != 1 {
		t.Fatal("unchanged false disabled again")
	}
}
