// phvm - PHP Version Manager
//
// A command-line tool to install, manage, and switch between
// multiple versions of PHP built from source.
package main

import (
	"fmt"
	"os"

	"github.com/hightemp/phvm/internal/cli"
	"github.com/hightemp/phvm/internal/redact"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", redact.Text(err.Error()))
		os.Exit(cli.ExitCode(err))
	}
}
