// Package main provides the entry point for the amanmcp CLI.
package main

import (
	"os"

	"github.com/Aman-CERP/amanmcp/cmd/amanmcp/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
