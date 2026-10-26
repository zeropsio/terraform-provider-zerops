package locks

import (
	"context"
	"errors"
	"testing"
)

func TestKeyed(t *testing.T) {
	var k Keyed
	unlock, err := k.Lock(context.Background(), ServiceKey("s"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := k.Lock(ctx, ServiceKey("s")); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	other, err := k.Lock(context.Background(), ServiceKey("other"))
	if err != nil {
		t.Fatal(err)
	}
	other()
	unlock()
	unlock()
	again, err := k.Lock(context.Background(), ServiceKey("s"))
	if err != nil {
		t.Fatal(err)
	}
	again()
	if len(k.entries) != 0 {
		t.Fatal("leaked registry entries")
	}
}

func TestWaitingCancellation(t *testing.T) {
	var k Keyed
	unlock, _ := k.Lock(context.Background(), "key")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { _, err := k.Lock(ctx, "key"); done <- err }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	unlock()
}

func TestKeys(t *testing.T) {
	got := []string{ServiceKey("x"), ProjectHTTPKey("x"), ProjectPortKey("x"), ProjectEnvKey("x")}
	seen := map[string]bool{}
	for _, v := range got {
		if seen[v] {
			t.Fatal("collision")
		}
		seen[v] = true
	}
}
