package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// These will be set by the Go compiler (-ldflags; see Makefile)
	version   string
	buildUser string
	gitTag    string
	buildDate string

	// Definition of the version subcommand
	versionCmd = &cobra.Command{
		Use:   "version [no options!]",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Auklet version: %s-%s (built by %s on %s)\n", version, gitTag, buildUser, buildDate)
		},
	}
)

func init() {
	RootCmd.AddCommand(versionCmd)
}
