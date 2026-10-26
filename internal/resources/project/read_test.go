package project

import (
	"context"
	"errors"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/providerdata"
	"testing"
)

type readAPI struct {
	client.API
	project client.Project
	err     error
}

func (a readAPI) GetProject(context.Context, string) (client.Project, error)   { return a.project, a.err }
func (a readAPI) ListProjectEnv(context.Context, string) ([]client.Env, error) { return nil, nil }
func TestReadDeletingAndErrors(t *testing.T) {
	for _, tc := range []struct {
		status string
		err    error
		absent bool
	}{{"DELETING", nil, true}, {"ACTIVE", nil, false}, {"", &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}, true}, {"", &client.APIError{HTTPStatus: 404, Code: "notFound"}, false}, {"", errors.New("transport failure"), false}} {
		r := Resource{data: &providerdata.ProviderData{API: readAPI{project: client.Project{ID: "p", Status: tc.status}, err: tc.err}}}
		m := Model{ID: types.StringValue("p"), MaxCreditLimit: types.StringValue("12")}
		e := r.read(context.Background(), &m)
		if client.IsNotFound(e, "projectNotFound") != tc.absent {
			t.Fatalf("status %s error %v", tc.status, e)
		}
		if m.MaxCreditLimit.ValueString() != "12" {
			t.Fatal("read changed credit")
		}
	}
}
