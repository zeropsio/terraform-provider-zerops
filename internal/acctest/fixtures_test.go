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
	for typ, memory := range map[string]float64{"alpine/nodejs@22": 0.125, "valkey:single@7.2": 0.125, "postgresql:single@16": 0.25} {
		t.Run(typ, func(t *testing.T) {
			t.Setenv("ZEROPS_TARGET", "dev")
			input := client.ServiceCreate{Name: "fixture", ServiceStackVersionName: typ}
			if got := MinimalServiceInput(input); got.CustomAutoscaling != nil {
				t.Fatal("dev scaling defaults changed")
			}
			t.Setenv("ZEROPS_TARGET", "prod")
			got := MinimalServiceInput(input)
			scaling := got.CustomAutoscaling
			if got.Name != input.Name || got.ServiceStackVersionName != typ || scaling == nil || scaling.Vertical == nil {
				t.Fatal("production fixture identity or scaling missing")
			}
			disk := 1.0
			if typ == "postgresql:single@16" {
				disk = 2.0
			}
			v := scaling.Vertical
			if *v.CPUMode != "SHARED" || *v.MinResource.CPUCoreCount != 1 || *v.MaxResource.CPUCoreCount != 1 || *v.MinResource.MemoryGBytes != memory || *v.MaxResource.MemoryGBytes != memory || *v.MinResource.DiskGBytes != disk || *v.MaxResource.DiskGBytes != disk || *v.StartCPUCoreCount != 1 {
				t.Fatalf("production fixture must use CPU 1 / RAM %g GB / disk %g GB", memory, disk)
			}
			if typ == "alpine/nodejs@22" {
				if scaling.Horizontal == nil || *scaling.Horizontal.MinContainerCount != 1 || *scaling.Horizontal.MaxContainerCount != 1 {
					t.Fatal("runtime container count is not fixed at one")
				}
			} else if scaling.Horizontal != nil {
				t.Fatal("single-node managed fixture must not set horizontal autoscaling")
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
