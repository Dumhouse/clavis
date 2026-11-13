// Command clidocs regenerates the CLI reference (docs/reference and the JSON
// used by the docs site and the web app) from the command tree and
// internal/cli/reference.yaml. Run it from the repository root:
//
//	go run ./internal/tools/clidocs
package main

import (
	"fmt"
	"os"

	"github.com/dumhouse/clavis/internal/cli"
)

func main() {
	ref, err := cli.BuildReference(cli.NewRootCommand())
	if err == nil {
		var files map[string][]byte
		files, err = cli.ReferenceFiles(ref)
		if err == nil {
			err = cli.WriteReferenceFiles(".", files)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "clidocs:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d commands and %d topics to docs/reference\n", len(ref.Commands), len(ref.Topics))
}
