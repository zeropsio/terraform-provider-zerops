package waiter

import (
	"context"
	"errors"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
	"time"
)

func TestProjectConfigPropagationAndErrors(t *testing.T) {
	clock := &fakeClock{}
	calls := 0
	err := WaitProjectConfig(context.Background(), "p", func(context.Context) (bool, error) { calls++; return calls == 3, nil }, WithClock(clock))
	if err != nil || calls != 3 || clock.elapsed < 10*time.Second {
		t.Fatalf("polls=%d elapsed=%s err=%v", calls, clock.elapsed, err)
	}
	clock = &fakeClock{}
	calls = 0
	err = WaitProjectConfig(context.Background(), "p", func(context.Context) (bool, error) {
		calls++
		return false, &client.APIError{HTTPStatus: 403, Code: "denied"}
	}, WithClock(clock))
	if err == nil || calls != 1 || clock.elapsed != 0 {
		t.Fatal("permanent error retried")
	}
	clock = &fakeClock{limit: 15 * time.Second}
	err = WaitProjectConfig(context.Background(), "p", func(context.Context) (bool, error) { return false, nil }, WithClock(clock))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("missing timeout", err)
	}
	clock = &fakeClock{}
	err = WaitProjectConfig(context.Background(), "p", func(context.Context) (bool, error) {
		return false, &client.APIError{HTTPStatus: 500, Code: "temporary"}
	}, WithClock(clock))
	if err == nil || clock.elapsed < 60*time.Second {
		t.Fatal("transient allowance not bounded", err)
	}
}
