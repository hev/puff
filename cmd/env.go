package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hev/puff/internal/config"
	"github.com/hev/puff/internal/output"
	"github.com/hev/puff/internal/regions"
	"github.com/spf13/cobra"
)

var activeMarkerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("78")) // green

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage puff environments",
}

var envAddCmd = &cobra.Command{
	Use:   "add NAME",
	Short: "Add a new environment (interactive setup)",
	Args:  cobra.ExactArgs(1),
	RunE:  runEnvAdd,
}

var envUseCmd = &cobra.Command{
	Use:   "use NAME",
	Short: "Switch active environment",
	Args:  cobra.ExactArgs(1),
	RunE:  runEnvUse,
}

var envListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all environments",
	RunE:    runEnvList,
}

var envRmCmd = &cobra.Command{
	Use:   "rm NAME",
	Short: "Remove an environment",
	Args:  cobra.ExactArgs(1),
	RunE:  runEnvRm,
}

var envShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current active environment",
	RunE:  runEnvShow,
}

var envSetContentCmd = &cobra.Command{
	Use:   "set-content FIELD",
	Short: "Set the content field shown in the browser preview",
	Long: `Set which document field is displayed as the preview in the interactive browser.

Without --namespace, sets the default for the active environment.
With --namespace, sets an override for that specific namespace.

Use an empty string "" to clear the setting.`,
	Args: cobra.ExactArgs(1),
	RunE: runEnvSetContent,
}

func init() {
	envSetContentCmd.Flags().StringP("namespace", "n", "", "Set content field for a specific namespace")

	envCmd.AddCommand(envAddCmd)
	envCmd.AddCommand(envUseCmd)
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envRmCmd)
	envCmd.AddCommand(envShowCmd)
	envCmd.AddCommand(envSetContentCmd)
	rootCmd.AddCommand(envCmd)
}

func runEnvAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("API key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	fmt.Printf("Region [%s]: ", regions.DefaultRegion)
	region, _ := reader.ReadString('\n')
	region = strings.TrimSpace(region)
	if region == "" {
		region = regions.DefaultRegion
	}

	if !regions.IsValid(region) {
		fmt.Fprintf(os.Stderr, "Error: Unknown region '%s'. Valid regions:\n", region)
		for _, r := range regions.TurbopufferRegions {
			fmt.Fprintf(os.Stderr, "  %s\n", r)
		}
		os.Exit(1)
	}

	fmt.Print("Base URL (optional, press Enter to skip): ")
	baseURL, _ := reader.ReadString('\n')
	baseURL = strings.TrimSpace(baseURL)

	if err := config.AddEnv(name, apiKey, region, baseURL); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	fmt.Printf("Environment '%s' added.\n", name)

	_, _, ok := config.GetActiveEnv()
	if ok {
		activeName, _, _ := config.GetActiveEnv()
		if activeName == name {
			fmt.Println("Set as active environment.")
		}
	}
	return nil
}

func runEnvUse(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := config.SetActive(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("Switched to environment '%s'.\n", name)
	return nil
}

func runEnvList(cmd *cobra.Command, args []string) error {
	envs := config.ListEnvs()
	if len(envs) == 0 {
		fmt.Println("No environments configured. Run 'puff env add <name>' to add one.")
		return nil
	}

	headers := []string{"", "Name", "Region", "API Key"}
	var rows [][]string
	for _, e := range envs {
		marker := ""
		if e.IsActive {
			if output.IsPlain() {
				marker = "*"
			} else {
				marker = activeMarkerStyle.Render("*")
			}
		}
		rows = append(rows, []string{
			marker,
			e.Name,
			e.Config.Region,
			config.MaskKey(e.Config.APIKey),
		})
	}

	output.PrintTable(headers, rows)
	return nil
}

func runEnvRm(cmd *cobra.Command, args []string) error {
	name := args[0]
	if !confirmPrompt(fmt.Sprintf("Are you sure you want to remove environment '%s'?", name)) {
		fmt.Println("Aborted")
		return nil
	}
	if err := config.RemoveEnv(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("Environment '%s' removed.\n", name)
	return nil
}

func runEnvShow(cmd *cobra.Command, args []string) error {
	name, env, ok := config.GetActiveEnv()
	if !ok {
		fmt.Println("No active environment. Run 'puff env add <name>' to add one.")
		return nil
	}

	fmt.Printf("Active environment: %s\n", name)
	fmt.Printf("  Region:   %s\n", env.Region)
	fmt.Printf("  API Key:  %s\n", config.MaskKey(env.APIKey))
	if env.BaseURL != "" {
		fmt.Printf("  Base URL: %s\n", env.BaseURL)
	}
	if env.ContentField != "" {
		fmt.Printf("  Content:  %s\n", env.ContentField)
	}
	if len(env.ContentFields) > 0 {
		fmt.Println("  Content overrides:")
		for ns, field := range env.ContentFields {
			fmt.Printf("    %s → %s\n", ns, field)
		}
	}
	return nil
}

func runEnvSetContent(cmd *cobra.Command, args []string) error {
	field := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")

	if err := config.SetContentField(field, namespace); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	if namespace != "" {
		if field == "" {
			fmt.Printf("Cleared content field for namespace '%s'.\n", namespace)
		} else {
			fmt.Printf("Content field for namespace '%s' set to '%s'.\n", namespace, field)
		}
	} else {
		if field == "" {
			fmt.Println("Cleared default content field.")
		} else {
			fmt.Printf("Default content field set to '%s'.\n", field)
		}
	}
	return nil
}
