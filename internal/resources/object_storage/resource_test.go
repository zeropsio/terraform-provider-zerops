package object_storage

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	testingresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
	"os"
	"regexp"
	"testing"
	"time"
)

type storageAPI struct{ client.API }

func (*storageAPI) GetService(context.Context, string) (client.Service, error) {
	cdn := false
	return client.Service{ID: "s", ProjectID: "p", Name: "bucket", Base: "object-storage", CDNEnabled: &cdn}, nil
}
func (*storageAPI) GetObjectStorage(context.Context, string) (client.StorageInfo, error) {
	return client.StorageInfo{DiskGBytesUsed: 0, RawPolicy: `{"websiteAccess":false,"indexDocument":null,"errorDocument":null}`}, nil
}
func (*storageAPI) ListServiceEnv(context.Context, string) ([]client.Env, error) {
	return []client.Env{{Key: "quotaGBytes", Content: "20", Type: "SYSTEM"}, {Key: "quotaGBytes", Content: "999", Type: "USER"}, {ID: "secret", Key: "secretAccessKey", Content: "REDACTED", Type: "SYSTEM", Sensitive: true}}, nil
}
func (*storageAPI) RevealUserData(context.Context, string) (string, error) {
	return "", &client.APIError{HTTPStatus: 403, Code: "notInSudoMode"}
}
func TestStorageMappingAndMismatch(t *testing.T) {
	r := Resource{data: &providerdata.ProviderData{API: &storageAPI{}}}
	m := model{ID: types.StringValue("s"), Policy: types.StringNull(), Raw: jsontypes.NewNormalizedNull()}
	var d diag.Diagnostics
	exists, err := r.refresh(context.Background(), &m, &d, false)
	if err != nil || !exists || m.Quota.ValueInt64() != 20 || !m.Secret.IsNull() || m.Policy.ValueString() != "private" {
		t.Fatalf("storage mapping failed: %+v, %v", m, err)
	}
	m.Policy = types.StringValue("custom")
	m.Raw = jsontypes.NewNormalizedValue(`{"Version":"2012-10-17","Statement":[]}`)
	if _, err = r.refresh(context.Background(), &m, &d, true); err == nil {
		t.Fatal("discarded custom fields were accepted")
	}
}

type policyStorageAPI struct {
	storageAPI
	raw string
}

func (a *policyStorageAPI) GetObjectStorage(context.Context, string) (client.StorageInfo, error) {
	return client.StorageInfo{RawPolicy: a.raw}, nil
}

func TestStoragePolicyImport(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		private bool
	}{
		{"native private", `{"websiteAccess":false,"indexDocument":null,"errorDocument":null}`, true},
		{"native omitted documents", `{"websiteAccess":false}`, true},
		{"AWS private", `{ "Statement": [], "Version": "2012-10-17" }`, true},
		{"native public", `{"websiteAccess":true,"indexDocument":"index.html","errorDocument":null}`, false},
		{"native custom document", `{"websiteAccess":false,"indexDocument":"custom.html"}`, false},
		{"native unknown field", `{"websiteAccess":false,"unknown":null}`, false},
		{"AWS custom metadata", discardedAWSMetadataPolicy, false},
		{"AWS nonempty statements", `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":"s3:GetObject","Resource":"*","Principal":"*"}]}`, false},
		{"AWS unknown field", `{"Version":"2012-10-17","Statement":[],"unknown":null}`, false},
		{"AWS null statements", `{"Version":"2012-10-17","Statement":null}`, false},
		{"AWS other version", `{"Version":"2008-10-17","Statement":[]}`, false},
		{"unknown object", `{}`, false},
		{"unknown projection", `{"unknown":true}`, false},
		{"null projection", `null`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, policy := range []types.String{types.StringNull(), types.StringUnknown()} {
				r := Resource{data: &providerdata.ProviderData{API: &policyStorageAPI{raw: tc.raw}}}
				m := model{ID: types.StringValue("s"), Policy: policy, Raw: jsontypes.NewNormalizedNull()}
				var d diag.Diagnostics
				exists, err := r.refresh(context.Background(), &m, &d, false)
				if err != nil || !exists || d.HasError() {
					t.Fatalf("refresh failed: exists=%t err=%v diagnostics=%v", exists, err, d)
				}
				if tc.private {
					if m.Policy.ValueString() != "private" || !m.Raw.IsNull() || len(d) != 0 {
						t.Fatalf("canonical private projection not recognized: policy=%v raw=%v diagnostics=%v", m.Policy, m.Raw, d)
					}
				} else if m.Policy.ValueString() != "custom" || !semanticEqual(m.Raw.ValueString(), tc.raw) || len(d) != 1 {
					t.Fatalf("custom projection lost: policy=%v raw=%v diagnostics=%v", m.Policy, m.Raw, d)
				}
			}
		})
	}
}

func TestStorageExplicitCustomPolicy(t *testing.T) {
	for _, raw := range []string{
		`{"websiteAccess":false,"indexDocument":null,"errorDocument":null}`,
		`{"Version":"2012-10-17","Statement":[]}`,
		discardedAWSMetadataPolicy,
	} {
		r := Resource{data: &providerdata.ProviderData{API: &policyStorageAPI{raw: raw}}}
		m := model{ID: types.StringValue("s"), Quota: types.Int64Value(20), Policy: types.StringValue("custom"), Raw: jsontypes.NewNormalizedValue(raw)}
		var d diag.Diagnostics
		if _, err := r.refresh(context.Background(), &m, &d, true); err != nil || d.HasError() || m.Policy.ValueString() != "custom" || !semanticEqual(m.Raw.ValueString(), raw) {
			t.Fatalf("explicit custom intent lost: policy=%v raw=%v err=%v diagnostics=%v", m.Policy, m.Raw, err, d)
		}
		m.Raw = jsontypes.NewNormalizedValue(`{"different":true}`)
		if _, err := r.refresh(context.Background(), &m, &d, true); err == nil || !regexp.MustCompile("custom policy mismatch").MatchString(err.Error()) {
			t.Fatalf("custom policy verification bypassed: %v", err)
		}
	}
}

// Production preserves this empty AWS projection. Explicit custom intent is
// retained on refresh, but import can only identify the canonical private JSON.
const productionCustomPolicy = `{"Version":"2012-10-17","Statement":[]}`

// Production acceptance observed this Id discarded on both create and update.
// Keep it as unsupported input evidence, not as a successful persistence fixture.
const discardedAWSMetadataPolicy = `{"Version":"2012-10-17","Id":"tfacc-custom-empty","Statement":[]}`

func checkCustomPolicy(raw string) testingresource.TestCheckFunc {
	return func(state *terraform.State) error {
		row, ok := state.RootModule().Resources["zerops_object_storage.test"]
		if !ok || row.Primary == nil {
			return fmt.Errorf("object-storage state missing")
		}
		if row.Primary.Attributes["policy"] != "custom" || !semanticEqual(row.Primary.Attributes["raw_policy"], raw) {
			return fmt.Errorf("custom policy did not round-trip semantically")
		}
		return nil
	}
}

func TestPolicySemantics(t *testing.T) {
	for _, tt := range []struct {
		policy types.String
		raw    jsontypes.Normalized
		bad    bool
	}{{types.StringNull(), jsontypes.NewNormalizedValue(`{}`), false}, {types.StringValue("private"), jsontypes.NewNormalizedValue(`{}`), true}, {types.StringValue("custom"), jsontypes.NewNormalizedNull(), true}, {types.StringValue("custom"), jsontypes.NewNormalizedValue(`{}`), false}, {types.StringUnknown(), jsontypes.NewNormalizedNull(), false}} {
		if (policyError(tt.policy, tt.raw) != nil) != tt.bad {
			t.Fatalf("policy validation failed: %+v", tt)
		}
	}
	if !semanticEqual(`{"a":1,"b":true}`, `{ "b":true, "a":1 }`) || semanticEqual(`{"Version":"2012"}`, `{"websiteAccess":false}`) {
		t.Fatal("semantic comparison lost fields")
	}
}
func TestAccObjectStorage(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	host := acctest.RandomHostname()
	config := func(h string, q int, policy, raw string) string {
		if acctest.IsProd() {
			q = 1
		}
		extra := ""
		if policy != "" {
			extra += fmt.Sprintf("policy=%q\n", policy)
		}
		if raw != "" {
			extra += fmt.Sprintf("raw_policy=%q\n", raw)
		}
		return acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_object_storage\" \"test\" {\nproject_id=%q\nhostname=%q\nquota_gb=%d\n%s}\n", p.ID, h, q, extra)
	}
	base := config(host, 1, "private", "")
	custom := `{"websiteAccess":true,"indexDocument":"custom.html","errorDocument":"404.html"}`
	if acctest.IsProd() {
		custom = productionCustomPolicy
	}
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), Steps: []testingresource.TestStep{
		{Config: base, Check: testingresource.ComposeTestCheckFunc(testingresource.TestCheckResourceAttr("zerops_object_storage.test", "quota_gb", "1"), testingresource.TestCheckResourceAttrSet("zerops_object_storage.test", "bucket_name"), testingresource.TestCheckResourceAttrSet("zerops_object_storage.test", "api_url"), testingresource.TestCheckResourceAttrSet("zerops_object_storage.test", "access_key_id"))},
		{ResourceName: "zerops_object_storage.test", ImportState: true, ImportStateVerify: true},
		{Config: config(host, 20, "public-read", "")},
		{Config: config(host, 20, "", ""), Check: testingresource.TestCheckResourceAttr("zerops_object_storage.test", "policy", "private")},
		{Config: config(host, 5, "public-read-write", "")},
		{Config: config(host, 5, "public-objects-read", "")},
		{Config: config(host, 5, "public-write", "")},
		{Config: config(host, 5, "", custom), Check: checkCustomPolicy(custom)},
		{Config: config(host, 5, "", custom), PlanOnly: true},
		{Config: config(host, 5, "private", "")},
		{Config: config(host+"a", 5, "private", "")},
		{Config: config(host+"a", 5, "private", ""), PreConfig: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			api := acctest.Client(t)
			s, err := api.GetServiceByName(ctx, p.ID, host+"a")
			if err != nil {
				t.Fatal(err)
			}
			process, err := api.DeleteService(ctx, s.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = waiter.WaitProcess(ctx, api, process.ID); err != nil {
				t.Fatal(err)
			}
		}, PlanOnly: true, ExpectNonEmptyPlan: true},
	}})
}
func TestAccObjectStorageAWSMismatch(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC required")
	}
	acctest.PreCheck(t)
	p := acctest.CreateTestProject(t)
	raw := productionCustomPolicy
	config := acctest.ProviderConfig() + fmt.Sprintf("resource \"zerops_object_storage\" \"test\" {\nproject_id=%q\nhostname=%q\nquota_gb=1\npolicy=\"custom\"\nraw_policy=%q\n}\n", p.ID, acctest.RandomHostname(), raw)
	steps := []testingresource.TestStep{{Config: config, ExpectError: regexp.MustCompile("custom policy mismatch")}}
	if acctest.IsProd() {
		// Only the observed empty AWS JSON persistence is asserted, not arbitrary
		// AWS semantics. Its custom intent is ambiguous on import.
		steps = []testingresource.TestStep{
			{Config: config, Check: checkCustomPolicy(raw)},
			{Config: config, PlanOnly: true},
		}
	}
	testingresource.Test(t, testingresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(provider.Registry{Resources: []func() resource.Resource{NewResource}}), Steps: steps})
}
