package waiter

import (
	"context"
	"errors"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"testing"
	"time"
)

type backupFake struct {
	read func() (client.BackupConfig, error)
}

func (f backupFake) GetBackupConfig(context.Context, string) (client.BackupConfig, error) {
	return f.read()
}
func TestBackupReadbackLagAndTimeout(t *testing.T) {
	clock := &fakeClock{limit: time.Minute}
	period := "15 3 * * *"
	want := client.BackupConfigWrite{BackupPeriod: &period}
	calls := 0
	f := backupFake{read: func() (client.BackupConfig, error) {
		calls++
		v := client.BackupConfig{}
		if calls >= 3 {
			v.BackupConfigWrite = want
		}
		return v, nil
	}}
	if _, e := WaitBackupConfig(context.Background(), f, "s", want, WithClock(clock)); e != nil || calls != 3 || clock.elapsed < 10*time.Second {
		t.Fatal(e, calls, clock.elapsed)
	}
	clock = &fakeClock{limit: 10 * time.Second}
	f.read = func() (client.BackupConfig, error) { return client.BackupConfig{}, nil }
	if _, e := WaitBackupConfig(context.Background(), f, "s", want, WithClock(clock)); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("mismatch did not time out", e)
	}
}
func TestStorageCDNReadbackLagAndMissing(t *testing.T) {
	clock := &fakeClock{limit: time.Minute}
	calls := 0
	api := &fakeAPI{service: func() (client.Service, error) {
		calls++
		enabled := calls > 2
		return client.Service{CDNEnabled: &enabled}, nil
	}}
	if _, e := WaitStorageCDN(context.Background(), api, "s", true, WithClock(clock)); e != nil || calls != 3 {
		t.Fatal(e, calls)
	}
	api.service = func() (client.Service, error) { return client.Service{}, nil }
	if _, e := WaitStorageCDN(context.Background(), api, "s", true, WithClock(clock)); e == nil {
		t.Fatal("missing flag accepted")
	}
}
func TestServiceConfigAuthNotRetried(t *testing.T) {
	calls := 0
	api := backupFake{read: func() (client.BackupConfig, error) {
		calls++
		return client.BackupConfig{}, &client.APIError{HTTPStatus: 403, Code: "forbidden"}
	}}
	if _, e := WaitBackupConfig(context.Background(), api, "s", client.BackupConfigWrite{}); e == nil || calls != 1 {
		t.Fatal(e, calls)
	}
}
