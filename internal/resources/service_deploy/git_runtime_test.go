package service_deploy

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	"testing"
)

func TestRuntimeGitToken(t *testing.T) {
	t.Setenv("ZEROPS_GIT_TOKEN", "runtime-secret")
	for _, tt := range []struct {
		name, mode string
		explicit   types.String
		want       string
	}{
		{"null", "provider", types.StringNull(), "runtime-secret"}, {"explicit", "provider", types.StringValue("explicit-secret"), "explicit-secret"}, {"empty", "provider", types.StringValue(""), ""}, {"unknown", "provider", types.StringUnknown(), ""}, {"server", "server", types.StringNull(), ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeGitToken(tt.mode, tt.explicit); got != tt.want {
				t.Fatal("wrong credential selection")
			}
		})
	}
	t.Setenv("ZEROPS_GIT_TOKEN", "apply-secret")
	if runtimeGitToken("provider", types.StringNull()) != "apply-secret" {
		t.Fatal("credential was cached across plan and apply")
	}
	t.Setenv("ZEROPS_GIT_TOKEN", "")
	if runtimeGitToken("provider", types.StringNull()) != "" {
		t.Fatal("empty runtime token must be anonymous")
	}
}
