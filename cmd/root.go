/*
Copyright © 2026 Rohit Patil <rohtivpatil0810@gmail.com>
*/
package cmd

import (
	"os"

	tunnelwayagent "github.com/rohitvpatil0810/tunnelway-agent/internal/tunnelway-agent"
	"github.com/spf13/cobra"
)

var port int16

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "tunnelway",
	Short: "Tunnelway is a simple CLI tool to tunnel your local services to the internet.",
	Long: `Tunnelway is a simple CLI tool to tunnel your local services to the internet. 
It allows you to expose your local services to the internet without 
the need for complex configurations or additional software. 
	
With Tunnelway, you can easily share your local services with others, 
test webhooks, or access your local development environment from anywhere.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Initialize the agent and start it
		tunnelwayagent.Init(port)
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

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.tunnelway-agent.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().Int16VarP(&port, "port", "p", 8080, "The local port to forward traffic to")
}
