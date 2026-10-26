package waiter

import (
	"context"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
	"time"
)

func TestReadyToDeployFlicker(t *testing.T) {
	c := &fakeClock{limit: 30 * time.Second}
	f := &fakeAPI{}
	f.service = func() (client.Service, error) {
		status := "READY_TO_DEPLOY"
		if c.elapsed >= 2*time.Second && c.elapsed < 6*time.Second {
			status = "CREATING"
		}
		return client.Service{Status: status}, nil
	}
	_, err := WaitServiceSettled(context.Background(), f, "s", []string{"READY_TO_DEPLOY"}, nil, nil, WithClock(c))
	if err != nil || c.elapsed < 8*time.Second {
		t.Fatalf("%v elapsed %s", err, c.elapsed)
	}
}
func TestNewFailureAndListErrorsPreventSettle(t *testing.T) {
	c := &fakeClock{limit: 90 * time.Second}
	f := &fakeAPI{}
	f.processes = func(client.PageQuery) (client.Page[client.Process], error) {
		return client.Page[client.Process]{Items: []client.Process{{ID: "new", Status: "FAILED"}}, Total: 1}, nil
	}
	if _, err := WaitServiceSettled(context.Background(), f, "s", []string{"ACTIVE"}, ProcessIDs{"old": {}}, nil, WithClock(c)); err == nil {
		t.Fatal("new failure ignored")
	}
	f.processes = func(client.PageQuery) (client.Page[client.Process], error) {
		return client.Page[client.Process]{}, &client.APIError{HTTPStatus: 503}
	}
	if _, err := WaitServiceSettled(context.Background(), f, "s", []string{"ACTIVE"}, nil, nil, WithClock(c)); err == nil || c.elapsed < 60*time.Second {
		t.Fatalf("list failure incorrectly settled: %v %s", err, c.elapsed)
	}
}
func TestProcessNotFoundIsNotCompletion(t *testing.T) {
	c := &fakeClock{}
	f := &fakeAPI{process: func(string) (client.Process, error) {
		return client.Process{}, &client.APIError{HTTPStatus: 400, Code: "processNotFound"}
	}}
	if _, err := WaitProcess(context.Background(), f, "p", WithClock(c)); err == nil || c.elapsed != 0 {
		t.Fatalf("%v %s", err, c.elapsed)
	}
}
func TestProjectCreateFailure(t *testing.T) {
	f := &fakeAPI{processes: func(client.PageQuery) (client.Page[client.Process], error) {
		return client.Page[client.Process]{Items: []client.Process{{ID: "create", ActionName: "project.create", Status: "FAILED"}}, Total: 1}, nil
	}}
	if _, err := WaitProjectActive(context.Background(), f, "p"); err == nil {
		t.Fatal("project process failure ignored")
	}
}

func TestNotAvailableIsNotTransient(t *testing.T) {
	c := &fakeClock{}
	f := &fakeAPI{process: func(string) (client.Process, error) {
		return client.Process{}, &client.APIError{HTTPStatus: 503, Code: "notAvailable"}
	}}
	if _, err := WaitProcess(context.Background(), f, "p", WithClock(c)); err == nil || c.elapsed != 0 {
		t.Fatalf("%v %s", err, c.elapsed)
	}
	if !transient(&client.TransportError{}) {
		t.Fatal("classified transport error must be transient")
	}
}
