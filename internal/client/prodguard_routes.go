package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// productionMutationRoute preserves v1 mutations, but never grants arbitrary
// future endpoints merely because their parent belongs to a test project.
func productionMutationRoute(method string, parts []string) bool {
	if len(parts) < 2 {
		return false
	}
	suffix := strings.Join(parts[2:], "/")
	allowed := ""
	switch parts[0] {
	case "project":
		switch suffix {
		case "":
			allowed = "PUT DELETE"
		case "env", "service-stack", "service-stack/object_storage_v1", "service-stack/import", "public-http-routing":
			allowed = "POST"
		case "mode-upgrade", "public-ipv4-shared", "sync-public-http-routing", "sync-public-port-routing", "request-ipv4", "remote-logging", "l7httpbalancer-config", "core-stack/prometheus-config":
			allowed = "PUT"
		case "vpn":
			allowed = "POST DELETE"
		}
	case "service-stack":
		switch suffix {
		case "":
			allowed = "DELETE"
		case "user-data", "public-port-routing", "app-version":
			allowed = "POST"
		case "autoscaling", "start", "stop", "reload", "enable-subdomain-access", "disable-subdomain-access", "object-storage-size", "trigger-pipeline", "backup-config", "restart", "external-repository-integration":
			allowed = "PUT"
		case "cdn":
			allowed = "PUT DELETE"
		}
	case "project-env", "user-data", "public-port-routing", "public-http-routing":
		if suffix == "" {
			allowed = "PUT DELETE"
		} else if suffix == "restore" && (parts[0] == "public-port-routing" || parts[0] == "public-http-routing") {
			allowed = "PUT"
		} else if parts[0] == "public-http-routing" && (suffix == "location" || suffix == "trigger-domains-dns-check") {
			allowed = "PUT"
		}
	case "app-version":
		if suffix == "deploy" || suffix == "build-and-deploy" {
			allowed = "PUT"
		}
	}
	return allowed != "" && strings.Contains(" "+allowed+" ", " "+method+" ")
}

func tokenMutationBody(req *http.Request) (string, []string, error) {
	if req.Body == nil {
		return "", nil, errors.New("token mutation requires a body")
	}
	data, err := io.ReadAll(req.Body)
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(data))
	var body struct {
		Name              string `json:"name"`
		RoleCode          string `json:"roleCode"`
		CanCreateProjects bool   `json:"canCreateProjects"`
		CanViewFinances   bool   `json:"canViewFinances"`
		CanEditFinances   bool   `json:"canEditFinances"`
		Sudo              bool   `json:"sudo"`
		Projects          []struct {
			ProjectID string `json:"projectId"`
		} `json:"projects"`
	}
	if err != nil || json.Unmarshal(data, &body) != nil || !guardPrefix(body.Name) || body.Projects == nil {
		return "", nil, errors.New("invalid production token name or project scopes")
	}
	// Production test tokens may gain access only through owned project scopes,
	// never through organization-wide roles or administrative capabilities.
	if body.RoleCode != "NO_ACCESS" || body.CanCreateProjects || body.CanViewFinances || body.CanEditFinances || body.Sudo {
		return "", nil, errors.New("production token organization permissions refused")
	}
	ids := make([]string, len(body.Projects))
	for i, p := range body.Projects {
		if p.ProjectID == "" {
			return "", nil, errors.New("invalid production token project scope")
		}
		ids[i] = p.ProjectID
	}
	return body.Name, ids, nil
}

func (g *ProdTransport) checkToken(ctx context.Context, req *http.Request, parts []string) error {
	create := len(parts) == 3 && req.Method == http.MethodPost
	update := len(parts) == 4 && req.Method == http.MethodPut
	remove := len(parts) == 4 && req.Method == http.MethodDelete
	rotate := len(parts) == 5 && parts[4] == "regenerate" && req.Method == http.MethodPut
	if !create && !update && !remove && !rotate {
		return errors.New("production token mutation route is not allowlisted")
	}
	if !create {
		ledger, err := ReadLedger(g.LedgerPath)
		if err != nil {
			return err
		}
		if _, ok := ledger.Tokens[parts[3]]; !ok {
			return errors.New("token is not recorded in production ledger")
		}
		var ambient struct{ ID string }
		if err := g.get(ctx, "/user/info", &ambient); err != nil {
			return err
		}
		if ambient.ID == "" || ambient.ID == parts[3] {
			return errors.New("production refused ambient token mutation")
		}
		var metadata struct{ ID, Name string }
		if err := g.get(ctx, "/client/"+g.OrgID+"/integration-token/"+parts[3], &metadata); err != nil {
			return err
		}
		if metadata.ID != parts[3] || !guardPrefix(metadata.Name) {
			return errors.New("production token ownership refused")
		}
	}
	if create || update {
		_, projects, err := tokenMutationBody(req)
		if err != nil {
			return err
		}
		for _, id := range projects {
			if err := g.own(ctx, id); err != nil {
				return err
			}
		}
	}
	return nil
}
