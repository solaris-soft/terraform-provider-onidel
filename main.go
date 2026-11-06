package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/solaris-soft/terraform-provider-onidel/internal/provider"
)

// Set by release builds using -ldflags.
var version = "dev"

func main() {
	debug := flag.Bool("debug", false, "Enable debugger support")
	flag.Parse()
	if err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/solaris-soft/onidel", Debug: *debug,
	}); err != nil {
		log.Fatal(err)
	}
}
