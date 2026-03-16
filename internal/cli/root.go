package cli

import (
	"github.com/spf13/cobra"
	"github.com/nlook-service/nlook-router/internal/config"
)

var configPath string

var rootCmd = &cobra.Command{
	Use:   "nlook-router",
	Short: "nlook local router and CLI",
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&JSONOutput, "json", false, "output as JSON")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "config file path (default ~/.nlook/config.yaml)")
}

// Root returns the root command.
func Root() *cobra.Command {
	return rootCmd
}

// GetConfigPath returns the effective config path.
func GetConfigPath() string {
	if configPath != "" {
		return configPath
	}
	return config.ConfigPath()
}
