// Command clavis is the single binary that runs both the server and the CLI.
package main

import (
	"os"

	"github.com/dumhouse/clavis/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
