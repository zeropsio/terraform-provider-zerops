package acctest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/terraform-provider-zerops/internal/client"
	"github.com/zeropsio/terraform-provider-zerops/internal/waiter"
)

//go:embed testdata/httpapp/*
var httpApp embed.FS

// CreateTestProject registers bounded cleanup immediately, before readiness waits.
// Cleanup deletes the entire fixture project, including services and deployments.
func CreateTestProject(t *testing.T) client.Project {
	t.Helper()
	c := Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	p, err := c.CreateProject(ctx, os.Getenv("ZEROPS_ORGANIZATION_ID"), client.ProjectCreate{Name: RandomName("tfacc")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		if err := DeleteTestProject(ctx, c, p.ID); err != nil {
			t.Errorf("cleanup project %s: %v", p.ID, err)
		}
	})
	p, err = waiter.WaitProjectActive(ctx, c, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// DeleteTestProject waits for both the deletion process and exact API absence.
func DeleteTestProject(ctx context.Context, c client.API, id string) error {
	p, err := c.DeleteProject(ctx, id)
	if client.IsNotFound(err, "projectNotFound") {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = waiter.WaitProcess(ctx, c, p.ID); err != nil {
		return err
	}
	return waiter.WaitNotFound(ctx, id, func(ctx context.Context) error { _, err := c.GetProject(ctx, id); return err }, []string{"projectNotFound"})
}

// MinimalServiceMemoryGB is shared by API and Terraform production fixtures.
// PostgreSQL single@16 uses its catalog minimum of 0.25 GB RAM. The platform
// currently fails to provision PostgreSQL with less than 2 GB of disk (generic
// HTTP 500 error); direct-API fixtures must set that disk size explicitly.
// Terraform fixtures rely on the provider disk floor instead.
func MinimalServiceMemoryGB(typ string) float64 {
	if typ == "postgresql:single@16" {
		return 0.25
	}
	return 0.125
}

// MinimalServiceInput pins production fixtures to one shared-CPU container.
// Single-node managed types fix their own count and reject horizontal settings.
// Development keeps its original API defaults and scaling coverage.
func MinimalServiceInput(input client.ServiceCreate) client.ServiceCreate {
	if !IsProd() {
		return input
	}
	one, memory, disk, cpuMode := 1, MinimalServiceMemoryGB(input.ServiceStackVersionName), 1.0, "SHARED"
	// Direct API calls bypass the provider's PostgreSQL disk floor.
	if input.ServiceStackVersionName == "postgresql:single@16" {
		disk = 2.0
	}
	limits := &client.ScalingResource{CPUCoreCount: &one, MemoryGBytes: &memory, DiskGBytes: &disk}
	input.CustomAutoscaling = &client.Autoscaling{
		Vertical: &client.VerticalAutoscaling{MinResource: limits, MaxResource: limits, CPUMode: &cpuMode, StartCPUCoreCount: &one},
	}
	if !strings.Contains(input.ServiceStackVersionName, ":single@") {
		input.CustomAutoscaling.Horizontal = &client.HorizontalAutoscaling{MinContainerCount: &one, MaxContainerCount: &one}
	}
	return input
}

// DeployHTTPApp creates and builds the embedded TCP/HTTP 3000 + UDP 8080 app.
// The project should be owned by CreateTestProject so failure cleanup is retained.
func DeployHTTPApp(t *testing.T, projectID string) client.Service {
	return deployHTTPApp(t, projectID, false)
}

// DeployHTTPAppChild registers independent service cleanup for a retained holder project.
func DeployHTTPAppChild(t *testing.T, projectID string) client.Service {
	return deployHTTPApp(t, projectID, true)
}

func deployHTTPApp(t *testing.T, projectID string, cleanup bool) client.Service {
	t.Helper()
	c := Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	start := true
	input := client.ServiceCreate{Name: RandomHostname(), ServiceStackVersionName: "alpine/nodejs@22", StartWithoutCode: &start}
	if cleanup || IsProd() {
		one, memory, disk, cpuMode := 1, 0.125, 1.0, "SHARED"
		limits := &client.ScalingResource{CPUCoreCount: &one, MemoryGBytes: &memory, DiskGBytes: &disk}
		input.CustomAutoscaling = &client.Autoscaling{
			Horizontal: &client.HorizontalAutoscaling{MinContainerCount: &one, MaxContainerCount: &one},
			Vertical:   &client.VerticalAutoscaling{MinResource: limits, MaxResource: limits, CPUMode: &cpuMode, StartCPUCoreCount: &one},
		}
	}
	created, err := c.CreateService(ctx, projectID, input)
	if err != nil {
		t.Fatal(err)
	}
	if cleanup {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			process, err := c.DeleteService(ctx, created.ID)
			if client.IsNotFound(err, "serviceStackNotFound") {
				return
			}
			if err == nil {
				_, err = waiter.WaitProcess(ctx, c, process.ID)
			}
			if err == nil {
				err = waiter.WaitNotFound(ctx, created.ID, func(ctx context.Context) error {
					_, err := c.GetService(ctx, created.ID)
					return err
				}, []string{"serviceStackNotFound"})
			}
			if err != nil {
				t.Errorf("cleaning IPv4 holder child %s: %v", created.ID, err)
			}
		})
	}
	if err = waitHTTPAppActive(ctx, c, created); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"index.js", "zerops.yml"} {
		data, err := httpApp.ReadFile("testdata/httpapp/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err = tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err = tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = gz.Close(); err != nil {
		t.Fatal(err)
	}
	upload, err := c.CreateAppVersion(ctx, created.ID, RandomName("tfacc"))
	if err != nil {
		t.Fatal(err)
	}
	if err = c.UploadArchive(ctx, upload.UploadURL, bytes.NewReader(archive.Bytes()), int64(archive.Len())); err != nil {
		t.Fatal(err)
	}
	yaml, err := httpApp.ReadFile("testdata/httpapp/zerops.yml")
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := waiter.SnapshotProcessIDs(ctx, c, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	process, err := c.DeployAppVersion(ctx, upload.ID, client.DeployRequest{ZeropsYaml: string(yaml), Setup: "app"}, true)
	if err != nil {
		t.Fatal(err)
	}
	service, err := waiter.WaitDeploy(ctx, c, upload.ID, process.ID, created.ID, baseline)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

// A new start-without-code service has no historical processes to ignore.
func waitHTTPAppActive(ctx context.Context, api waiter.ServiceAPI, created client.ServiceProcess, opts ...waiter.Option) error {
	var returned []string
	if created.Process != nil {
		returned = []string{created.Process.ID}
	}
	_, err := waiter.WaitServiceSettled(ctx, api, created.ID, []string{"ACTIVE"}, waiter.ProcessIDs{}, returned, opts...)
	return err
}
