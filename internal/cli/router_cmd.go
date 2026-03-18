package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

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

var routerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "stop running router",
	RunE:  runRouterStop,
}

var routerRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "restart the router (stop + start)",
	RunE:  runRouterRestart,
}

func init() {
	routerCmd.AddCommand(routerStartCmd, routerStatusCmd, routerStopCmd, routerRestartCmd)
	Root().AddCommand(routerCmd)
}

func runRouterStart(cmd *cobra.Command, args []string) error {
	path := GetConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	config.ApplyEnv(cfg)

	// Check if port is already in use and offer to kill
	if pid := findProcessOnPort(cfg.Port); pid > 0 {
		fmt.Printf("⚠️  Port %d is in use (PID %d). Stopping existing process...\n", cfg.Port, pid)
		killProcess(pid)
		fmt.Println("✅ Stopped. Starting new instance...")
	}

	return RunDaemon(cfg)
}

func runRouterStop(cmd *cobra.Command, args []string) error {
	path := GetConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	config.ApplyEnv(cfg)

	pid := findProcessOnPort(cfg.Port)
	if pid <= 0 {
		fmt.Println("❌ Router is not running")
		return nil
	}
	killProcess(pid)
	fmt.Printf("✅ Router stopped (PID %d)\n", pid)
	return nil
}

func runRouterRestart(cmd *cobra.Command, args []string) error {
	path := GetConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	config.ApplyEnv(cfg)

	if pid := findProcessOnPort(cfg.Port); pid > 0 {
		fmt.Printf("🔄 Stopping router (PID %d)...\n", pid)
		killProcess(pid)
	}

	fmt.Println("🚀 Starting router...")
	return RunDaemon(cfg)
}

// findProcessOnPort returns the PID using the given port, or 0 if none.
func findProcessOnPort(port int) int {
	switch runtime.GOOS {
	case "darwin", "linux":
		out, err := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port)).Output()
		if err != nil || len(out) == 0 {
			return 0
		}
		lines := strings.TrimSpace(string(out))
		pid, err := strconv.Atoi(strings.Split(lines, "\n")[0])
		if err != nil {
			return 0
		}
		return pid
	case "windows":
		out, err := exec.Command("netstat", "-ano").Output()
		if err != nil {
			return 0
		}
		portStr := fmt.Sprintf(":%d", port)
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, portStr) && strings.Contains(line, "LISTENING") {
				fields := strings.Fields(line)
				if len(fields) >= 5 {
					pid, _ := strconv.Atoi(fields[len(fields)-1])
					return pid
				}
			}
		}
		return 0
	}
	return 0
}

// killProcess terminates a process by PID.
func killProcess(pid int) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = p.Signal(os.Interrupt)
	// Wait briefly then force kill
	time.Sleep(2 * time.Second)
	_ = p.Kill()
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

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "show nlook-router version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("nlook-router v%s\n", Version)
	},
}

func init() {
	Root().AddCommand(versionCmd)
}
