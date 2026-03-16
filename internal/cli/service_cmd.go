package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "manage system service (auto-start on boot)",
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "install nlook-router as a system service",
	RunE:  runServiceInstall,
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "uninstall nlook-router system service",
	RunE:  runServiceUninstall,
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "check service status",
	RunE:  runServiceStatus,
}

func init() {
	serviceCmd.AddCommand(serviceInstallCmd, serviceUninstallCmd, serviceStatusCmd)
	Root().AddCommand(serviceCmd)
}

func runServiceInstall(cmd *cobra.Command, args []string) error {
	binPath, err := getInstalledBinaryPath()
	if err != nil {
		return err
	}
	configPath := GetConfigPath()

	switch runtime.GOOS {
	case "darwin":
		return installMacOSService(binPath, configPath)
	case "linux":
		return installLinuxService(binPath, configPath)
	case "windows":
		return installWindowsService(binPath, configPath)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func runServiceUninstall(cmd *cobra.Command, args []string) error {
	switch runtime.GOOS {
	case "darwin":
		return uninstallMacOSService()
	case "linux":
		return uninstallLinuxService()
	case "windows":
		return uninstallWindowsService()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func runServiceStatus(cmd *cobra.Command, args []string) error {
	switch runtime.GOOS {
	case "darwin":
		return statusMacOSService()
	case "linux":
		return statusLinuxService()
	case "windows":
		return statusWindowsService()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func getInstalledBinaryPath() (string, error) {
	// Try current executable
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		return exe, nil
	}
	// Fallback to ~/.nlook/bin
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".nlook", "bin", "nlook-router")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("nlook-router binary not found")
}

// ─── macOS (launchd) ───

const macPlistName = "me.nlook.router"

func macPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", macPlistName+".plist")
}

func installMacOSService(binPath, configPath string) error {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".nlook", "logs")
	os.MkdirAll(logDir, 0755)

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>router</string>
        <string>start</string>
        <string>--config</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/nlook-router.log</string>
    <key>StandardErrorPath</key>
    <string>%s/nlook-router.err</string>
    <key>ThrottleInterval</key>
    <integer>10</integer>
</dict>
</plist>`, macPlistName, binPath, configPath, logDir, logDir)

	path := macPlistPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	fmt.Println("✅ Service installed and started")
	fmt.Printf("   Plist: %s\n", path)
	fmt.Printf("   Logs:  %s/nlook-router.log\n", logDir)
	fmt.Println("   The router will auto-start on login.")
	return nil
}

func uninstallMacOSService() error {
	path := macPlistPath()
	exec.Command("launchctl", "unload", path).Run()
	os.Remove(path)
	fmt.Println("✅ Service uninstalled")
	return nil
}

func statusMacOSService() error {
	out, err := exec.Command("launchctl", "list", macPlistName).CombinedOutput()
	if err != nil {
		fmt.Println("❌ Service not running")
		return nil
	}
	fmt.Printf("✅ Service registered\n%s\n", strings.TrimSpace(string(out)))
	return nil
}

// ─── Linux (systemd) ───

const systemdServiceName = "nlook-router"

func systemdServicePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", systemdServiceName+".service")
}

func installLinuxService(binPath, configPath string) error {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".nlook", "logs")
	os.MkdirAll(logDir, 0755)

	unit := fmt.Sprintf(`[Unit]
Description=nlook local router
After=network.target

[Service]
Type=simple
ExecStart=%s router start --config %s
Restart=always
RestartSec=10
Environment=HOME=%s

[Install]
WantedBy=default.target
`, binPath, configPath, home)

	path := systemdServicePath()
	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}

	cmds := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", systemdServiceName},
		{"systemctl", "--user", "start", systemdServiceName},
	}
	for _, c := range cmds {
		if err := exec.Command(c[0], c[1:]...).Run(); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(c, " "), err)
		}
	}

	fmt.Println("✅ Service installed and started")
	fmt.Printf("   Unit: %s\n", path)
	fmt.Println("   The router will auto-start on login.")
	fmt.Println("   View logs: journalctl --user -u nlook-router -f")
	return nil
}

func uninstallLinuxService() error {
	exec.Command("systemctl", "--user", "stop", systemdServiceName).Run()
	exec.Command("systemctl", "--user", "disable", systemdServiceName).Run()
	os.Remove(systemdServicePath())
	exec.Command("systemctl", "--user", "daemon-reload").Run()
	fmt.Println("✅ Service uninstalled")
	return nil
}

func statusLinuxService() error {
	out, err := exec.Command("systemctl", "--user", "status", systemdServiceName).CombinedOutput()
	if err != nil {
		fmt.Printf("❌ Service not running\n%s\n", strings.TrimSpace(string(out)))
		return nil
	}
	fmt.Printf("✅ Service running\n%s\n", strings.TrimSpace(string(out)))
	return nil
}

// ─── Windows (Task Scheduler) ───

const windowsTaskName = "NlookRouter"

func installWindowsService(binPath, configPath string) error {
	// Use schtasks to create a task that runs at logon
	args := []string{
		"/Create",
		"/TN", windowsTaskName,
		"/TR", fmt.Sprintf(`"%s" router start --config "%s"`, binPath, configPath),
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/F",
	}

	if err := exec.Command("schtasks", args...).Run(); err != nil {
		return fmt.Errorf("schtasks create: %w", err)
	}

	// Start immediately
	exec.Command("schtasks", "/Run", "/TN", windowsTaskName).Run()

	fmt.Println("✅ Service installed and started")
	fmt.Printf("   Task: %s\n", windowsTaskName)
	fmt.Println("   The router will auto-start on login.")
	return nil
}

func uninstallWindowsService() error {
	exec.Command("schtasks", "/End", "/TN", windowsTaskName).Run()
	exec.Command("schtasks", "/Delete", "/TN", windowsTaskName, "/F").Run()
	fmt.Println("✅ Service uninstalled")
	return nil
}

func statusWindowsService() error {
	out, err := exec.Command("schtasks", "/Query", "/TN", windowsTaskName).CombinedOutput()
	if err != nil {
		fmt.Println("❌ Service not registered")
		return nil
	}
	fmt.Printf("✅ Service registered\n%s\n", strings.TrimSpace(string(out)))
	return nil
}
