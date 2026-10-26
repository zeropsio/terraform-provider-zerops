package waiter

import (
	"context"
	"time"
)

// WaitProjectConfig waits for a semantic feature readback under the caller's deadline.
// The callback returns false for propagation lag and an error for malformed data.
func WaitProjectConfig(ctx context.Context, id string, read func(context.Context) (bool, error), opts ...Option) error {
	p := newPoll(opts)
	for {
		if err := ctx.Err(); err != nil {
			return &Error{ResourceID: id, Cause: err}
		}
		ready, err := read(ctx)
		if fatal := p.readError(err); fatal != nil {
			return &Error{ResourceID: id, Cause: fatal}
		}
		if err == nil && ready {
			return nil
		}
		if err := p.sleep(ctx, 5*time.Second); err != nil {
			return &Error{ResourceID: id, Cause: err}
		}
	}
}
