package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/nlook-service/nlook-router/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "manage config",
}

var configSetCmd = &cobra.Command{
	Use:   "set [KEY] [VALUE]",
	Short: "set config key",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:   "get [KEY]",
	Short: "get config key",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigGet,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "list config (masked)",
	RunE:  runConfigList,
}

func init() {
	configCmd.AddCommand(configSetCmd, configGetCmd, configListCmd)
	Root().AddCommand(configCmd)
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	path := GetConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	key, val := args[0], args[1]
	switch key {
	case "NLOOK_API_KEY", "api_key":
		cfg.APIKey = val
	case "NLOOK_API_URL", "api_url":
		cfg.APIURL = val
	case "router_id":
		cfg.RouterID = val
	default:
		return fmt.Errorf("unknown key: %s", key)
	}
	if err := cfg.Save(path); err != nil {
		return err
	}
	if !JSONOutput {
		fmt.Fprintf(os.Stderr, "Saved %s to config\n", key)
	}
	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	path := GetConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	config.ApplyEnv(cfg)
	key := args[0]
	var val string
	switch key {
	case "NLOOK_API_KEY", "api_key":
		val = cfg.APIKey
	case "NLOOK_API_URL", "api_url":
		val = cfg.APIURL
	case "router_id":
		val = cfg.RouterID
	default:
		return fmt.Errorf("unknown key: %s", key)
	}
	if JSONOutput {
		return PrintJSON(map[string]string{key: val})
	}
	fmt.Println(val)
	return nil
}

func runConfigList(cmd *cobra.Command, args []string) error {
	path := GetConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	config.ApplyEnv(cfg)
	m := map[string]string{
		"api_url":  cfg.APIURL,
		"api_key":  mask(cfg.APIKey),
		"router_id": cfg.RouterID,
	}
	return PrintJSON(m)
}

func mask(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}
