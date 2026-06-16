/*
Copyright © 2026 Rohit Patil <rohtivpatil0810@gmail.com>
*/
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tunnelwayagent "github.com/rohitvpatil0810/tunnelway-agent/internal/tunnelway-agent"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var port int16
var configPath string
var serverURL string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "tunnelway",
	Short: "Tunnelway is a simple CLI tool to tunnel your local services to the internet.",
	Long: `Tunnelway is a simple CLI tool to tunnel your local services to the internet. 
It allows you to expose your local services to the internet without 
the need for complex configurations or additional software. 
	
With Tunnelway, you can easily share your local services with others, 
test webhooks, or access your local development environment from anywhere.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		resolvedServerURL, err := resolveServerURL(configPath)
		if err != nil {
			return err
		}

		// Initialize the agent and start it.
		tunnelwayagent.Init(port, resolvedServerURL)
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath(), "Path to config file")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().Int16VarP(&port, "port", "p", 0, "The local port to forward traffic to")
	rootCmd.Flags().StringVar(&serverURL, "server-url", "", "Runtime override for server URL (ws/wss)")

	_ = rootCmd.MarkFlagRequired("port")
	_ = viper.BindPFlag("server_url", rootCmd.Flags().Lookup("server-url"))
}

func defaultConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".tunnelway-agent.yaml"
	}

	return filepath.Join(homeDir, ".config", "tunnelway-agent", "config.yaml")
}

func resolveServerURL(cfgPath string) (string, error) {
	viper.SetConfigFile(cfgPath)
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			if !os.IsNotExist(err) {
				return "", fmt.Errorf("failed to read config file %q: %w", cfgPath, err)
			}
		}
	}

	if !viper.IsSet("server_url") {
		return "", fmt.Errorf("missing server URL: run `tunnelway setup` to create %s or pass --server-url", cfgPath)
	}
	if !viper.IsSet("server_path") {
		return "", fmt.Errorf("missing server path in %s: run `tunnelway setup`", cfgPath)
	}

	resolved := strings.TrimSpace(viper.GetString("server_url"))
	serverPath := strings.TrimSpace(viper.GetString("server_path"))
	normalized, err := buildServerURL(resolved, serverPath)
	if err != nil {
		return "", err
	}

	return normalized, nil
}
