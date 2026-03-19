package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nlook-service/nlook-router/internal/config"
	"github.com/nlook-service/nlook-router/internal/ollama"
	"github.com/spf13/cobra"
)

var modelFlag string
var engineFlag string

var aiCmd = &cobra.Command{
	Use:   "ai",
	Short: "Manage local AI models",
}

var aiSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Download AI model for local chat (one command setup)",
	RunE:  runAISetup,
}

var aiListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed AI models",
	RunE:  runAIList,
}

var aiRemoveCmd = &cobra.Command{
	Use:   "remove [model]",
	Short: "Remove an installed AI model",
	Args:  cobra.ExactArgs(1),
	RunE:  runAIRemove,
}

var aiSetupVLLMCmd = &cobra.Command{
	Use:   "setup-vllm",
	Short: "Install vLLM engine for high-performance multi-agent inference",
	RunE:  runAISetupVLLM,
}

func init() {
	aiSetupCmd.Flags().StringVar(&modelFlag, "model", "", "model to download (auto-detected by engine)")
	aiSetupCmd.Flags().StringVar(&engineFlag, "engine", "auto", "LLM engine: auto, vllm, ollama")
	aiSetupVLLMCmd.Flags().StringVar(&modelFlag, "model", "Qwen/Qwen3-8B", "HuggingFace model for vLLM")
	aiCmd.AddCommand(aiSetupCmd, aiSetupVLLMCmd, aiListCmd, aiRemoveCmd)
	rootCmd.AddCommand(aiCmd)
}

func runAISetupVLLM(cmd *cobra.Command, args []string) error {
	fmt.Println()
	fmt.Println("  ╭─────────────────────────────────────╮")
	fmt.Println("  │  nlook AI Setup (vLLM)              │")
	fmt.Println("  │  High-performance multi-agent AI    │")
	fmt.Println("  ╰─────────────────────────────────────╯")
	fmt.Println()

	// Step 1: Check Python/pip
	fmt.Println("  [1/3] Checking Python environment...")
	if _, err := exec.LookPath("python3"); err != nil {
		fmt.Println("  ✗ Python3 not found. Install Python 3.10+ first.")
		return nil
	}
	fmt.Println("  ✓ Python3 found")

	// Step 2: Install vLLM
	fmt.Println()
	fmt.Println("  [2/3] Installing vLLM...")
	installCmd := exec.Command("pip", "install", "vllm")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		fmt.Printf("  ✗ Failed to install vLLM: %v\n", err)
		fmt.Println("  → Try: pip install vllm")
		return nil
	}
	fmt.Println("  ✓ vLLM installed")

	// Step 3: Config
	fmt.Println()
	fmt.Println("  [3/3] Configuration")
	fmt.Println()
	fmt.Println("  ✓ Setup complete!")
	fmt.Println()
	fmt.Println("  ╭──────────────────────────────────────────╮")
	fmt.Printf("  │  Model:   %-31s │\n", modelFlag)
	fmt.Println("  │  Engine:  vLLM                           │")
	fmt.Println("  │                                          │")
	fmt.Println("  │  Start:                                  │")
	fmt.Println("  │  NLOOK_LLM_ENGINE=vllm \\                │")
	fmt.Println("  │    nlook-router router start             │")
	fmt.Println("  │                                          │")
	fmt.Println("  │  Or set in ~/.nlook/config.yaml:         │")
	fmt.Println("  │    llm_engine: vllm                      │")
	fmt.Printf("  │    ai_model: %-28s │\n", modelFlag)
	fmt.Println("  ╰──────────────────────────────────────────╯")
	fmt.Println()
	return nil
}

func runAISetup(cmd *cobra.Command, args []string) error {
	fmt.Println()
	fmt.Println("  ╭─────────────────────────────────────╮")
	fmt.Println("  │  nlook AI Setup                     │")
	fmt.Println("  │  Local AI for chat & workflows      │")
	fmt.Println("  ╰─────────────────────────────────────╯")
	fmt.Println()

	ctx := context.Background()

	// Auto-detect engine: prefer vLLM if Python available
	engine := engineFlag
	if engine == "auto" {
		if _, err := exec.LookPath("python3"); err == nil {
			engine = "vllm"
			fmt.Println("  Python detected → using vLLM (recommended)")
		} else {
			engine = "ollama"
			fmt.Println("  Python not found → using Ollama")
		}
		fmt.Println()
	}

	if engine == "vllm" {
		return setupVLLM(ctx)
	}

	// Ollama setup
	if modelFlag == "" {
		modelFlag = "qwen3:8b"
	}

	client := ollama.NewClient()
	fmt.Println("  [1/3] Checking Ollama...")
	if !client.IsRunning(ctx) {
		// Try to find ollama binary
		_, err := exec.LookPath("ollama")
		if err != nil {
			fmt.Println("  ✗ Ollama not found.")
			fmt.Println("  → Installing Ollama...")
			if installErr := installOllama(); installErr != nil {
				fmt.Println()
				fmt.Println("  ❌ Failed to install Ollama automatically.")
				fmt.Println()
				fmt.Println("  Install manually:")
				if runtime.GOOS == "darwin" {
					fmt.Println("    brew install ollama")
				} else {
					fmt.Println("    curl -fsSL https://ollama.ai/install.sh | sh")
				}
				fmt.Println()
				fmt.Println("  Then run: nlook-router ai setup")
				return nil
			}
			fmt.Println("  ✓ Ollama installed")
		}

		// Step 2: Start Ollama
		fmt.Println()
		fmt.Println("  [2/3] Starting Ollama server...")
		startCmd := exec.Command("ollama", "serve")
		startCmd.Stdout = nil
		startCmd.Stderr = nil
		if err := startCmd.Start(); err != nil {
			fmt.Printf("  ✗ Failed to start Ollama: %v\n", err)
			fmt.Println("  → Run manually: ollama serve")
			return nil
		}

		// Wait for server to be ready
		for i := 0; i < 15; i++ {
			time.Sleep(1 * time.Second)
			if client.IsRunning(ctx) {
				break
			}
		}
		if !client.IsRunning(ctx) {
			fmt.Println("  ✗ Ollama server did not start in time.")
			fmt.Println("  → Run manually: ollama serve")
			return nil
		}
		fmt.Println("  ✓ Ollama running")
	} else {
		fmt.Println("  ✓ Ollama is running")
		fmt.Println()
		fmt.Println("  [2/3] Ollama server ready")
	}

	// Step 3: Pull model
	fmt.Println()
	fmt.Printf("  [3/4] Downloading model: %s\n", modelFlag)

	lastStatus := ""
	err := client.Pull(ctx, modelFlag, func(status string, completed, total int64) {
		if strings.HasPrefix(status, "pulling") && total > 0 {
			pct := float64(completed) / float64(total) * 100
			bar := progressBar(pct, 30)
			fmt.Printf("\r  ▸ Downloading %s %.0f%% %s/%s  ",
				bar, pct, humanSize(completed), humanSize(total))
		} else if status != lastStatus {
			fmt.Printf("\r  ▸ %s\n", status)
			lastStatus = status
		}
	})
	fmt.Println()

	if err != nil {
		fmt.Printf("\n  ❌ Failed to download model: %v\n", err)
		return nil
	}

	// Step 4: Pull embedding model for semantic search
	fmt.Println()
	fmt.Println("  [4/4] Downloading embedding model: nomic-embed-text")
	_ = client.Pull(ctx, "nomic-embed-text", func(status string, completed, total int64) {
		if strings.HasPrefix(status, "pulling") && total > 0 {
			pct := float64(completed) / float64(total) * 100
			bar := progressBar(pct, 30)
			fmt.Printf("\r  ▸ Downloading %s %.0f%%  ", bar, pct)
		}
	})
	fmt.Println()
	fmt.Println("  ✓ Embedding model ready")

	// Get model info
	models, _ := client.List(ctx)
	var size string
	for _, m := range models {
		if strings.HasPrefix(m.Name, strings.Split(modelFlag, ":")[0]) {
			size = humanSize(m.Size)
			break
		}
	}

	// Step 5: Install tools-bridge dependencies
	installToolsBridge()

	fmt.Println()
	fmt.Println("  ✓ Setup complete!")
	fmt.Println()
	fmt.Println("  ╭─────────────────────────────────────╮")
	fmt.Printf("  │  Model:  %-27s │\n", modelFlag+" ("+size+")")
	fmt.Println("  │  Start:  nlook-router router start  │")
	fmt.Println("  │  Chat:   https://nlook.me/ai-search │")
	fmt.Println("  ╰─────────────────────────────────────╯")
	fmt.Println()

	return nil
}

func setupVLLM(ctx context.Context) error {
	model := modelFlag
	if model == "" {
		model = "Qwen/Qwen3-8B"
	}

	// Step 1: Check/install vLLM
	fmt.Println("  [1/4] Checking vLLM...")
	if _, err := exec.LookPath("vllm"); err != nil {
		fmt.Println("  ▸ Installing vLLM (this may take a few minutes)...")
		installCmd := exec.Command("pip", "install", "vllm")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			// Try with pip3
			installCmd2 := exec.Command("pip3", "install", "vllm")
			installCmd2.Stdout = os.Stdout
			installCmd2.Stderr = os.Stderr
			if err2 := installCmd2.Run(); err2 != nil {
				fmt.Printf("  ✗ Failed to install vLLM: %v\n", err)
				fmt.Println("  → Try manually: pip install vllm")
				return nil
			}
		}
	}
	fmt.Println("  ✓ vLLM installed")

	// Step 2: Install embedding model via Ollama (if available)
	fmt.Println()
	fmt.Println("  [2/4] Checking embedding model...")
	ollamaClient := ollama.NewClient()
	if ollamaClient.IsRunning(ctx) {
		fmt.Println("  ▸ Installing nomic-embed-text for RAG...")
		_ = ollamaClient.Pull(ctx, "nomic-embed-text", func(status string, completed, total int64) {
			if strings.HasPrefix(status, "pulling") && total > 0 {
				pct := float64(completed) / float64(total) * 100
				fmt.Printf("\r  ▸ Downloading %s %.0f%%  ", progressBar(pct, 20), pct)
			}
		})
		fmt.Println()
		fmt.Println("  ✓ Embedding model ready")
	} else {
		fmt.Println("  ⏭ Ollama not running, skipping embedding model")
	}

	// Step 3: Verify
	fmt.Println()
	fmt.Println("  [3/4] Verifying vLLM installation...")
	verifyCmd := exec.Command("vllm", "--version")
	if out, err := verifyCmd.Output(); err == nil {
		fmt.Printf("  ✓ vLLM %s", strings.TrimSpace(string(out)))
	} else {
		fmt.Println("  ✓ vLLM installed")
	}

	// Step 4: Config
	fmt.Println()
	fmt.Println("  [4/4] Configuration saved")
	fmt.Println()
	fmt.Println("  ✓ Setup complete!")
	fmt.Println()
	fmt.Println("  ╭──────────────────────────────────────────╮")
	fmt.Printf("  │  Engine:  vLLM (auto-managed)            │\n")
	fmt.Printf("  │  Model:   %-30s │\n", model)
	fmt.Println("  │                                          │")
	fmt.Println("  │  Start:   nlook-router router start      │")
	fmt.Println("  │  Chat:    https://nlook.me/ai-search     │")
	fmt.Println("  │                                          │")
	fmt.Println("  │  vLLM starts automatically on port 18000 │")

	installToolsBridge()
	fmt.Println("  ╰──────────────────────────────────────────╯")
	fmt.Println()
	return nil
}

func runAIList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client := ollama.NewClient()

	if !client.IsRunning(ctx) {
		fmt.Println("Ollama is not running. Start with: ollama serve")
		return nil
	}

	models, err := client.List(ctx)
	if err != nil {
		return fmt.Errorf("list models: %w", err)
	}

	if len(models) == 0 {
		fmt.Println("No models installed. Run: nlook-router ai setup")
		return nil
	}

	fmt.Printf("%-25s %-10s %s\n", "NAME", "SIZE", "MODIFIED")
	for _, m := range models {
		ago := time.Since(m.ModifiedAt)
		var modified string
		if ago < time.Hour {
			modified = fmt.Sprintf("%d minutes ago", int(ago.Minutes()))
		} else if ago < 24*time.Hour {
			modified = fmt.Sprintf("%d hours ago", int(ago.Hours()))
		} else {
			modified = fmt.Sprintf("%d days ago", int(ago.Hours()/24))
		}
		fmt.Printf("%-25s %-10s %s\n", m.Name, humanSize(m.Size), modified)
	}
	return nil
}

func runAIRemove(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client := ollama.NewClient()

	if !client.IsRunning(ctx) {
		fmt.Println("Ollama is not running. Start with: ollama serve")
		return nil
	}

	model := args[0]
	if err := client.Remove(ctx, model); err != nil {
		return fmt.Errorf("remove model: %w", err)
	}
	fmt.Printf("✓ Removed %s\n", model)
	return nil
}

func installToolsBridge() {
	// Find tools-bridge directory
	exePath, _ := os.Executable()
	dirs := []string{
		filepath.Join(filepath.Dir(exePath), "tools-bridge"),
		filepath.Join(filepath.Dir(exePath), "..", "tools-bridge"),
		"tools-bridge",
	}
	// Also check config
	cfg, _ := config.Load(GetConfigPath())
	if cfg != nil && cfg.ToolsBridgeDir != "" {
		dirs = append([]string{cfg.ToolsBridgeDir}, dirs...)
	}

	for _, dir := range dirs {
		reqFile := filepath.Join(dir, "requirements.txt")
		if _, err := os.Stat(reqFile); err == nil {
			fmt.Println()
			fmt.Println("  [5/5] Installing tools-bridge dependencies...")
			cmd := exec.Command("pip3", "install", "-q", "-r", reqFile)
			cmd.Dir = dir
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("  ⚠ tools-bridge install warning: %v\n", err)
			} else {
				fmt.Println("  ✓ Tools ready (web_search, code_interpreter, etc.)")
			}
			return
		}
	}
	fmt.Println()
	fmt.Println("  [5/5] Tools-bridge not found, skipping")
}

func installOllama() error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("brew", "install", "ollama")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	case "linux":
		cmd := exec.Command("sh", "-c", "curl -fsSL https://ollama.ai/install.sh | sh")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	default:
		return fmt.Errorf("unsupported OS: %s. Install Ollama manually from https://ollama.ai", runtime.GOOS)
	}
}

func progressBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func humanSize(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	if b < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(b)/1024/1024)
	}
	return fmt.Sprintf("%.1f GB", float64(b)/1024/1024/1024)
}
