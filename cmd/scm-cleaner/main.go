// Command scm-cleaner is the CLI entry point. It contains no business
// logic itself - see internal/cli for command wiring and internal/app for
// use cases.
package main

import (
	"os"

	"github.com/domehahn/housekeeping/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:]))
}
