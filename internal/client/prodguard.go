package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const ProductionEndpoint = "https://api.app-prg1.zerops.io"
const productionHost = "api.app-prg1.zerops.io"

type Target string

const (
	TargetDev  Target = "dev"
	TargetProd Target = "prod"
)

func ParseTarget(value string) (Target, error) {
	switch value {
	case "", "dev":
		return TargetDev, nil
	case "prod":
		return TargetProd, nil
	default:
		return "", errors.New("ZEROPS_TARGET must be empty, dev, or prod")
	}
}
func ValidateTargetEndpoint(target Target, endpoint string) error {
	if endpoint == "" {
		return errors.New("ZEROPS_ENDPOINT is required")
	}
	if target == TargetDev && os.Getenv("ZEROPS_DEV_DOMAIN") == "" {
		return errors.New("ZEROPS_DEV_DOMAIN is required")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u == nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "") {
		return errors.New("target requires an HTTPS origin")
	}
	switch target {
	case TargetDev:
		host := strings.ToLower(u.Hostname())
		if insideDevDomain(host, os.Getenv("ZEROPS_DEV_DOMAIN")) && host != productionHost {
			return nil
		}
	case TargetProd:
		if endpoint == ProductionEndpoint {
			return nil
		}
	}
	return errors.New("endpoint does not match target")
}

// Only the public production upload host is allowed; all other hosts fail closed.
var productionUploadHosts = [...]string{"proxy.app-prg1.zerops.io"}

func ValidateUploadURL(target Target, value string) error {
	u, err := url.Parse(value)
	if err != nil || u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return errors.New("upload requires HTTPS without credentials")
	}
	if target == TargetDev {
		host := strings.ToLower(u.Hostname())
		if insideDevDomain(host, os.Getenv("ZEROPS_DEV_DOMAIN")) && host != productionHost {
			return nil
		}
	}
	if target == TargetProd {
		for _, host := range productionUploadHosts {
			if u.Host == host {
				return nil
			}
		}
	}
	return errors.New("upload host refused: " + u.Hostname())
}

type Ledger struct {
	Projects       map[string]string
	Tokens         map[string]string
	IPv4Projects   map[string]string
	IPv4           bool
	unreleasedIPv4 map[string]bool
}

func readLedger(f *os.File) (Ledger, error) {
	out := Ledger{Projects: make(map[string]string), Tokens: make(map[string]string), IPv4Projects: make(map[string]string)}
	activeIPv4 := make(map[string]bool)
	out.unreleasedIPv4 = activeIPv4
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return out, errors.New("cannot read production ledger")
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return out, errors.New("cannot read production ledger")
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			return out, errors.New("invalid production ledger record")
		}
		if _, err := time.Parse(time.RFC3339, fields[0]); err != nil {
			return out, errors.New("invalid production ledger timestamp")
		}
		switch fields[1] {
		case "project":
			out.Projects[fields[2]] = fields[3]
		case "ipv4":
			activeIPv4[fields[2]] = true
			out.IPv4Projects[fields[2]] = fields[3]
		case "note":
			// Reservations survive interrupted or indeterminate acquisitions.
			switch fields[3] {
			case "ipv4-reserved":
				activeIPv4[fields[2]] = true
			case "ipv4-released":
				delete(activeIPv4, fields[2])
			}
		case "token":
			out.Tokens[fields[2]] = fields[3]
		case "dns", "repo":
			// External fixtures never confer project or token ownership.
		default:
			return out, errors.New("invalid production ledger kind")
		}
	}
	out.IPv4 = len(activeIPv4) != 0
	return out, nil
}
func ReadLedger(path string) (Ledger, error) {
	f, err := openLedger(path)
	if err != nil {
		return Ledger{}, err
	}
	defer f.Close()
	return readLedger(f)
}
func appendRecord(f *os.File, kind, id, value, source string) error {
	if id == "" || strings.ContainsAny(id+value, "\t\r\n") {
		return errors.New("invalid ledger record")
	}
	switch source {
	case "acctest", "smoke", "probe", "provider":
	default:
		return errors.New("invalid ledger source")
	}
	_, err := fmt.Fprintf(f, "%s\t%s\t%s\t%s\t%s\n", time.Now().UTC().Format(time.RFC3339), kind, id, value, source)
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		return errors.New("cannot append production ledger")
	}
	return nil
}
func RecordProject(path, id, name, source string) error {
	if !guardPrefix(name) {
		return errors.New("project name requires tfacc- or tfprobe- prefix")
	}
	f, err := openLedger(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return appendRecord(f, "project", id, name, source)
}

// RecordIPv4 can fill in the address after the acquisition recorded an empty one.
func RecordIPv4(path, id, address, source string) error {
	f, err := openLedger(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return appendRecord(f, "ipv4", id, address, source)
}
func guardPrefix(name string) bool {
	return strings.HasPrefix(name, "tfacc-") || strings.HasPrefix(name, "tfprobe-")
}

// ProdTransport is fail-closed even before bootstrap pins OrgID. Use the
// constructor to resolve the organization rather than trusting environment IDs.
type ProdTransport struct {
	Base                      http.RoundTripper
	OrgID, LedgerPath, Source string
	token                     string
	owned                     sync.Map
	objects                   sync.Map
}

func NewProdTransport(ctx context.Context, cfg Config, requestedOrg, ledgerPath, source string) (*ProdTransport, error) {
	if err := ValidateTargetEndpoint(TargetProd, cfg.Endpoint); err != nil {
		return nil, err
	}
	if strings.TrimSpace(requestedOrg) == "" {
		return nil, errors.New("ZEROPS_ORGANIZATION_ID is required for production")
	}
	if _, err := ReadLedger(ledgerPath); err != nil {
		return nil, err
	}
	g := &ProdTransport{Base: cfg.Transport, LedgerPath: ledgerPath, Source: source, token: cfg.Token}
	api, err := New(Config{Endpoint: cfg.Endpoint, Token: cfg.Token, Transport: g})
	if err != nil {
		return nil, err
	}
	orgs, err := api.ListOrganizations(ctx)
	if err != nil {
		return nil, errors.New("cannot resolve production organization")
	}
	// Verify the explicitly pinned organization against token memberships.
	present := false
	for _, org := range orgs {
		if org.ID == requestedOrg {
			present = true
		}
	}
	if !present {
		return nil, errors.New("production token has no membership in the pinned organization")
	}
	g.OrgID = requestedOrg
	return g, nil
}

func (g *ProdTransport) base() http.RoundTripper {
	if g.Base != nil {
		return g.Base
	}
	return http.DefaultTransport
}
func guardPath(u *url.URL) ([]string, error) {
	p := u.Path
	if strings.Contains(p, "//") || strings.Contains(u.EscapedPath(), "%") || strings.Contains(p, "\\") {
		return nil, errors.New("production refused ambiguous path")
	}
	// Strip only the complete API prefix, never lookalikes such as
	// /api/rest/publicclient/... that a backend could route differently.
	if strings.HasPrefix(p, "/api/rest/public/") {
		p = strings.TrimPrefix(p, "/api/rest/public")
	}
	if !strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return nil, errors.New("production refused noncanonical path")
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for _, v := range parts {
		if v == "." || v == ".." || v == "" {
			return nil, errors.New("production refused ambiguous path")
		}
	}
	return parts, nil
}

type ownershipHTTPError struct {
	status int
	code   string
}

func (e *ownershipHTTPError) Error() string { return "production ownership lookup failed" }

// RecordIPv4Release verifies release while holding the same ledger lock used
// for acquisition. Historical IPv4Projects entries remain available to sweepers.
func (g *ProdTransport) RecordIPv4Release(ctx context.Context, id string) error {
	if strings.TrimSpace(g.OrgID) == "" {
		return errors.New("production organization is not pinned")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ProductionEndpoint+"/api/rest/public/project/"+id, nil)
	if err != nil {
		return errors.New("invalid production project id")
	}
	parts, err := guardPath(req.URL)
	if err != nil || len(parts) != 2 || parts[0] != "project" || parts[1] != id || req.URL.RawQuery != "" || req.URL.Fragment != "" {
		return errors.New("invalid production project id")
	}
	if err := g.Check(ctx, req); err != nil {
		return err
	}
	f, err := openLedger(g.LedgerPath)
	if err != nil {
		return err
	}
	defer f.Close()
	ledger, err := readLedger(f)
	if err != nil {
		return err
	}
	if !ledger.unreleasedIPv4[id] {
		return errors.New("project has no unreleased production IPv4 acquisition")
	}
	var p Project
	if err := g.get(ctx, "/project/"+id, &p); err != nil {
		var statusErr *ownershipHTTPError
		if !errors.As(err, &statusErr) || statusErr.status != http.StatusBadRequest || statusErr.code != "projectNotFound" {
			return err
		}
	} else if p.ClientID != g.OrgID || (p.PublicIPv4 != nil && *p.PublicIPv4 != "") {
		return errors.New("production IPv4 release not proven")
	}
	source := g.Source
	if source == "" {
		source = "provider"
	}
	return appendRecord(f, "note", id, "ipv4-released", source)
}

func (g *ProdTransport) get(ctx context.Context, path string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ProductionEndpoint+"/api/rest/public"+path, nil)
	req.Header.Set("Authorization", "Bearer "+g.token)
	resp, err := g.base().RoundTrip(req)
	if err != nil {
		return errors.New("production ownership lookup failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var wire struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&wire)
		return &ownershipHTTPError{status: resp.StatusCode, code: wire.Error.Code}
	}
	if json.NewDecoder(resp.Body).Decode(out) != nil {
		return errors.New("invalid production ownership response")
	}
	return nil
}
func (g *ProdTransport) own(ctx context.Context, id string) error {
	if _, ok := g.owned.Load(id); ok {
		return nil
	}
	ledger, err := ReadLedger(g.LedgerPath)
	if err != nil {
		return err
	}
	if _, ok := ledger.Projects[id]; !ok {
		return errors.New("project is not recorded as created in production ledger")
	}
	var p struct{ ClientID, Name string }
	if err := g.get(ctx, "/project/"+id, &p); err != nil {
		return err
	}
	if p.ClientID != g.OrgID || !guardPrefix(p.Name) {
		return errors.New("production project ownership refused")
	}
	g.owned.Store(id, true)
	return nil
}

// Each entry grants mutations only after an object lookup proves project ownership.
var productionObjectRoutes = map[string]bool{"service-stack": true, "user-data": true, "project-env": true, "public-http-routing": true, "public-port-routing": true, "app-version": true}

func (g *ProdTransport) objectOwner(ctx context.Context, kind, id string) error {
	key := kind + "/" + id
	if v, ok := g.objects.Load(key); ok {
		return g.own(ctx, v.(string))
	}
	var obj struct {
		ProjectID      string `json:"projectId"`
		ServiceStackID string `json:"serviceStackId"`
	}
	if err := g.get(ctx, "/"+key, &obj); err != nil {
		return err
	}
	if obj.ProjectID == "" && obj.ServiceStackID != "" {
		return g.objectOwner(ctx, "service-stack", obj.ServiceStackID)
	}
	if obj.ProjectID == "" {
		return errors.New("production object has no project ownership")
	}
	if err := g.own(ctx, obj.ProjectID); err != nil {
		return err
	}
	g.objects.Store(key, obj.ProjectID)
	return nil
}
func projectName(req *http.Request, importing bool) (string, error) {
	if req.Body == nil {
		return "", errors.New("project creation requires a body")
	}
	data, err := io.ReadAll(req.Body)
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("cannot inspect project creation body")
	}
	var body struct {
		Name string `json:"name"`
		YAML string `json:"yaml"`
	}
	if json.Unmarshal(data, &body) != nil {
		return "", errors.New("invalid project creation JSON")
	}
	if importing {
		var doc struct {
			Project struct {
				Name string `yaml:"name"`
			} `yaml:"project"`
			Services []struct {
				Hostname string `yaml:"hostname"`
			} `yaml:"services"`
		}
		if yaml.Unmarshal([]byte(body.YAML), &doc) != nil {
			return "", errors.New("invalid project import YAML")
		}
		if err := checkImportHostnames(len(doc.Services), func(i int) string { return doc.Services[i].Hostname }); err != nil {
			return "", err
		}
		body.Name = doc.Project.Name
	}
	if !guardPrefix(body.Name) {
		return "", errors.New("project name requires tfacc- or tfprobe- prefix")
	}
	return body.Name, nil
}

// serviceImportBody guards POST /project/{P}/service-stack/import: no project
// section and no reserved/system hostname (importing `core` reuses and can
// poison the project's system core stack, leaving an undeletable project).
func serviceImportBody(req *http.Request) error {
	if req.Body == nil {
		return errors.New("service import requires a body")
	}
	data, err := io.ReadAll(req.Body)
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(data))
	if err != nil {
		return errors.New("cannot inspect service import body")
	}
	var body struct {
		YAML string `json:"yaml"`
	}
	if json.Unmarshal(data, &body) != nil {
		return errors.New("invalid service import JSON")
	}
	var doc map[string]any
	if yaml.Unmarshal([]byte(body.YAML), &doc) != nil {
		return errors.New("invalid service import YAML")
	}
	if _, ok := doc["project"]; ok {
		return errors.New("service import must not contain a project section")
	}
	var services struct {
		Services []struct {
			Hostname string `yaml:"hostname"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal([]byte(body.YAML), &services) != nil {
		return errors.New("invalid service import YAML")
	}
	return checkImportHostnames(len(services.Services), func(i int) string { return services.Services[i].Hostname })
}

func checkImportHostnames(n int, hostname func(int) string) error {
	for i := range n {
		if reservedHostname(hostname(i)) {
			return errors.New("production import refused empty or reserved service hostname")
		}
	}
	return nil
}

func reservedHostname(name string) bool {
	h := strings.ToLower(strings.TrimSpace(name))
	return h == "" || h == "core"
}

// serviceCreateBody refuses POST /project/{P}/service-stack[/object_storage_v1]
// with the reserved `core` name (it reuses the system core stack).
func serviceCreateBody(req *http.Request) error {
	if req.Body == nil {
		return errors.New("service creation requires a body")
	}
	data, err := io.ReadAll(req.Body)
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(data))
	if err != nil {
		return errors.New("cannot inspect service creation body")
	}
	var body struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &body) != nil {
		return errors.New("invalid service creation JSON")
	}
	if reservedHostname(body.Name) {
		return errors.New("production refused empty or reserved service hostname")
	}
	return nil
}

// Check is also used by the shell helper; RoundTrip remains the enforcement
// point so shell execution cannot bypass response recording or the IPv4 lock.
func (g *ProdTransport) Check(ctx context.Context, req *http.Request) error {
	if req == nil || req.URL == nil || req.URL.Scheme != "https" || req.URL.User != nil {
		return errors.New("production refused non-HTTPS request")
	}
	if req.URL.Host != productionHost {
		if req.Method == http.MethodPut && req.Header.Get("Authorization") == "" {
			return ValidateUploadURL(TargetProd, req.URL.String())
		}
		return errors.New("production refused host: " + req.URL.Hostname())
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead && (req.URL.RawQuery != "" || req.URL.ForceQuery) {
		return errors.New("production refused query on mutation")
	}
	parts, err := guardPath(req.URL)
	if err != nil {
		return err
	}
	if g.OrgID == "" {
		if req.Method == http.MethodGet && strings.Join(parts, "/") == "user/client-list" {
			return nil
		}
		return errors.New("production organization is not pinned")
	}
	for i, p := range parts {
		if p == "client" && i+1 < len(parts) && parts[i+1] != g.OrgID {
			return errors.New("production refused foreign organization")
		}
	}
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		return nil
	}
	if len(parts) >= 3 && parts[0] == "client" && parts[2] == "integration-token" {
		return g.checkToken(ctx, req, parts)
	}
	if len(parts) >= 3 && parts[0] == "client" && parts[2] == "project" && req.Method == http.MethodPost && (len(parts) == 3 || (len(parts) == 4 && parts[3] == "import")) {
		_, err := projectName(req, len(parts) == 4)
		return err
	}
	if !productionMutationRoute(req.Method, parts) {
		return errors.New("production mutation route is not allowlisted")
	}
	if len(parts) >= 2 && parts[0] == "project" {
		if err := g.own(ctx, parts[1]); err != nil {
			var statusErr *ownershipHTTPError
			if req.Method != http.MethodDelete || len(parts) != 2 || !errors.As(err, &statusErr) || statusErr.status < 500 || statusErr.status > 599 {
				return err
			}
			ledger, ledgerErr := ReadLedger(g.LedgerPath)
			if ledgerErr != nil {
				return ledgerErr
			}
			if !guardPrefix(ledger.Projects[parts[1]]) {
				return err
			}
			fmt.Fprintf(os.Stderr, "prodguard: ledger-proven ownership fallback used for DELETE /project/%s after preflight HTTP %d\n", parts[1], statusErr.status)
		}
		if req.Method == http.MethodPost && len(parts) == 4 && parts[2] == "service-stack" && parts[3] == "import" {
			if err := serviceImportBody(req); err != nil {
				return err
			}
		}
		if req.Method == http.MethodPost && len(parts) >= 3 && parts[2] == "service-stack" && (len(parts) == 3 || parts[3] == "object_storage_v1") {
			if err := serviceCreateBody(req); err != nil {
				return err
			}
		}
		if req.Method == http.MethodPut && len(parts) == 3 && parts[2] == "request-ipv4" {
			ledger, err := ReadLedger(g.LedgerPath)
			if err != nil {
				return err
			}
			if ledger.IPv4 {
				return errors.New("production dedicated IPv4 was already requested")
			}
		}
		return nil
	}
	if len(parts) >= 2 && productionObjectRoutes[parts[0]] {
		return g.objectOwner(ctx, parts[0], parts[1])
	}
	return errors.New("production mutation route is not allowlisted")
}
func responseBytes(resp *http.Response) ([]byte, error) {
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("cannot inspect production mutation response")
	}
	return data, nil
}
func (g *ProdTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("production refused missing request")
	}
	if err := g.Check(req.Context(), req); err != nil {
		return nil, err
	}
	parts, _ := guardPath(req.URL)
	create := req.Method == http.MethodPost && len(parts) >= 3 && parts[0] == "client" && parts[2] == "project"
	tokenCreate := req.Method == http.MethodPost && len(parts) == 3 && parts[0] == "client" && parts[2] == "integration-token"
	ipv4 := req.Method == http.MethodPut && len(parts) == 3 && parts[0] == "project" && parts[2] == "request-ipv4"
	var lock *os.File
	source := g.Source
	if source == "" {
		source = "provider"
	}
	if ipv4 {
		var err error
		lock, err = openLedger(g.LedgerPath)
		if err != nil {
			return nil, err
		}
		defer lock.Close()
		ledger, err := readLedger(lock)
		if err != nil {
			return nil, err
		}
		if ledger.IPv4 {
			return nil, errors.New("production dedicated IPv4 was already requested")
		}
		// Persist before sending: a crash or failed response must not permit a
		// second acquisition. The successful address is appended separately.
		if err := appendRecord(lock, "note", parts[1], "ipv4-reserved", source); err != nil {
			return nil, err
		}
	}
	name := ""
	if create {
		var err error
		name, err = projectName(req, len(parts) == 4)
		if err != nil {
			return nil, err
		}
	}
	if tokenCreate {
		var err error
		name, _, err = tokenMutationBody(req)
		if err != nil {
			return nil, err
		}
	}
	resp, err := g.base().RoundTrip(req)
	if err != nil {
		return nil, errors.New("production request failed")
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		return nil, errors.New("production redirects are disabled")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && (create || ipv4 || tokenCreate) {
		data, err := responseBytes(resp)
		if err != nil {
			return nil, err
		}
		if create {
			id, err := createdProjectID(data, len(parts) == 4)
			if err != nil {
				return nil, err
			}
			if err := RecordProject(g.LedgerPath, id, name, source); err != nil {
				return nil, &productionCleanupError{operation: "created", id: id}
			}
		} else if tokenCreate {
			var wire struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(data, &wire) != nil || wire.ID == "" {
				return nil, errors.New("created production token response has no token id; manual cleanup required")
			}
			f, err := openLedger(g.LedgerPath)
			if err != nil {
				return nil, errors.New("created production token but ledger recording failed; manual cleanup required")
			}
			err = appendRecord(f, "token", wire.ID, name, source)
			f.Close()
			if err != nil {
				return nil, errors.New("created production token but ledger recording failed; manual cleanup required")
			}
		} else {
			var wire struct {
				IPv4 string `json:"ipv4"`
			}
			_ = json.Unmarshal(data, &wire)
			if err := appendRecord(lock, "ipv4", parts[1], wire.IPv4, source); err != nil {
				return nil, &productionCleanupError{operation: "acquired IPv4 for", id: parts[1]}
			}
		}
	}
	return resp, nil
}

// A distinct type lets the client preserve actionable cleanup IDs without ever
// exposing raw RoundTripper errors, response bodies, tokens, or signed URLs.
type productionCleanupError struct {
	operation string
	id        string
}

func (e *productionCleanupError) Error() string {
	id := e.id
	if len(id) > 128 {
		id = "unknown"
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			id = "unknown"
			break
		}
	}
	return fmt.Sprintf("%s project %s but ledger recording failed; manual cleanup required", e.operation, id)
}

func createdProjectID(data []byte, importing bool) (string, error) {
	var wire struct {
		ID        string `json:"id"`
		ProjectID string `json:"projectId"`
		Project   struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if json.Unmarshal(data, &wire) != nil {
		return "", errors.New("created production project response is invalid; manual cleanup required")
	}
	id := wire.ID
	if importing {
		id = wire.Project.ID
		if id == "" {
			id = wire.ProjectID
		}
	}
	if id == "" {
		return "", errors.New("created production project response has no project id; manual cleanup required")
	}
	return id, nil
}

// CreatedProject shares create/import response parsing with shell tooling.
// req must contain the original creation body, not a consumed network stream.
func CreatedProject(req *http.Request, response []byte) (id, name string, err error) {
	if req == nil || req.URL == nil {
		return "", "", errors.New("missing project creation request")
	}
	if req.URL.RawQuery != "" || req.URL.ForceQuery {
		return "", "", errors.New("production refused query on mutation")
	}
	parts, err := guardPath(req.URL)
	if err != nil {
		return "", "", err
	}
	if req.Method != http.MethodPost || len(parts) < 3 || parts[0] != "client" || parts[2] != "project" || (len(parts) != 3 && !(len(parts) == 4 && parts[3] == "import")) {
		return "", "", errors.New("not a project creation request")
	}
	importing := len(parts) == 4
	name, err = projectName(req, importing)
	if err != nil {
		return "", "", err
	}
	id, err = createdProjectID(response, importing)
	return id, name, err
}
