package waiter

import (
	"context"
	"errors"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"strings"
	"testing"
	"time"
)

func TestProductionPollingMinimum(t *testing.T) {
	for _, variable := range []string{"ZEROPS_TARGET", "ZEROPS_SANDBOX"} {
		t.Run(variable, func(t *testing.T) {
			t.Setenv("ZEROPS_TARGET", "dev")
			t.Setenv("ZEROPS_SANDBOX", "dev")
			t.Setenv(variable, "prod")
			c := options([]Option{WithIntervals(time.Second, 2*time.Second, time.Second, time.Minute)})
			if c.initial != 5*time.Second || c.max != 5*time.Second || c.stability != 5*time.Second {
				t.Fatalf("production intervals not clamped: %#v", c)
			}
			c = options([]Option{WithClock(&fakeClock{})})
			if c.initial != time.Second || c.stability != 2*time.Second {
				t.Fatalf("fake-clock intervals unexpectedly clamped: %#v", c)
			}
		})
	}
}

type fakeClock struct {
	now     time.Time
	elapsed time.Duration
	limit   time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now.Add(c.elapsed) }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.elapsed += d
	if c.limit > 0 && c.elapsed >= c.limit {
		return context.DeadlineExceeded
	}
	return nil
}

type fakeAPI struct {
	clock     *fakeClock
	process   func(string) (client.Process, error)
	service   func() (client.Service, error)
	app       func() (client.AppVersion, error)
	processes func(client.PageQuery) (client.Page[client.Process], error)
	project   func() (client.Project, error)
}

func (f *fakeAPI) GetProcess(_ context.Context, id string) (client.Process, error) {
	if f.process != nil {
		return f.process(id)
	}
	return client.Process{ID: id, Status: "FINISHED"}, nil
}
func (f *fakeAPI) GetService(context.Context, string) (client.Service, error) {
	if f.service != nil {
		return f.service()
	}
	return client.Service{Status: "ACTIVE", ActiveAppVersionID: "app"}, nil
}
func (f *fakeAPI) GetAppVersion(context.Context, string) (client.AppVersion, error) {
	if f.app != nil {
		return f.app()
	}
	return client.AppVersion{Status: "ACTIVE"}, nil
}
func (f *fakeAPI) GetProject(context.Context, string) (client.Project, error) {
	if f.project != nil {
		return f.project()
	}
	return client.Project{Status: "ACTIVE"}, nil
}
func (f *fakeAPI) ListServiceProcesses(_ context.Context, _ string, q client.PageQuery) (client.Page[client.Process], error) {
	if f.processes != nil {
		return f.processes(q)
	}
	return client.Page[client.Process]{}, nil
}
func (f *fakeAPI) ListProjectProcesses(ctx context.Context, id string, q client.PageQuery) (client.Page[client.Process], error) {
	return f.ListServiceProcesses(ctx, id, q)
}
func TestProcessStatuses(t *testing.T) {
	for _, status := range []string{"FAILED", "CANCELED", "ALIEN", "PENDING", "RUNNING", "ROLLBACKING", "CANCELING", "FINISHED"} {
		t.Run(status, func(t *testing.T) {
			c := &fakeClock{limit: 10 * time.Second}
			f := &fakeAPI{process: func(id string) (client.Process, error) {
				return client.Process{ID: id, ActionName: "deploy", Status: status, Error: &client.ProcessError{Code: "bad", Message: "reason", Meta: map[string]any{"token": "secret"}}}, nil
			}}
			_, err := WaitProcess(context.Background(), f, "pid", WithClock(c))
			if status == "FINISHED" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("leaked meta")
			}
			if failed(status) {
				if !strings.Contains(err.Error(), "bad") || strings.Contains(err.Error(), "reason") || c.elapsed != 0 {
					t.Fatal(err)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	}
}
func TestSettleHiddenDeployAndReadLag(t *testing.T) {
	c := &fakeClock{limit: 40 * time.Second}
	f := &fakeAPI{clock: c}
	f.service = func() (client.Service, error) {
		status := "READY_TO_DEPLOY"
		if c.elapsed >= 6*time.Second && c.elapsed < 10*time.Second {
			status = "CREATING"
		}
		if c.elapsed >= 10*time.Second {
			status = "ACTIVE"
		}
		return client.Service{Status: status}, nil
	}
	f.processes = func(client.PageQuery) (client.Page[client.Process], error) {
		ps := []client.Process{{ID: "old", Status: "FAILED"}}
		if c.elapsed >= 6*time.Second && c.elapsed < 12*time.Second {
			ps = append(ps, client.Process{ID: "hidden", Status: "RUNNING"})
		}
		return client.Page[client.Process]{Items: ps, Total: len(ps)}, nil
	}
	_, err := WaitServiceSettled(context.Background(), f, "stack", []string{"ACTIVE"}, ProcessIDs{"old": {}}, nil, WithClock(c))
	if err != nil {
		t.Fatal(err)
	}
	if c.elapsed < 14*time.Second {
		t.Fatalf("premature settle %s", c.elapsed)
	}
}
func TestReturnedProcessHistoryLagAndOldActive(t *testing.T) {
	c := &fakeClock{limit: 30 * time.Second}
	f := &fakeAPI{}
	f.process = func(id string) (client.Process, error) { return client.Process{ID: id, Status: "FAILED"}, nil }
	_, err := WaitServiceSettled(context.Background(), f, "stack", []string{"ACTIVE"}, ProcessIDs{"returned": {}}, []string{"returned"}, WithClock(c))
	if err == nil || c.elapsed != 0 {
		t.Fatal("returned failure was ignored")
	}
	f.processes = func(client.PageQuery) (client.Page[client.Process], error) {
		status := "RUNNING"
		if c.elapsed >= 4*time.Second {
			status = "FINISHED"
		}
		return client.Page[client.Process]{Items: []client.Process{{ID: "old", Status: status}}, Total: 1}, nil
	}
	_, err = WaitServiceSettled(context.Background(), f, "stack", []string{"ACTIVE"}, ProcessIDs{"old": {}}, nil, WithClock(c))
	if err != nil || c.elapsed < 6*time.Second {
		t.Fatalf("%v elapsed %v", err, c.elapsed)
	}
}
func TestDeployReadLagAndFailure(t *testing.T) {
	for _, status := range []string{"ACTIVE", "BUILD_FAILED", "WAITING_TO_BUILD_FAILED", "CANCELLED", "UNKNOWN"} {
		t.Run(status, func(t *testing.T) {
			c := &fakeClock{limit: 25 * time.Second}
			f := &fakeAPI{}
			f.app = func() (client.AppVersion, error) { return client.AppVersion{Status: status}, nil }
			f.service = func() (client.Service, error) {
				s := client.Service{Status: "ACTIVE", ActiveAppVersionID: "old"}
				if c.elapsed >= 4*time.Second {
					s.ActiveAppVersionID = "app"
				}
				return s, nil
			}
			_, err := WaitDeploy(context.Background(), f, "app", "p", "s", nil, WithClock(c))
			if status == "ACTIVE" {
				if err != nil || c.elapsed < 6*time.Second {
					t.Fatalf("%v %v", err, c.elapsed)
				}
			} else if err == nil {
				t.Fatal("accepted failure/unknown")
			} else if status != "UNKNOWN" && c.elapsed != 0 {
				t.Fatal("failure not immediate")
			}
		})
	}
}
func TestTransientAllowanceResets(t *testing.T) {
	c := &fakeClock{limit: 200 * time.Second}
	calls := 0
	f := &fakeAPI{process: func(id string) (client.Process, error) {
		calls++
		if calls == 14 {
			return client.Process{ID: id, Status: "RUNNING"}, nil
		}
		return client.Process{}, &client.APIError{HTTPStatus: 503, Code: "unavailable"}
	}}
	_, err := WaitProcess(context.Background(), f, "p", WithClock(c))
	if err == nil || c.elapsed < 110*time.Second || c.elapsed > 140*time.Second {
		t.Fatalf("elapsed %s err %v", c.elapsed, err)
	}
}
func TestSnapshotPagination(t *testing.T) {
	f := &fakeAPI{processes: func(q client.PageQuery) (client.Page[client.Process], error) {
		return client.Page[client.Process]{Items: []client.Process{{ID: []string{"one", "two"}[q.Offset]}}, Total: 2}, nil
	}}
	ids, err := SnapshotProcessIDs(context.Background(), f, "s")
	if err != nil || len(ids) != 2 {
		t.Fatalf("%v %v", ids, err)
	}
}
func TestProjectAndDelete(t *testing.T) {
	c := &fakeClock{limit: 30 * time.Second}
	f := &fakeAPI{project: func() (client.Project, error) {
		s := "CREATING"
		if c.elapsed >= 4*time.Second {
			s = "ACTIVE"
		}
		return client.Project{Status: s}, nil
	}}
	if _, err := WaitProjectActive(context.Background(), f, "p", WithClock(c)); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := WaitNotFound(context.Background(), "p", func(context.Context) error {
		calls++
		if calls > 2 {
			return &client.APIError{HTTPStatus: 400, Code: "projectNotFound"}
		}
		return nil
	}, []string{"projectNotFound"}, WithClock(c))
	if err != nil || calls != 3 {
		t.Fatalf("%v calls %d", err, calls)
	}
	err = WaitNotFound(context.Background(), "p", func(context.Context) error { return &client.APIError{HTTPStatus: 400, Code: "processNotFound"} }, []string{"projectNotFound"}, WithClock(c))
	if err == nil {
		t.Fatal("wrong absence accepted")
	}
}

func TestProcessErrorSanitizesServerDetails(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"deploy.FAILED-1_code", "deploy.FAILED-1_code"},
		{strings.Repeat("a", 64), strings.Repeat("a", 64)},
		{"token=super-secret", "unknown"},
		{"super-secret\ninjected", "unknown"},
		{strings.Repeat("secret", 11), "unknown"},
		{"", "unknown"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			f := &fakeAPI{process: func(id string) (client.Process, error) {
				return client.Process{ID: id, ActionName: "deploy", Status: "FAILED", Error: &client.ProcessError{Code: tc.code, Message: "password=super-secret"}}, nil
			}}
			_, err := WaitProcess(context.Background(), f, "pid")
			var detail *Error
			if !errors.As(err, &detail) {
				t.Fatalf("unexpected error: %v", err)
			}
			if detail.Code != tc.want || detail.Message != "" {
				t.Fatalf("unsafe process details: %#v", detail)
			}
			for _, value := range []string{"pid", "deploy", "FAILED", tc.want} {
				if !strings.Contains(err.Error(), value) {
					t.Fatalf("missing identity/code %q: %v", value, err)
				}
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "password") {
				t.Fatalf("leaked secret: %v", err)
			}
		})
	}
}
