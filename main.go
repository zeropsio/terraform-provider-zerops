package main

import (
	"context"
	"flag"
	"log"

	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/zeropsio/terraform-provider-zerops/provider"
)

// version is replaced by release builds using -ldflags '-X main.version=...'.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "Run with debugger support")
	flag.Parse()
	factory := func() framework.Provider { return provider.New(version) }
	if err := providerserver.Serve(context.Background(), factory, providerserver.ServeOpts{Address: "registry.terraform.io/zeropsio/zerops", Debug: debug}); err != nil {
		log.Fatal(err)
	}
}
