package cli

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/nlook-service/nlook-router/internal/config"
	"github.com/nlook-service/nlook-router/internal/server"
)

var routerCmd = &cobra.Command{
	Use:   "router",
	Short: "router daemon control",
}

var routerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "start the router daemon",
	RunE:  runRouterStart,
}

var routerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "show router status",
	RunE:  runRouterStatus,
}

func init() {
	routerCmd.AddCommand(routerStartCmd, routerStatusCmd)
	Root().AddCommand(routerCmd)
}

func runRouterStart(cmd *cobra.Command, args []string) error {
	path := GetConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	config.ApplyEnv(cfg)
	return RunDaemon(cfg)
}

func runRouterStatus(cmd *cobra.Command, args []string) error {
	path := GetConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	config.ApplyEnv(cfg)
	addr := fmt.Sprintf("http://127.0.0.1:%d/status", cfg.Port)
	resp, err := http.Get(addr)
	if err != nil {
		if JSONOutput {
			return PrintJSON(map[string]interface{}{"running": false, "error": err.Error()})
		}
		fmt.Println("router not running:", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if JSONOutput {
			return PrintJSON(map[string]interface{}{"running": true, "status_code": resp.StatusCode})
		}
		fmt.Printf("router returned %d\n", resp.StatusCode)
		return nil
	}
	var status server.Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return err
	}
	if JSONOutput {
		return PrintJSON(map[string]interface{}{"running": true, "status": status})
	}
	fmt.Printf("router_id: %s\nconnected: %v\n", status.RouterID, status.Connected)
	return nil
}
