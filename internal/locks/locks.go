// Package locks serializes mutations sharing one provider client.
package locks

import (
	"context"
	"sync"
)

type entry struct {
	token chan struct{}
	refs  int
}

// Keyed is a zero-value-ready, context-aware mutex registry.
// Entries are removed when no holder or waiter references them.
type Keyed struct {
	mu      sync.Mutex
	entries map[string]*entry
}

func (k *Keyed) Lock(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	if k.entries == nil {
		k.entries = make(map[string]*entry)
	}
	e := k.entries[key]
	if e == nil {
		e = &entry{token: make(chan struct{}, 1)}
		e.token <- struct{}{}
		k.entries[key] = e
	}
	e.refs++
	k.mu.Unlock()
	releaseRef := func() {
		k.mu.Lock()
		e.refs--
		if e.refs == 0 {
			delete(k.entries, key)
		}
		k.mu.Unlock()
	}
	select {
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	case <-e.token:
	}
	if err := ctx.Err(); err != nil {
		e.token <- struct{}{}
		releaseRef()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { e.token <- struct{}{}; releaseRef() }) }, nil
}

func ServiceKey(id string) string     { return "service:" + id }
func ProjectHTTPKey(id string) string { return "project:" + id + ":http" }
func ProjectPortKey(id string) string { return "project:" + id + ":port" }
func ProjectEnvKey(id string) string  { return "project:" + id + ":env" }
