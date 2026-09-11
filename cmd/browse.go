package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hev/puff/internal/tui"
	"github.com/spf13/cobra"
)

var browseCmd = &cobra.Command{
	Use:   "browse",
	Short: "Interactive browser for namespaces and documents",
	RunE:  runBrowse,
}

func init() {
	browseCmd.Flags().StringP("region", "r", "", "Override the region")
	rootCmd.AddCommand(browseCmd)
}

func runBrowse(cmd *cobra.Command, args []string) error {
	region, _ := cmd.Flags().GetString("region")
	return launchBrowser(region)
}

func launchBrowser(region string) error {
	m := tui.New(region)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	return nil
}
