package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/zeropsio/terraform-provider-zerops/internal/provider"
	"github.com/zeropsio/terraform-provider-zerops/internal/registry"
)

// version is replaced by release builds using -ldflags '-X main.version=...'.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "Run with debugger support")
	flag.Parse()
	if err := providerserver.Serve(context.Background(), provider.New(version, registry.New()), providerserver.ServeOpts{Address: "registry.terraform.io/zeropsio/zerops", Debug: debug}); err != nil {
		log.Fatal(err)
	}
}
