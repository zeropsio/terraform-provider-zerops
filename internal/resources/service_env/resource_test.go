package service_env

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	testingresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"os"
	"strings"
	"testing"
	"time"
)

func TestContentValidation(t *testing.T) {
	for _, tt := range []struct {
		value types.String
		bad   bool
	}{
		{types.StringValue(strings.Repeat("x", 8192)), false},
		{types.StringValue(strings.Repeat("x", 8193)), true},
		{types.StringValue("line\nbreak"), true},
		{types.StringValue("line\rbreak"), true},
		{types.StringValue("nonascii-é"), true},
		{types.StringUnknown(), false},
	} {
		var resp validator.StringResponse
		envContent{}.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("content"), ConfigValue: tt.value}, &resp)
		if resp.Diagnostics.HasError() != tt.bad {
			t.Fatal("content validation mismatch")
		}
	}
}

type fakeAPI struct {
	client.API
	pages     []client.Page[client.Env]
	offsets   []int
	env       client.Env
	revealErr error
}

func (f *fakeAPI) ListUserData(_ context.Context, _ string, q client.UserDataQuery) (client.Page[client.Env], error) {
	f.offsets = append(f.offsets, q.Offset)
	p := f.pages[0]
	f.pages = f.pages[1:]
	return p, nil
}
func (f *fakeAPI) GetUserData(context.Context, string) (client.Env, error) { return f.env, nil }
func (f *fakeAPI) RevealUserData(context.Context, string) (string, error)  { return "", f.revealErr }
func TestLookupOwnershipAndPagination(t *testing.T) {
	rows := make([]client.Env, 20)
	for i := range rows {
		rows[i] = client.Env{ID: fmt.Sprint(i), Key: "PATH", Type: "SYSTEM", Editable: true}
	}
	api := &fakeAPI{pages: []client.Page[client.Env]{{Items: rows, Total: 21}, {Items: []client.Env{{ID: "user", Key: "path", Type: "USER", Editable: true}}, Total: 21}}}
	e, err := lookup(context.Background(), api, "s", "PATH")
	if err != nil || e.ID != "user" || len(api.offsets) != 2 || api.offsets[1] != 20 {
		t.Fatalf("lookup = %+v, %v; offsets=%v", e, err, api.offsets)
	}
	if owned(client.Env{Type: "SYSTEM", Editable: true}) || owned(client.Env{Type: "USER", Editable: true, Internal: true}) {
		t.Fatal("adopted foreign row")
	}
}
func TestMaskedRefreshPreservesKnownValue(t *testing.T) {
	api := &fakeAPI{env: client.Env{ID: "u", ServiceStackID: "s", Key: "KEY", Type: "USER", Editable: true, Sensitive: true, Content: "REDACTED"}, revealErr: &client.APIError{HTTPStatus: 403, Code: "notInSudoMode"}}
	r := Resource{data: &providerdata.ProviderData{API: api}}
	m := model{ID: types.StringValue("u"), Content: types.StringValue("known-secret")}
	var d diag.Diagnostics
	_, err := r.refresh(context.Background(), &m, &d)
	if err != nil || m.Content.ValueString() != "known-secret" || len(d) != 1 {
		t.Fatalf("masked refresh: %v, %v", err, d)
	}
	m.Content = types.StringNull()
	if _, err = r.refresh(context.Background(), &m, &d); err == nil {
		t.Fatal("allowed unreadable sensitive import")
	}
	for _, code := range []string{"insufficientPermissions", "forbidden"} {
		api.revealErr = &client.APIError{HTTPStatus: 403, Code: code}
		m.Content = types.StringValue("known-secret")
		d = nil
		if _, err := r.refresh(context.Background(), &m, &d); err == nil || len(d) != 0 {
			t.Fatalf("unexpected fallback for %s: %v, %v", code, err, d)
		}
	}
}
func fixture(t *testing.T) string {
	t.Helper()
	p := acctest.CreateTestProject(t)
	api := acctest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	start := true
	s, err := api.CreateService(ctx, p.ID, acctest.MinimalServiceInput(client.ServiceCreate{Name: acctest.RandomHostname(), ServiceStackVersionName: "alpine/nodejs@22", StartWithoutCode: &start}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Process != nil {
		if _, err = waiter.WaitProcess(ctx, api, s.Process.ID); err != nil {
			t.Fatal(err)
		}
	}
	baseline, err := waiter.SnapshotProcessIDs(ctx, api, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = waiter.WaitServiceSettled(ctx, api, s.ID, []string{"ACTIVE"}, baseline, nil); err != nil {
		t.Fatal(err)
	}
	return s.ID
}
func TestAccServiceEnv(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	service := fixture(t)
	service2 := fixture(t)
	var id string
	config := func(s, key, content string, sensitive bool) string {
		return acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_service_env\" \"test\" {\nservice_id=%q\nkey=%q\ncontent=%q\nsensitive=%t\n}\n", s, key, content, sensitive)
	}
	checkID := func(s *terraform.State) error {
		newID := s.RootModule().Resources["zerops_service_env.test"].Primary.ID
		if id != "" && newID != id {
			return fmt.Errorf("rename changed id")
		}
		id = newID
		return nil
	}
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), Steps: []testingresource.TestStep{
		{Config: config(service, "TFACC_KEY", "one", false), Check: checkID},
		{ResourceName: "zerops_service_env.test", ImportState: true, ImportStateVerify: true},
		{ResourceName: "zerops_service_env.test", ImportState: true, ImportStateId: service + "/TFACC_KEY", ImportStateVerify: true},
		{Config: config(service, "TFACC_RENAMED", "two", true), Check: testingresource.ComposeTestCheckFunc(checkID, testingresource.TestCheckResourceAttr("zerops_service_env.test", "content", "two"))},
		{Config: config(service, "TFACC_RENAMED", "three", false), Check: checkID},
		{Config: config(service2, "TFACC_RENAMED", "three", false), PreConfig: func() { id = "" }},
		{Config: config(service2, "TFACC_RENAMED", "three", false), PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			api := acctest.Client(t)
			e, err := lookup(ctx, api, service2, "TFACC_RENAMED")
			if err != nil {
				t.Fatal(err)
			}
			p, err := api.DeleteUserData(ctx, e.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = waiter.WaitProcess(ctx, api, p.ID); err != nil {
				t.Fatal(err)
			}
		}, PlanOnly: true, ExpectNonEmptyPlan: true},
	}})
}
func TestAccServiceEnvNonSudoMaskedRefresh(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	token := os.Getenv("ZEROPS_ACC_NONSUDO_TOKEN")
	if token == "" {
		t.Skip("ZEROPS_ACC_NONSUDO_TOKEN unavailable")
	}
	service := fixture(t)
	config := acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_service_env\" \"test\" {\nservice_id=%q\nkey=\"TFACC_MASKED\"\ncontent=\"known-value\"\nsensitive=true\n}\n", service)
	original := os.Getenv("ZEROPS_TOKEN")
	t.Cleanup(func() { _ = os.Setenv("ZEROPS_TOKEN", original) })
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), Steps: []testingresource.TestStep{{Config: config}, {Config: config, PreConfig: func() { _ = os.Setenv("ZEROPS_TOKEN", token) }, PlanOnly: true, Check: testingresource.TestCheckResourceAttr("zerops_service_env.test", "content", "known-value")}}})
}
