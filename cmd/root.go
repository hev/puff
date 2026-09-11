package cmd

import (
	"fmt"
	"os"

	debugpkg "github.com/hev/puff/internal/debug"
	"github.com/hev/puff/internal/output"
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X github.com/hev/puff/cmd.version=x.y.z"
var version = "dev"

var (
	debugFlag      bool
	outputModeFlag string
)

var rootCmd = &cobra.Command{
	Use:   "puff",
	Short: "CLI tool for Turbopuffer vector database",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if debugFlag {
			debugpkg.Enable()
		}
		output.CurrentMode = output.Resolve(outputModeFlag)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		region, _ := cmd.Flags().GetString("region")
		return launchBrowser(region)
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Enable debug output")
	rootCmd.PersistentFlags().StringVarP(&outputModeFlag, "output", "o", "", "Output format: human or plain")
	rootCmd.Version = version
	rootCmd.SetHelpCommand(&cobra.Command{Use: "no-help", Hidden: true})
	rootCmd.PersistentFlags().BoolP("help", "h", false, "Help for puff")
	rootCmd.Flags().StringP("region", "r", "", "Override the region")
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
