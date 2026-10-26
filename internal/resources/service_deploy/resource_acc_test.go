package service_deploy

import (
	"fmt"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/zeropsio/terraform-provider-zerops/internal/acctest"
	"os"
	"strings"
	"testing"
)

func TestAccServiceDeployPrivateGit(t *testing.T) {
	if os.Getenv("TF_ACC") == "" || os.Getenv("ZEROPS_ACC_GIT_PRIVATE") != "1" {
		t.Skip("requires TF_ACC and ZEROPS_ACC_GIT_PRIVATE=1")
	}
	if os.Getenv("ZEROPS_GIT_TOKEN") == "" {
		t.Fatal("ZEROPS_GIT_TOKEN must be supplied at runtime")
	}
	url := os.Getenv("ZEROPS_ACC_PRIVATE_GIT_URL")
	ref := os.Getenv("ZEROPS_ACC_PRIVATE_GIT_REF")
	if acctest.IsProd() {
		if err := validateProductionPrivateGitFixture(url, ref, os.Getenv("ZEROPS_ACC_PROD_GIT_REPOSITORY")); err != nil {
			t.Fatal(err)
		}
	} else {
		if url == "" {
			t.Skip("set ZEROPS_ACC_PRIVATE_GIT_URL to a private fixture repository readable with ZEROPS_GIT_TOKEN")
		}
		if ref == "" {
			ref = "main"
		}
	}
	s := accService(t)
	yaml, err := os.ReadFile("testdata/app/zerops.yml")
	if err != nil {
		t.Fatal(err)
	}
	config := accConfig(s.ID, fmt.Sprintf("git = { url = %q, ref = %q, mode = \"provider\" }", url, ref), string(yaml), true)
	tfresource.Test(t, tfresource.TestCase{ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories(accFactories()), Steps: []tfresource.TestStep{{Config: config, Check: func(st *terraform.State) error {
		attrs := st.RootModule().Resources["zerops_service_deploy.app"].Primary.Attributes
		if !fullSHA.MatchString(attrs["git_commit"]) {
			return fmt.Errorf("deployment did not pin a commit")
		}
		for k, v := range attrs {
			if strings.Contains(v, os.Getenv("ZEROPS_GIT_TOKEN")) {
				return fmt.Errorf("runtime credential persisted")
			}
			if strings.Contains(k, "auth_token") && v != "" {
				return fmt.Errorf("auth_token unexpectedly populated")
			}
		}
		return nil
	}}, {Config: config, PlanOnly: true}}})
}

// pinned is the only private fixture repository (owner/name on GitHub) that
// production acceptance may deploy from, supplied via ZEROPS_ACC_PROD_GIT_REPOSITORY.
func validateProductionPrivateGitFixture(url, ref, pinned string) error {
	if pinned == "" || strings.Count(pinned, "/") != 1 || strings.TrimSpace(pinned) != pinned {
		return fmt.Errorf("production private Git acceptance requires ZEROPS_ACC_PROD_GIT_REPOSITORY=<owner>/<repository>")
	}
	repository := "https://github.com/" + pinned
	if url != repository && url != repository+".git" {
		return fmt.Errorf("production private Git acceptance requires explicit ZEROPS_ACC_PRIVATE_GIT_URL=%s (optional .git suffix)", repository)
	}
	branch := strings.TrimPrefix(ref, "refs/heads/")
	if branch == "" || branch == "main" || branch == "HEAD" || strings.TrimSpace(branch) != branch || strings.HasPrefix(branch, "refs/") || fullSHA.MatchString(branch) {
		return fmt.Errorf("production private Git acceptance requires explicit ZEROPS_ACC_PRIVATE_GIT_REF naming a non-main fixture branch")
	}
	return nil
}

func TestProductionPrivateGitFixture(t *testing.T) {
	const pinned = "zerops-test/fixture"
	const repository = "https://github.com/" + pinned
	for _, tc := range []struct {
		name string
		url  string
		ref  string
		ok   bool
		// unpinned leaves ZEROPS_ACC_PROD_GIT_REPOSITORY empty.
		unpinned bool
	}{
		{name: "authorized", url: repository, ref: "tfprobe/acceptance", ok: true},
		{name: "authorized dot git", url: repository + ".git", ref: "tfprobe/acceptance", ok: true},
		{name: "qualified branch", url: repository, ref: "refs/heads/tfprobe/acceptance", ok: true},
		{name: "missing repository", ref: "tfprobe/acceptance"},
		{name: "other repository", url: "https://github.com/zerops-test/other", ref: "tfprobe/acceptance"},
		{name: "repository suffix", url: repository + "/other", ref: "tfprobe/acceptance"},
		{name: "missing ref", url: repository},
		{name: "blank ref", url: repository, ref: " "},
		{name: "main", url: repository, ref: "main"},
		{name: "qualified main", url: repository, ref: "refs/heads/main"},
		{name: "padded main", url: repository, ref: " main "},
		{name: "default head", url: repository, ref: "HEAD"},
		{name: "tag", url: repository, ref: "refs/tags/fixture"},
		{name: "commit", url: repository, ref: strings.Repeat("a", 40)},
		{name: "no pinned repository", url: repository, ref: "tfprobe/acceptance", unpinned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pin := pinned
			if tc.unpinned {
				pin = ""
			}
			if err := validateProductionPrivateGitFixture(tc.url, tc.ref, pin); (err == nil) != tc.ok {
				t.Fatalf("fixture validation error = %v, want accepted = %t", err, tc.ok)
			}
		})
	}
}
