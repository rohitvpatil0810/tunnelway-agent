package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Create or update Tunnelway agent config",
	RunE: func(cmd *cobra.Command, args []string) error {
		if fileExists(configPath) {
			overwrite, err := confirmOverwrite(cmd, configPath)
			if err != nil {
				return err
			}
			if !overwrite {
				fmt.Fprintln(cmd.OutOrStdout(), "Setup cancelled.")
				return nil
			}
		}

		serverURL, err := promptServerURL(cmd)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}

		v := viper.New()
		v.Set("server_url", serverURL)
		v.Set("server_path", "/_ws/agent")

		if err := v.WriteConfigAs(configPath); err != nil {
			return fmt.Errorf("failed to write config %q: %w", configPath, err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Config written to %s\n", configPath)
		fmt.Fprintln(cmd.OutOrStdout(), "Run next: tunnelway --port <local-port>")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

func promptServerURL(cmd *cobra.Command) (string, error) {
	reader := bufio.NewReader(cmd.InOrStdin())

	for {
		fmt.Fprint(cmd.OutOrStdout(), "Server URL (ws://host or wss://host): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		candidate := strings.TrimSpace(input)
		normalized, err := normalizeServerBaseURL(candidate)
		if err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Invalid URL: %v\n", err)
			continue
		}

		return normalized, nil
	}
}

func confirmOverwrite(cmd *cobra.Command, path string) (bool, error) {
	reader := bufio.NewReader(cmd.InOrStdin())
	fmt.Fprintf(cmd.OutOrStdout(), "Config %s already exists. Overwrite? [y/N]: ", path)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	answer := strings.ToLower(strings.TrimSpace(input))
	return answer == "y" || answer == "yes", nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
