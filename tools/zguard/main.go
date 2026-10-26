// Command zguard applies the shared production policy to shell tooling.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/terraform-provider-zerops/internal/client"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(97)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected resolve-org, check, request, upload, record-project, record-ipv4, record-ipv4-release, or check-upload")
	}
	target, err := client.ParseTarget(os.Getenv("ZEROPS_TARGET"))
	if err != nil {
		return err
	}
	if args[0] == "check-upload" {
		if len(args) != 2 {
			return fmt.Errorf("usage: zguard check-upload URL")
		}
		if err := client.ValidateUploadURL(target, args[1]); err != nil {
			return err
		}
		u, _ := url.Parse(args[1])
		fmt.Println(u.Hostname())
		return nil
	}
	if target != client.TargetProd {
		return fmt.Errorf("this command requires ZEROPS_TARGET=prod")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if args[0] == "upload" {
		if len(args) != 2 {
			return fmt.Errorf("usage: zguard upload ARCHIVE (URL on stdin)")
		}
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 65537))
		if err != nil || len(data) > 65536 {
			return fmt.Errorf("cannot read upload URL")
		}
		value := strings.TrimSpace(string(data))
		if err := client.ValidateUploadURL(target, value); err != nil {
			return err
		}
		archive, err := os.Open(args[1])
		if err != nil {
			return fmt.Errorf("cannot open upload archive")
		}
		defer archive.Close()
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, value, archive)
		if err != nil {
			return fmt.Errorf("invalid upload request")
		}
		start := time.Now()
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			return fmt.Errorf("archive upload failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return fmt.Errorf("archive upload redirects are disabled")
		}
		u, _ := url.Parse(value)
		return json.NewEncoder(os.Stdout).Encode(struct {
			Status int     `json:"status"`
			Time   float64 `json:"time"`
			Host   string  `json:"host"`
		}{resp.StatusCode, time.Since(start).Seconds(), u.Hostname()})
	}
	ledger := os.Getenv("ZEROPS_PROD_LEDGER")
	if !filepath.IsAbs(ledger) {
		return fmt.Errorf("ZEROPS_PROD_LEDGER must be absolute")
	}
	switch args[0] {
	case "record-project", "record-ipv4":
		if len(args) != 4 {
			return fmt.Errorf("usage: zguard %s ID VALUE SOURCE", args[0])
		}
		if args[0] == "record-project" {
			return client.RecordProject(ledger, args[1], args[2], args[3])
		}
		return client.RecordIPv4(ledger, args[1], args[2], args[3])
	case "record-ipv4-release":
		if len(args) != 2 {
			return fmt.Errorf("usage: zguard record-ipv4-release PROJECT_ID")
		}
	case "resolve-org":
		if len(args) != 1 {
			return fmt.Errorf("usage: zguard resolve-org")
		}
	case "check", "request":
		if len(args) < 3 || len(args) > 4 {
			return fmt.Errorf("usage: zguard %s METHOD PATH [BODYFILE]", args[0])
		}
		if !strings.HasPrefix(args[2], "/") || strings.HasPrefix(args[2], "//") || strings.ContainsAny(args[2], "#\\") {
			return fmt.Errorf("request requires a relative API path")
		}
	default:
		return fmt.Errorf("unknown zguard command")
	}
	endpoint := os.Getenv("ZEROPS_ENDPOINT")
	if err := client.ValidateTargetEndpoint(target, endpoint); err != nil {
		return err
	}
	if strings.TrimSpace(os.Getenv("ZEROPS_TOKEN")) == "" {
		return fmt.Errorf("ZEROPS_TOKEN is required")
	}
	guard, err := client.NewProdTransport(ctx, client.Config{Endpoint: endpoint, Token: os.Getenv("ZEROPS_TOKEN")}, os.Getenv("ZEROPS_ORGANIZATION_ID"), ledger, "probe")
	if err != nil {
		return err
	}
	if args[0] == "resolve-org" {
		fmt.Println(guard.OrgID)
		return nil
	}
	if args[0] == "record-ipv4-release" {
		if err := guard.RecordIPv4Release(ctx, args[1]); err != nil {
			return fmt.Errorf("project %q: IPv4 release refused: %w", args[1], err)
		}
		fmt.Printf("project %q: IPv4 release recorded\n", args[1])
		return nil
	}
	var body io.Reader
	if len(args) == 4 {
		f, err := os.Open(args[3])
		if err != nil {
			return fmt.Errorf("cannot read request body file")
		}
		defer f.Close()
		body = f
	}
	req, err := http.NewRequestWithContext(ctx, args[1], endpoint+"/api/rest/public"+args[2], body)
	if err != nil {
		return fmt.Errorf("invalid request")
	}
	if args[0] == "check" {
		return guard.Check(ctx, req)
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("ZEROPS_TOKEN"))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	start := time.Now()
	resp, err := guard.RoundTrip(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cannot read API response")
	}
	var headers strings.Builder
	for name, values := range resp.Header {
		for _, value := range values {
			fmt.Fprintf(&headers, "%s: %s\n", name, value)
		}
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Status  int     `json:"status"`
		Time    float64 `json:"time"`
		Body    string  `json:"body"`
		Headers string  `json:"headers"`
	}{resp.StatusCode, time.Since(start).Seconds(), string(data), headers.String()})
}
