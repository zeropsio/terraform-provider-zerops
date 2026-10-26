package waiter

import (
	"context"
	"fmt"
	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"reflect"
	"time"
)

type BackupAPI interface {
	GetBackupConfig(context.Context, string) (client.BackupConfig, error)
}

func WaitBackupConfig(ctx context.Context, api BackupAPI, id string, want client.BackupConfigWrite, opts ...Option) (client.BackupConfig, error) {
	p := newPoll(opts)
	for {
		v, err := api.GetBackupConfig(ctx, id)
		if err == nil && reflect.DeepEqual(v.BackupConfigWrite, want) {
			return v, nil
		}
		if e := p.readError(err); e != nil {
			return v, e
		}
		if err = p.sleep(ctx, 5*time.Second); err != nil {
			return v, fmt.Errorf("backup configuration for service %q did not match: %w", id, err)
		}
	}
}

type StorageCDNAPI interface {
	GetService(context.Context, string) (client.Service, error)
}

func WaitStorageCDN(ctx context.Context, api StorageCDNAPI, id string, want bool, opts ...Option) (client.Service, error) {
	p := newPoll(opts)
	for {
		v, err := api.GetService(ctx, id)
		if err == nil {
			if v.CDNEnabled == nil {
				return v, fmt.Errorf("storage service %q response missing cdnEnabled", id)
			}
			if *v.CDNEnabled == want {
				return v, nil
			}
		}
		if e := p.readError(err); e != nil {
			return v, e
		}
		if err = p.sleep(ctx, 5*time.Second); err != nil {
			return v, fmt.Errorf("CDN flag for service %q did not match: %w", id, err)
		}
	}
}
