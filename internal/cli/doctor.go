package cli

import (
	"fmt"
	"os"

	"github.com/hightemp/phvm/internal/doctor"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system dependencies for building PHP",
	Long: `Check if the required build tools and libraries are installed.

This command verifies that all necessary dependencies for building PHP
from source are available on your system.`,
	Run: func(cmd *cobra.Command, args []string) {
		result := doctor.Check()
		fmt.Print(doctor.FormatResults(result))

		if !result.AllOK {
			os.Exit(1)
		}
	},
}
