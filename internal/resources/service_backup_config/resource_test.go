package service_backup_config

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/locks"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"reflect"
	"testing"
)

func TestCronValidation(t *testing.T) {
	for _, s := range []string{"* * * * *", "15 3 * * *", "*/5 0-23 * JAN,MAR MON-FRI", "0,30 0 * * 0"} {
		if e := validateCron(s); e != nil {
			t.Errorf("valid cron %q: %v", s, e)
		}
	}
	for _, s := range []string{"", "@every 10s", "* * * * * *", "60 * * * *", "0 24 * * *", "*/0 * * * *", "0 0 * * 7", "0 0 * * 5-1"} {
		if validateCron(s) == nil {
			t.Errorf("invalid cron accepted %q", s)
		}
	}
}
func TestPolicyRoundTrip(t *testing.T) {
	p := &client.BackupRetentionPolicy{MaxTotalFiles: 10, MaxTotalGiB: 2, ProtectedTags: []string{"test", "a:b"}, MinDaily: 1, MaxDaily: 3, MinWeekly: 2, MaxWeekly: 4, MinMonthly: 3, MaxMonthly: 5}
	got, known := policyInput(policyValue(p))
	if !known || !reflect.DeepEqual(p, got) {
		t.Fatal(got, known)
	}
	v, known := policyInput(types.ObjectUnknown(policyTypes()))
	if known || v != nil {
		t.Fatal("unknown policy guessed")
	}
	if !policyValue(nil).IsNull() {
		t.Fatal("null policy not absent")
	}
}

type backupAPI struct {
	client.API
	written       client.BackupConfigWrite
	result        client.BackupConfig
	err           error
	reads, writes int
}

func (f *backupAPI) GetService(context.Context, string) (client.Service, error) {
	return client.Service{ID: "service"}, nil
}
func (f *backupAPI) SetBackupConfig(_ context.Context, _ string, v client.BackupConfigWrite) (*client.Process, error) {
	f.writes++
	f.written = v
	return nil, f.err
}
func (f *backupAPI) GetBackupConfig(context.Context, string) (client.BackupConfig, error) {
	f.reads++
	return f.result, f.err
}
func TestBackupNullProcessAndReset(t *testing.T) {
	f := &backupAPI{result: client.BackupConfig{DefaultRetentionPolicy: &client.BackupRetentionPolicy{MaxTotalFiles: 50}}}
	r := Resource{data: &providerdata.ProviderData{API: f, Locks: &locks.Keyed{}}}
	m := model{ServiceID: types.StringValue("service"), Period: types.StringNull(), Retention: policyValue(nil)}
	var d diag.Diagnostics
	saves := 0
	r.apply(context.Background(), &m, &d, func() { saves++ }, false)
	if d.HasError() || f.writes != 1 || f.reads != 1 || saves != 2 || m.ID.ValueString() != "service" || f.written.BackupPeriod != nil || f.written.RetentionPolicy != nil {
		t.Fatal(d, f, saves, m)
	}
	if m.Default.Attributes()["max_total_files"].(types.Int64).ValueInt64() != 50 {
		t.Fatal("server default ignored")
	}
	f.result.DefaultRetentionPolicy.MaxTotalFiles = 40
	r.apply(context.Background(), &m, &d, func() {}, false)
	if m.Default.Attributes()["max_total_files"].(types.Int64).ValueInt64() != 40 {
		t.Fatal("server defaults hidden")
	}
}
func TestBackupFailureAndImport(t *testing.T) {
	f := &backupAPI{err: &client.APIError{HTTPStatus: 400, Code: "backupUnsupportedStackType"}}
	r := Resource{data: &providerdata.ProviderData{API: f, Locks: &locks.Keyed{}}}
	m := model{ServiceID: types.StringValue("s"), Period: types.StringNull(), Retention: policyValue(nil)}
	var d diag.Diagnostics
	r.apply(context.Background(), &m, &d, func() { t.Fatal("failed mutation persisted identity") }, false)
	if !d.HasError() {
		t.Fatal("unsupported capability accepted")
	}
	f.err = nil
	f.result = client.BackupConfig{DefaultRetentionPolicy: &client.BackupRetentionPolicy{MaxTotalFiles: 50}}
	var schemaResp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResp)
	resp := resource.ImportStateResponse{}
	resp.State.Schema = schemaResp.Schema
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "s"}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var imported model
	if d := resp.State.Get(context.Background(), &imported); d.HasError() || imported.ID.ValueString() != "s" || !imported.Period.IsNull() {
		t.Fatal(d, imported)
	}
}
func TestBackupAbsenceClassification(t *testing.T) {
	for _, e := range []error{&client.APIError{HTTPStatus: 404, Code: "serviceStackNotFound"}, &client.APIError{HTTPStatus: 403, Code: "forbidden"}, &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}} {
		if absentDuringDelete(e, true) {
			t.Fatal("non-absence removed state", e)
		}
	}
	e := &client.APIError{HTTPStatus: 400, Code: "serviceStackNotFound"}
	if !absentDuringDelete(e, true) || absentDuringDelete(e, false) {
		t.Fatal("wrong endpoint absence handling")
	}
}

func TestBackupUnknownConfigurationDoesNotMutate(t *testing.T) {
	f := &backupAPI{}
	r := Resource{data: &providerdata.ProviderData{API: f, Locks: &locks.Keyed{}}}
	for _, m := range []model{
		{ServiceID: types.StringUnknown(), Period: types.StringNull(), Retention: policyValue(nil)},
		{ServiceID: types.StringValue("s"), Period: types.StringUnknown(), Retention: policyValue(nil)},
		{ServiceID: types.StringValue("s"), Period: types.StringNull(), Retention: types.ObjectUnknown(policyTypes())},
	} {
		var d diag.Diagnostics
		r.apply(context.Background(), &m, &d, func() { t.Fatal("unknown configuration saved") }, false)
		if !d.HasError() || f.writes != 0 || f.reads != 0 {
			t.Fatal("unknown input reached API", d, f)
		}
	}
}

func TestBackupRetentionConditionalRequiredness(t *testing.T) {
	ctx := context.Background()
	r := &Resource{}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	valid := policyValue(&client.BackupRetentionPolicy{MaxTotalFiles: 10, MaxTotalGiB: 1, ProtectedTags: []string{}, MinDaily: 1})
	missing := valid.Attributes()
	missing["max_total_gib"] = types.Int64Null()
	badMin := policyValue(&client.BackupRetentionPolicy{MaxTotalGiB: 1, ProtectedTags: []string{}, MinDaily: 50, MinWeekly: 51})
	badTag := policyValue(&client.BackupRetentionPolicy{MaxTotalGiB: 1, ProtectedTags: []string{"bad tag"}})
	for _, tc := range []struct {
		name  string
		value types.Object
		bad   bool
	}{
		{"absent", policyValue(nil), false},
		{"valid", valid, false},
		{"unknown", types.ObjectUnknown(policyTypes()), false},
		{"missing child", types.ObjectValueMust(policyTypes(), missing), true},
		{"minima", badMin, true},
		{"tag", badTag, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := model{ID: types.StringNull(), ServiceID: types.StringValue("s"), Period: types.StringNull(), Retention: tc.value, Default: types.ObjectNull(policyTypes()), Timeouts: timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType, "update": types.StringType, "delete": types.StringType})}}
			state := tfsdk.State{Schema: schema.Schema}
			if d := state.Set(ctx, m); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.ValidateConfigResponse{}
			r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: state.Raw}}, &resp)
			if resp.Diagnostics.HasError() != tc.bad {
				t.Fatal(resp.Diagnostics)
			}
		})
	}
}
