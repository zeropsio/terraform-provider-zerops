// Package waiter implements operation-scoped, read-only readiness polling.
package waiter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/zeropsio/terraform-provider-zerops/internal/client"
)

type Clock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type config struct {
	clock                              Clock
	initial, max, stability, transient time.Duration
}
type Option func(*config)

func WithClock(c Clock) Option {
	return func(o *config) {
		if c != nil {
			o.clock = c
		}
	}
}
func WithIntervals(initial, max, stability, transient time.Duration) Option {
	return func(o *config) {
		if initial > 0 {
			o.initial = initial
		}
		if max > 0 {
			o.max = max
		}
		if stability > 0 {
			o.stability = stability
		}
		if transient > 0 {
			o.transient = transient
		}
	}
}
func options(opts []Option) config {
	c := config{realClock{}, time.Second, 5 * time.Second, 2 * time.Second, 60 * time.Second}
	for _, o := range opts {
		o(&c)
	}
	if _, real := c.clock.(realClock); real && (os.Getenv("ZEROPS_TARGET") == "prod" || os.Getenv("ZEROPS_SANDBOX") == "prod") {
		// Shared production readback/process polling must never run faster than 5s.
		if c.initial < 5*time.Second {
			c.initial = 5 * time.Second
		}
		if c.max < 5*time.Second {
			c.max = 5 * time.Second
		}
		if c.stability < 5*time.Second {
			c.stability = 5 * time.Second
		}
	}
	return c
}

// Error preserves operation identity and last observed status. Server messages
// and metadata are omitted because they can contain credentials or application data.
type Error struct {
	ResourceID, ProcessID, Action, Status, Code, Message string
	Cause                                                error
}

func (e *Error) Error() string {
	return fmt.Sprintf("resource %q process %q action %q status %q: %s %s: %v", e.ResourceID, e.ProcessID, e.Action, e.Status, e.Code, e.Message, e.Cause)
}
func (e *Error) Unwrap() error { return e.Cause }
func processError(p client.Process) error {
	e := &Error{ProcessID: p.ID, Action: p.ActionName, Status: p.Status}
	if p.Error != nil {
		e.Code = sanitizeProcessCode(p.Error.Code)
	}
	return e
}

func sanitizeProcessCode(code string) string {
	if len(code) == 0 || len(code) > 64 {
		return "unknown"
	}
	for _, r := range code {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
			return "unknown"
		}
	}
	return code
}
func failed(s string) bool   { return s == "FAILED" || s == "CANCELED" || s == "CANCELLED" }
func terminal(s string) bool { return s == "FINISHED" || failed(s) }
func transient(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if e, ok := client.AsAPIError(err); ok {
		return e.HTTPStatus >= 500 && e.HTTPStatus <= 599 && !(e.HTTPStatus == 503 && e.Code == "notAvailable")
	}
	var transport *client.TransportError
	if errors.As(err, &transport) {
		return true
	}
	var n net.Error
	return errors.As(err, &n)
}

type poll struct {
	c     config
	delay time.Duration
	since *time.Time
}

func newPoll(opts []Option) *poll { c := options(opts); return &poll{c: c, delay: c.initial} }
func (p *poll) readError(err error) error {
	if err == nil {
		p.since = nil
		return nil
	}
	if !transient(err) {
		return err
	}
	now := p.c.clock.Now()
	if p.since == nil {
		p.since = &now
	}
	if now.Sub(*p.since) >= p.c.transient {
		return err
	}
	return nil
}
func (p *poll) sleep(ctx context.Context, min time.Duration) error {
	d := p.delay
	if d < min {
		d = min
	}
	if err := p.c.clock.Sleep(ctx, d); err != nil {
		return err
	}
	p.delay = p.delay * 3 / 2
	if p.delay > p.c.max {
		p.delay = p.c.max
	}
	return nil
}

type ProcessGetter interface {
	GetProcess(context.Context, string) (client.Process, error)
}
type ServiceProcessLister interface {
	ListServiceProcesses(context.Context, string, client.PageQuery) (client.Page[client.Process], error)
}
type ServiceAPI interface {
	ProcessGetter
	ServiceProcessLister
	GetService(context.Context, string) (client.Service, error)
}
type ProjectAPI interface {
	GetProject(context.Context, string) (client.Project, error)
	ListProjectProcesses(context.Context, string, client.PageQuery) (client.Page[client.Process], error)
}
type DeployAPI interface {
	ServiceAPI
	GetAppVersion(context.Context, string) (client.AppVersion, error)
}
type ProcessIDs map[string]struct{}

func listProcesses(ctx context.Context, read func(context.Context, string, client.PageQuery) (client.Page[client.Process], error), id string) ([]client.Process, error) {
	var all []client.Process
	for offset := 0; ; {
		page, err := read(ctx, id, client.PageQuery{Offset: offset, Limit: 100})
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		offset += len(page.Items)
		if offset >= page.Total {
			return all, nil
		}
		if len(page.Items) == 0 {
			return nil, fmt.Errorf("process pagination made no progress at offset %d", offset)
		}
	}
}
func SnapshotProcessIDs(ctx context.Context, api ServiceProcessLister, id string) (ProcessIDs, error) {
	ps, err := listProcesses(ctx, api.ListServiceProcesses, id)
	if err != nil {
		return nil, err
	}
	ids := make(ProcessIDs, len(ps))
	for _, p := range ps {
		ids[p.ID] = struct{}{}
	}
	return ids, nil
}

func WaitProcess(ctx context.Context, api ProcessGetter, id string, opts ...Option) (client.Process, error) {
	p := newPoll(opts)
	last := client.Process{ID: id}
	for {
		if err := ctx.Err(); err != nil {
			return last, &Error{ProcessID: id, Action: last.ActionName, Status: last.Status, Cause: err}
		}
		v, err := api.GetProcess(ctx, id)
		if fatal := p.readError(err); fatal != nil {
			return last, &Error{ProcessID: id, Action: last.ActionName, Status: last.Status, Cause: fatal}
		}
		if err == nil {
			last = v
			if failed(v.Status) {
				return last, processError(v)
			}
			if v.Status == "FINISHED" {
				return v, nil
			}
		}
		if err := p.sleep(ctx, 0); err != nil {
			return last, &Error{ProcessID: id, Action: last.ActionName, Status: last.Status, Cause: err}
		}
	}
}

func WaitNotFound(ctx context.Context, id string, read func(context.Context) error, codes []string, opts ...Option) error {
	p := newPoll(opts)
	for {
		if err := ctx.Err(); err != nil {
			return &Error{ResourceID: id, Cause: err}
		}
		err := read(ctx)
		if err != nil && client.IsNotFound(err, codes...) {
			return nil
		}
		if fatal := p.readError(err); fatal != nil {
			return &Error{ResourceID: id, Cause: fatal}
		}
		if err := p.sleep(ctx, 0); err != nil {
			return &Error{ResourceID: id, Cause: err}
		}
	}
}
