package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is injected at build time via:
//   go build -ldflags "-X linkedin-jobs/cmd.Version=<v>"
// It defaults to "dev" for `go run` or any unbuilt binary.
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the linkedin-jobs version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
