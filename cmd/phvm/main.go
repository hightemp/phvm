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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
