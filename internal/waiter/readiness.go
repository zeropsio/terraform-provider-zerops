package waiter

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zeropsio/terraform-provider-zerops/internal/client"
)

// serviceObservation checks all current work, including nonterminal baseline work.
// Baseline terminal failures are historical, but returned IDs always remain relevant.
func serviceObservation(ctx context.Context, api ServiceAPI, id string, baseline ProcessIDs, returned []string) (client.Service, bool, error) {
	ps, err := listProcesses(ctx, api.ListServiceProcesses, id)
	if err != nil {
		return client.Service{}, false, err
	}
	seen := make(map[string]struct{}, len(ps))
	for _, p := range ps {
		seen[p.ID] = struct{}{}
	}
	for _, pid := range returned {
		if _, ok := seen[pid]; ok {
			continue
		}
		p, err := api.GetProcess(ctx, pid)
		if err != nil {
			return client.Service{}, false, err
		}
		ps = append(ps, p)
	}
	active := false
	for _, p := range ps {
		_, old := baseline[p.ID]
		relevant := !old
		for _, pid := range returned {
			if pid == p.ID {
				relevant = true
				break
			}
		}
		if relevant && failed(p.Status) {
			return client.Service{}, false, processError(p)
		}
		if !terminal(p.Status) {
			active = true
		}
	}
	s, err := api.GetService(ctx, id)
	if err != nil {
		return s, false, err
	}
	if failed(s.Status) || strings.HasSuffix(s.Status, "_FAILED") {
		return s, false, &Error{ResourceID: id, Status: s.Status, Message: "service entered a failure status"}
	}
	return s, !active, nil
}
func expectedStatus(status string, expected []string) bool {
	for _, s := range expected {
		if s == status {
			return true
		}
	}
	return false
}
func WaitServiceSettled(ctx context.Context, api ServiceAPI, id string, expected []string, baseline ProcessIDs, returned []string, opts ...Option) (client.Service, error) {
	p := newPoll(opts)
	var stable *time.Time
	var last client.Service
	for {
		if err := ctx.Err(); err != nil {
			return last, &Error{ResourceID: id, Status: last.Status, Cause: err}
		}
		s, idle, err := serviceObservation(ctx, api, id, baseline, returned)
		if fatal := p.readError(err); fatal != nil {
			return last, &Error{ResourceID: id, Status: last.Status, Cause: fatal}
		}
		if err == nil {
			last = s
		}
		if err != nil || !idle || !expectedStatus(s.Status, expected) {
			stable = nil
		} else {
			now := p.c.clock.Now()
			if stable == nil {
				stable = &now
			} else if now.Sub(*stable) >= p.c.stability {
				return s, nil
			}
		}
		if err := p.sleep(ctx, p.c.stability); err != nil {
			return last, &Error{ResourceID: id, Status: last.Status, Cause: err}
		}
	}
}

func WaitProjectActive(ctx context.Context, api ProjectAPI, id string, opts ...Option) (client.Project, error) {
	p := newPoll(opts)
	var last client.Project
	var latest client.Process
	for {
		if err := ctx.Err(); err != nil {
			return last, &Error{ResourceID: id, ProcessID: latest.ID, Action: latest.ActionName, Status: last.Status, Cause: err}
		}
		ps, err := listProcesses(ctx, api.ListProjectProcesses, id)
		if err == nil {
			for _, v := range ps {
				if v.ActionName != "project.create" {
					continue
				}
				if latest.ID == "" || v.Created == nil || latest.Created == nil || v.Created.After(*latest.Created) {
					latest = v
				}
				if failed(v.Status) {
					return last, processError(v)
				}
			}
			last, err = api.GetProject(ctx, id)
		}
		if fatal := p.readError(err); fatal != nil {
			return last, &Error{ResourceID: id, ProcessID: latest.ID, Action: latest.ActionName, Status: last.Status, Cause: fatal}
		}
		if err == nil {
			if last.Status == "ACTIVE" {
				return last, nil
			}
			if failed(last.Status) || strings.HasSuffix(last.Status, "_FAILED") {
				return last, &Error{ResourceID: id, ProcessID: latest.ID, Action: latest.ActionName, Status: last.Status, Message: "project entered a failure status"}
			}
		}
		if err := p.sleep(ctx, 0); err != nil {
			return last, &Error{ResourceID: id, ProcessID: latest.ID, Action: latest.ActionName, Status: last.Status, Cause: err}
		}
	}
}

// WaitDeploy polls all three direct views each iteration, so a finished process
// never masks an app failure or a lagging active-version projection.
func WaitDeploy(ctx context.Context, api DeployAPI, appID, processID, stackID string, baseline ProcessIDs, opts ...Option) (client.Service, error) {
	p := newPoll(opts)
	var last client.Service
	var process client.Process
	var app client.AppVersion
	var stable *time.Time
	for {
		if err := ctx.Err(); err != nil {
			return last, &Error{ResourceID: stackID, ProcessID: processID, Action: process.ActionName, Status: process.Status + "/" + app.Status + "/" + last.Status, Cause: err}
		}
		observedProcess, observedApp, observedService, readErr := observeDeploy(ctx, api, appID, processID, stackID)
		if observedProcess != nil {
			process = *observedProcess
		}
		if observedApp != nil {
			app = *observedApp
		}
		if observedService != nil {
			last = *observedService
		}
		var operationError *Error
		if errors.As(readErr, &operationError) {
			return last, readErr
		}
		ready := readErr == nil && process.Status == "FINISHED" && app.Status == "ACTIVE" && last.ActiveAppVersionID == appID && last.Status == "ACTIVE"
		if ready {
			s, idle, err := serviceObservation(ctx, api, stackID, baseline, []string{processID})
			if err != nil {
				readErr = err
				ready = false
			} else {
				last = s
				ready = idle && s.Status == "ACTIVE" && s.ActiveAppVersionID == appID
			}
		}
		if fatal := p.readError(readErr); fatal != nil {
			return last, &Error{ResourceID: stackID, ProcessID: processID, Action: process.ActionName, Status: process.Status + "/" + app.Status + "/" + last.Status, Cause: fatal}
		}
		if !ready {
			stable = nil
		} else {
			now := p.c.clock.Now()
			if stable == nil {
				stable = &now
			} else if now.Sub(*stable) >= p.c.stability {
				return last, nil
			}
		}
		if err := p.sleep(ctx, p.c.stability); err != nil {
			return last, &Error{ResourceID: stackID, ProcessID: processID, Action: process.ActionName, Status: process.Status + "/" + app.Status + "/" + last.Status, Cause: err}
		}
	}
}

// Each read has the same operation deadline. Buffered channels let a failure
// return promptly without leaving a sibling blocked while delivering its result.
func observeDeploy(ctx context.Context, api DeployAPI, appID, processID, stackID string) (*client.Process, *client.AppVersion, *client.Service, error) {
	type result struct {
		process *client.Process
		app     *client.AppVersion
		service *client.Service
		err     error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan result, 3)
	go func() { v, err := api.GetProcess(ctx, processID); results <- result{process: &v, err: err} }()
	go func() { v, err := api.GetAppVersion(ctx, appID); results <- result{app: &v, err: err} }()
	go func() { v, err := api.GetService(ctx, stackID); results <- result{service: &v, err: err} }()
	var process *client.Process
	var app *client.AppVersion
	var service *client.Service
	var readErr error
	for range 3 {
		select {
		case <-ctx.Done():
			return process, app, service, ctx.Err()
		case r := <-results:
			if r.err != nil {
				if readErr == nil || !transient(r.err) {
					readErr = r.err
				}
				continue
			}
			if r.process != nil {
				process = r.process
				if failed(process.Status) {
					return process, app, service, processError(*process)
				}
			}
			if r.app != nil {
				app = r.app
				if failed(app.Status) || strings.HasSuffix(app.Status, "_FAILED") {
					return process, app, service, &Error{ResourceID: appID, ProcessID: processID, Status: app.Status, Message: "app version failed"}
				}
			}
			if r.service != nil {
				service = r.service
				if failed(service.Status) || strings.HasSuffix(service.Status, "_FAILED") {
					return process, app, service, &Error{ResourceID: stackID, ProcessID: processID, Status: service.Status, Message: "service failed"}
				}
			}
		}
	}
	return process, app, service, readErr
}
