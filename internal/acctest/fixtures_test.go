package acctest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

func TestMinimalServiceInput(t *testing.T) {
	cases := []struct {
		typ        string
		cpu        int
		memory     float64
		disk       float64
		containers int
		managed    bool
	}{
		{"alpine/nodejs@22", 1, 0.125, 1, 1, false},
		{"nodejs@22", 1, 0.125, 1, 1, false},
		{"ubuntu@24.04", 1, 0.125, 1, 1, false},
		{"postgresql:single@16", 1, 0.25, 2, 0, true},
		{"postgresql:ha@16", 1, 1, 2, 0, true},
		{"mariadb:single@10.6", 1, 0.25, 1, 0, true},
		{"valkey:single@7.2", 1, 0.125, 1, 0, true},
		{"clickhouse:single@25.3", 1, 0.375, 1, 0, true},
		{"elasticsearch:single@9.2", 1, 2, 2, 0, true},
		{"elasticsearch:ha@9.2", 1, 2, 2, 3, true},
		{"kafka:single@3.9", 1, 0.5, 1, 0, true},
		{"nats:single@2.12", 1, 0.25, 1, 0, true},
		{"qdrant:single@1.12", 1, 0.25, 1, 0, true},
		{"seaweedfs:single@3.85", 1, 0.5, 5, 0, true},
		{"typesense:single@30.2", 1, 0.25, 1, 0, true},
		{"meilisearch:single@1.44", 1, 0.25, 1, 0, true},
		{"mysql:ha@5.7", 1, 0.25, 1, 0, true},
		{"local-storage:single@1", 1, 0.125, 5, 0, true},
		{"ubuntu/zbox@1", 2, 2, 10, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			want := ServiceMinimum{CPU: tc.cpu, RAMGB: tc.memory, DiskGB: tc.disk, Containers: tc.containers, Managed: tc.managed}
			if got := MinimalServiceSize(tc.typ); got != want {
				t.Fatalf("minimum = %+v; want %+v", got, want)
			}
			if got := MinimalServiceMemoryGB(tc.typ); got != tc.memory {
				t.Fatalf("memory = %g; want %g", got, tc.memory)
			}
			for _, target := range []string{"dev", "prod"} {
				t.Run(target, func(t *testing.T) {
					t.Setenv("ZEROPS_TARGET", target)
					input := client.ServiceCreate{Name: "fixture", ServiceStackVersionName: tc.typ}
					got := MinimalServiceInput(input)
					if got.Name != input.Name || got.ServiceStackVersionName != tc.typ {
						t.Fatal("fixture identity changed")
					}
					if target == "dev" && !tc.managed {
						if got.CustomAutoscaling != nil {
							t.Fatal("dev runtime scaling defaults changed")
						}
						return
					}
					scaling := got.CustomAutoscaling
					if scaling == nil || scaling.Vertical == nil {
						t.Fatal("fixture scaling missing")
					}
					v := scaling.Vertical
					if v.CPUMode == nil || v.MinResource == nil || v.MaxResource == nil || v.StartCPUCoreCount == nil {
						t.Fatal("vertical scaling bounds missing")
					}
					for _, bounds := range []*client.ScalingResource{v.MinResource, v.MaxResource} {
						if bounds.CPUCoreCount == nil || bounds.MemoryGBytes == nil || bounds.DiskGBytes == nil {
							t.Fatal("vertical resource limits missing")
						}
						if *bounds.CPUCoreCount != tc.cpu || *bounds.MemoryGBytes != tc.memory || *bounds.DiskGBytes != tc.disk {
							t.Fatalf("fixture must use CPU %d / RAM %g GB / disk %g GB", tc.cpu, tc.memory, tc.disk)
						}
					}
					if *v.CPUMode != "SHARED" || *v.StartCPUCoreCount != tc.cpu {
						t.Fatal("fixture must start at its shared CPU minimum")
					}
					if tc.containers == 0 {
						if scaling.Horizontal != nil {
							t.Fatal("managed fixture must omit horizontal autoscaling")
						}
					} else {
						h := scaling.Horizontal
						if h == nil || h.MinContainerCount == nil || h.MaxContainerCount == nil || *h.MinContainerCount != tc.containers || *h.MaxContainerCount != tc.containers {
							t.Fatalf("fixture container count must be fixed at %d", tc.containers)
						}
					}
				})
			}
		})
	}
}

type fixtureClock struct{ elapsed time.Duration }

func (c *fixtureClock) Now() time.Time { return time.Time{}.Add(c.elapsed) }
func (c *fixtureClock) Sleep(_ context.Context, d time.Duration) error {
	c.elapsed += d
	if c.elapsed > 20*time.Second {
		return context.DeadlineExceeded
	}
	return nil
}

type fixtureServiceAPI struct {
	clock    *fixtureClock
	listed   []client.Process
	returned client.Process
	lookups  int
}

func (f *fixtureServiceAPI) GetProcess(context.Context, string) (client.Process, error) {
	f.lookups++
	return f.returned, nil
}
func (f *fixtureServiceAPI) ListServiceProcesses(context.Context, string, client.PageQuery) (client.Page[client.Process], error) {
	return client.Page[client.Process]{Items: f.listed, Total: len(f.listed)}, nil
}
func (f *fixtureServiceAPI) GetService(context.Context, string) (client.Service, error) {
	status := "READY_TO_DEPLOY"
	if f.clock.elapsed >= 6*time.Second {
		status = "ACTIVE"
	}
	return client.Service{ID: "service", Status: status}, nil
}

func TestHTTPAppCreateRequiresActive(t *testing.T) {
	clock := &fixtureClock{}
	api := &fixtureServiceAPI{clock: clock, returned: client.Process{ID: "create", Status: "FINISHED"}}
	created := client.ServiceProcess{Service: client.Service{ID: "service"}, Process: &client.Process{ID: "create"}}
	if err := waitHTTPAppActive(context.Background(), api, created, waiter.WithClock(clock)); err != nil {
		t.Fatal(err)
	}
	if clock.elapsed < 8*time.Second || api.lookups == 0 {
		t.Fatalf("settled before ACTIVE stability or ignored returned process: elapsed=%s lookups=%d", clock.elapsed, api.lookups)
	}
}

func TestHTTPAppCreateHasNoHistoricalFailures(t *testing.T) {
	for _, returned := range []bool{false, true} {
		clock := &fixtureClock{}
		failed := client.Process{ID: "create", ActionName: "create", Status: "FAILED"}
		api := &fixtureServiceAPI{clock: clock}
		created := client.ServiceProcess{Service: client.Service{ID: "service"}}
		if returned {
			created.Process = &failed
			api.returned = failed
		} else {
			api.listed = []client.Process{failed}
		}
		err := waitHTTPAppActive(context.Background(), api, created, waiter.WithClock(clock))
		var detail *waiter.Error
		if !errors.As(err, &detail) || clock.elapsed != 0 {
			t.Fatalf("ignored create failure (returned=%t): %v elapsed=%s", returned, err, clock.elapsed)
		}
	}
}
