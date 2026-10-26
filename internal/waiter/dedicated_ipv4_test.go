package waiter

import (
	"context"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
	"time"
)

func TestDedicatedIPv4Lag(t *testing.T) {
	clock := &fakeClock{}
	reads := 0
	address := "192.0.2.8"
	api := &fakeAPI{project: func() (client.Project, error) {
		reads++
		p := client.Project{ID: "project"}
		if reads == 3 {
			p.PublicIPv4 = &address
		}
		return p, nil
	}}
	got, err := WaitDedicatedIPv4(context.Background(), api, "project", WithClock(clock))
	if err != nil || got != address || reads != 3 || clock.elapsed < 10*time.Second {
		t.Fatalf("address=%s reads=%d elapsed=%s err=%v", got, reads, clock.elapsed, err)
	}
}
func TestDedicatedIPv4MalformedAndErrors(t *testing.T) {
	for _, value := range []string{"", "invalid", "2001:db8::1"} {
		p := client.Project{ID: "project", PublicIPv4: &value}
		if _, err := DedicatedIPv4Address(p); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	for _, code := range []string{"projectNotFound", "notAuthorized"} {
		api := &fakeAPI{project: func() (client.Project, error) { return client.Project{}, &client.APIError{HTTPStatus: 400, Code: code} }}
		if _, err := WaitDedicatedIPv4(context.Background(), api, "project"); err == nil {
			t.Fatal("masked API error")
		}
	}
}
