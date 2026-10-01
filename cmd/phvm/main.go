// phvm - PHP Version Manager
//
// A command-line tool to install, manage, and switch between
// multiple versions of PHP built from source.
package main

import (
	"fmt"
	"os"

	"github.com/hightemp/phvm/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, cli.FormatError(err, os.Stderr))
		os.Exit(cli.ExitCode(err))
	}
}
