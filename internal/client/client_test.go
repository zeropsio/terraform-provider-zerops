package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zerops-go/apiError"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, err := newTestClient(Config{Endpoint: s.URL, Token: "secret-token", UserAgent: "provider-test"})
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport.(*safeTransport).sleep = func(context.Context, time.Duration) error { return nil }
	return c
}
func respond(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
func TestErrorChannelsAndNotFound(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, 400, `{"error":{"code":"projectNotFound","message":"missing secret-token","meta":{"field":"name"}}}`)
	})
	_, err := c.GetProject(context.Background(), "project")
	a, ok := AsAPIError(err)
	if !ok || a.HTTPStatus != 400 || a.Code != "projectNotFound" || a.Message != "missing secret-token" {
		t.Fatalf("unexpected structured error: %v", err)
	}
	wrapped := fmt.Errorf("lookup: %w", err)
	if !IsNotFound(wrapped, "projectNotFound") || IsNotFound(wrapped, "NotFound") || IsNotFound(wrapped) {
		t.Fatal("not-found must require exact supplied code")
	}
	if IsNotFound(&APIError{HTTPStatus: 404, Code: "projectNotFound"}, "projectNotFound") {
		t.Fatal("HTTP classifier accepted 404")
	}
	converted := normalizeError(fmt.Errorf("wrapped: %w", apiError.Error{HttpStatusCode: 403, ErrorCode: "notInSudoMode"}))
	if e, ok := AsAPIError(converted); !ok || e.HTTPStatus != 403 {
		t.Fatal("SDK value error was not unwrapped")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatal("server message escaped through Error")
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) { respond(w, 400, "secret response body") })
	_, err = c.GetProject(context.Background(), "project")
	if err == nil || strings.Contains(err.Error(), "secret response body") {
		t.Fatal("non-JSON body escaped")
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) { respond(w, 200, "not-json-secret") })
	_, err = c.GetProject(context.Background(), "project")
	if err == nil || strings.Contains(err.Error(), "not-json-secret") {
		t.Fatal("outer decode channel was not sanitized")
	}
}
func TestGETRetries(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   int
	}{
		{"server", 500, `{"error":{"code":"failure"}}`, 3},
		{"unavailable", 503, `{"error":{"code":"notAvailable"}}`, 1},
		{"auth", 401, `{"error":{"code":"notAuthorized"}}`, 1},
		{"missing", 400, `{"error":{"code":"projectNotFound"}}`, 1},
		{"quota", 429, `{"error":{"code":"quota"}}`, 1},
		{"decode", 200, `broken`, 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; respond(w, tt.status, tt.body) })
			_, err := c.GetProject(context.Background(), "p")
			if err == nil || calls != tt.want {
				t.Fatalf("calls=%d want=%d err=%v", calls, tt.want, err)
			}
		})
	}
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; respond(w, 500, `{"error":{"code":"failure"}}`) })
	_, _ = c.CreateProject(context.Background(), "org", ProjectCreate{Name: "test"})
	if calls != 1 {
		t.Fatalf("mutation replayed %d times", calls)
	}
	calls = 0
	var delays []time.Duration
	transportCause := errors.New("transport contains secret-token and https://upload/?signature=private")
	c.http.Transport = &safeTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, transportCause }), sleep: func(ctx context.Context, d time.Duration) error { delays = append(delays, d); return nil }}
	_, err := c.GetProject(context.Background(), "p")
	var te *TransportError
	if calls != 3 || !errors.As(err, &te) || !errors.Is(err, transportCause) || strings.Contains(err.Error(), "signature") {
		t.Fatalf("transport handling calls=%d err=%v", calls, err)
	}
	if len(delays) != 2 || delays[0] < time.Second || delays[0] >= 1200*time.Millisecond || delays[1] < 2*time.Second || delays[1] >= 2200*time.Millisecond {
		t.Fatalf("unexpected retry delays %v", delays)
	}
	calls = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.http.Transport.(*safeTransport).sleep = sleepContext
	_, err = c.GetProject(ctx, "p")
	if err == nil || calls > 1 {
		t.Fatalf("cancellation replayed calls=%d", calls)
	}
}
func TestResponseShapes(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/rest/public/project/p/env":
			respond(w, 200, `{"id":"proc","actionName":"stack.updateProjectEnvs","status":"PENDING"}`)
		case "/api/rest/public/service-stack/s/autoscaling":
			respond(w, 200, `{"process":null}`)
		case "/api/rest/public/project/p/mode-upgrade":
			respond(w, 200, `{"process":{"id":"mode","status":"PENDING"}}`)
		case "/api/rest/public/project/p/service-stack":
			respond(w, 200, `{"id":"stack","name":"app","status":"NEW","process":{"id":"create","status":"PENDING"},"activeAppVersion":{"id":"active"}}`)
		case "/api/rest/public/service-stack/s/trigger-pipeline":
			respond(w, 200, `{"process":{"id":"build","status":"PENDING","appVersion":{"id":"version","name":"commit"}}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			respond(w, 404, `{}`)
		}
	})
	p, err := c.CreateProjectEnv(context.Background(), "p", EnvWrite{Key: "A", Content: "B"})
	if err != nil || p.ID != "proc" {
		t.Fatalf("direct process %#v %v", p, err)
	}
	nullable, err := c.SetAutoscaling(context.Background(), "s", AutoscalingWrite{})
	if err != nil || nullable != nil {
		t.Fatalf("nullable wrapper %#v %v", nullable, err)
	}
	nullable, err = c.UpgradeProject(context.Background(), "p", "SERIOUS")
	if err != nil || nullable == nil || nullable.ID != "mode" {
		t.Fatalf("process wrapper %#v %v", nullable, err)
	}
	service, err := c.CreateService(context.Background(), "p", ServiceCreate{Name: "app", ServiceStackVersionName: "alpine/nodejs@22"})
	if err != nil || service.ID != "stack" || service.Process == nil || service.Process.ID != "create" || service.ActiveAppVersionID != "active" {
		t.Fatalf("flattened stack %#v %v", service, err)
	}
	pipeline, err := c.TriggerGitPipeline(context.Background(), "s", PipelineRequest{BuildFromGit: "https://github.com/example/repo@main"})
	if err != nil || pipeline.Process == nil || pipeline.Process.ID != "build" || pipeline.AppVersionID != "version" {
		t.Fatalf("pipeline %#v %v", pipeline, err)
	}
}
func TestPaginationLookupAndEnvShapes(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/rest/public/client/org/project":
			calls++
			if r.URL.Query().Get("nameContains") != "a/b?#" || r.URL.Query().Get("limit") != "100" {
				t.Errorf("unexpected list query")
			}
			if r.URL.Query().Get("offset") == "0" {
				respond(w, 200, `{"list":[{"id":"other","name":"a/b?# extra","status":"ACTIVE"}],"total":2}`)
			} else if r.URL.Query().Get("offset") == "1" {
				respond(w, 200, `{"list":[{"id":"exact","name":"a/b?#","status":"ACTIVE"}],"total":2}`)
			} else {
				t.Error("wrong pagination offset")
				respond(w, 500, `{}`)
			}
		case "/api/rest/public/project/p/env", "/api/rest/public/service-stack/s/env":
			respond(w, 200, `{"items":[{"id":"env","key":"KEY","content":"value","type":"USER"}]}`)
		case "/api/rest/public/service-stack/s/user-data":
			if r.URL.Query().Get("keyContains") != "KEY" {
				t.Error("missing key filter")
			}
			respond(w, 200, `{"list":[{"id":"user","key":"KEY"}],"total":1}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			respond(w, 404, `{}`)
		}
	})
	p, err := c.GetProjectByName(context.Background(), "org", "a/b?#", []string{"ACTIVE"})
	if err != nil || p.ID != "exact" || calls != 2 {
		t.Fatalf("lookup %#v calls=%d err=%v", p, calls, err)
	}
	env, err := c.ListProjectEnv(context.Background(), "p")
	if err != nil || len(env) != 1 || env[0].ID != "env" {
		t.Fatalf("project env %#v %v", env, err)
	}
	env, err = c.ListServiceEnv(context.Background(), "s")
	if err != nil || len(env) != 1 {
		t.Fatalf("service env %#v %v", env, err)
	}
	page, err := c.ListUserData(context.Background(), "s", UserDataQuery{KeyContains: "KEY"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "user" || page.Total != 1 {
		t.Fatalf("user-data %#v %v", page, err)
	}
	_, err = CollectPages(context.Background(), func(context.Context, PageQuery) (Page[int], error) { return Page[int]{Total: 1}, nil })
	if err == nil {
		t.Fatal("nonadvancing pagination accepted")
	}
}
func TestOrganizationIDs(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, `{"list":[{"id":"membership","clientId":"org","roleCode":"ADMIN","client":{"id":"org","accountName":"account"}}],"total":1}`)
	})
	orgs, err := c.ListOrganizations(context.Background())
	if err != nil || len(orgs) != 1 || orgs[0].ID != "org" || orgs[0].MembershipID != "membership" || orgs[0].Name != "account" {
		t.Fatalf("organizations %#v %v", orgs, err)
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, `{"list":[{"id":"membership","clientId":"org","client":{"id":"wrong"}}],"total":1}`)
	})
	if _, err = c.ListOrganizations(context.Background()); err == nil {
		t.Fatal("mismatched organization IDs accepted")
	}
}
func TestUploadAndValidation(t *testing.T) {
	uploads := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		uploads++
		if r.Method != "PUT" || r.Header.Get("Authorization") != "" || r.ContentLength != 7 {
			t.Error("unsafe upload headers")
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != "archive" {
			t.Error("wrong archive")
		}
		w.WriteHeader(200)
	})
	if err := c.UploadArchive(context.Background(), c.endpoint+"/signed?secret=signature", strings.NewReader("archive"), 7); err != nil || uploads != 1 {
		t.Fatalf("upload count=%d err=%v", uploads, err)
	}
	if err := c.UploadArchive(context.Background(), "http://example.com/signed", strings.NewReader("archive"), 7); err == nil {
		t.Fatal("insecure upload accepted")
	}
	if _, err := New(Config{Endpoint: "http://127.0.0.1"}); err == nil {
		t.Fatal("production constructor accepted HTTP")
	}
	if _, err := c.GetProject(context.Background(), "bad\nsecret"); err == nil || uploads != 1 {
		t.Fatal("unsafe SDK path reached request builder")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("upload followed redirect") }))
	defer target.Close()
	redirect := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) })
	if err := redirect.UploadArchive(context.Background(), redirect.endpoint+"/signed", strings.NewReader("archive"), 7); err == nil {
		t.Fatal("redirect upload accepted")
	}
}

func TestPublicCatalogAndOrigin(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("regions request was authenticated")
		}
		respond(w, 200, `{"items":[{"name":"dev","isDefault":true,"address":"https://region.example"}]}`)
	})
	regions, err := c.Regions(context.Background())
	if err != nil || len(regions) != 1 || regions[0].Name != "dev" {
		t.Fatalf("regions %#v %v", regions, err)
	}
	for _, endpoint := range []string{"https://example.com?", "https://example.com#", "https://user@example.com", "https://example.com/path"} {
		if _, err := New(Config{Endpoint: endpoint}); err == nil {
			t.Error("non-origin endpoint accepted")
		}
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingBody) Close() error             { return nil }
func TestGETBodyReadRetries(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected network call") })
	calls := 0
	c.http.Transport = &safeTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body io.ReadCloser = failingBody{}
		if calls == 3 {
			body = io.NopCloser(strings.NewReader(`{"id":"project","name":"read"}`))
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body, Request: r}, nil
	}), sleep: func(context.Context, time.Duration) error { return nil }}
	p, err := c.GetProject(context.Background(), "p")
	if err != nil || calls != 3 || p.ID != "project" {
		t.Fatalf("body read retry calls=%d project=%s err=%v", calls, p.ID, err)
	}
}

func TestStalledGETAttemptTimesOutAndRetries(t *testing.T) {
	old := getAttemptTimeout
	getAttemptTimeout = 20 * time.Millisecond
	defer func() { getAttemptTimeout = old }()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected network call") })
	calls := 0
	c.http.Transport = &safeTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			<-r.Context().Done() // stalled server
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"project","name":"read"}`)), Request: r}, nil
	}), sleep: func(context.Context, time.Duration) error { return nil }}
	p, err := c.GetProject(context.Background(), "p")
	if err != nil || calls != 2 || p.ID != "project" {
		t.Fatalf("stalled retry calls=%d project=%s err=%v", calls, p.ID, err)
	}
	// An operation deadline that has already expired is not retried.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	if _, err := c.GetProject(ctx, "p"); err == nil || calls > 1 {
		t.Fatalf("canceled operation calls=%d err=%v", calls, err)
	}
}

func TestListOmitsAbsentFilters(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		for _, key := range []string{"nameContains", "statuses", "keyContains"} {
			if query.Has(key) {
				t.Errorf("absent filter %s was sent", key)
			}
		}
		if query.Get("limit") != "100" {
			t.Error("default page size not sent")
		}
		respond(w, 200, `{"list":[],"total":0}`)
	})
	ctx := context.Background()
	if _, err := c.ListServices(ctx, "p", ServiceQuery{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListProjects(ctx, "org", ProjectQuery{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListUserData(ctx, "s", UserDataQuery{}); err != nil {
		t.Fatal(err)
	}
}
