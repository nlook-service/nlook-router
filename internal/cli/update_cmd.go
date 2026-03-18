package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

const (
	repoOwner = "nlook-service"
	repoName  = "nlook-router"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func init() {
	rootCmd.AddCommand(selfUpdateCmd)
}

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "update nlook-router to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSelfUpdate()
	},
}

// CheckForUpdate checks GitHub for a newer version.
// If a new version is found, automatically downloads and replaces the binary,
// then prompts the user to restart.
func CheckForUpdate() {
	go func() {
		release, err := getLatestRelease()
		if err != nil || release == nil {
			return
		}
		latest := release.TagName
		current := "v" + Version
		if latest == current || latest <= current {
			return
		}

		log.Printf("📦 New version available: %s (current: %s). Auto-updating...", latest, current)

		// Find the right asset
		assetName := getBinaryAssetName()
		var downloadURL string
		for _, asset := range release.Assets {
			if asset.Name == assetName {
				downloadURL = asset.BrowserDownloadURL
				break
			}
		}
		if downloadURL == "" {
			log.Printf("⚠️  No binary found for %s in release %s. Run: nlook-router self-update", assetName, latest)
			return
		}

		// Download to temp file
		tmpFile, err := os.CreateTemp("", "nlook-router-update-*")
		if err != nil {
			log.Printf("⚠️  Auto-update failed: %v", err)
			return
		}
		defer os.Remove(tmpFile.Name())

		if err := downloadFile(downloadURL, tmpFile); err != nil {
			tmpFile.Close()
			log.Printf("⚠️  Auto-update download failed: %v", err)
			return
		}
		tmpFile.Close()

		// Replace binary
		execPath, err := os.Executable()
		if err != nil {
			home, _ := os.UserHomeDir()
			execPath = filepath.Join(home, ".nlook", "bin", "nlook-router")
		}
		execPath, _ = filepath.EvalSymlinks(execPath)

		if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
			log.Printf("⚠️  Auto-update chmod failed: %v", err)
			return
		}

		if err := os.Rename(tmpFile.Name(), execPath); err != nil {
			if err := copyFile(tmpFile.Name(), execPath); err != nil {
				log.Printf("⚠️  Auto-update replace failed: %v. Run: nlook-router self-update", err)
				return
			}
		}

		log.Printf("✅ Auto-updated to %s. Restart the router to use the new version.", latest)
	}()
}

func runSelfUpdate() error {
	fmt.Printf("🔍 Checking for updates (current: v%s)...\n", Version)

	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("check latest version: %w", err)
	}

	latest := release.TagName
	current := "v" + Version
	if latest == current {
		fmt.Printf("✅ Already up to date (v%s)\n", Version)
		return nil
	}

	fmt.Printf("📦 Updating: %s → %s\n", current, latest)

	// Find the right asset
	assetName := getBinaryAssetName()
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s in release %s", assetName, latest)
	}

	// Download to temp file
	fmt.Printf("⬇️  Downloading %s...\n", assetName)
	tmpFile, err := os.CreateTemp("", "nlook-router-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if err := downloadFile(downloadURL, tmpFile); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	tmpFile.Close()

	// Replace current binary
	execPath, err := os.Executable()
	if err != nil {
		// Fallback to ~/.nlook/bin/
		home, _ := os.UserHomeDir()
		execPath = filepath.Join(home, ".nlook", "bin", "nlook-router")
	}
	execPath, _ = filepath.EvalSymlinks(execPath)

	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpFile.Name(), execPath); err != nil {
		// Rename might fail across filesystems, try copy
		if err := copyFile(tmpFile.Name(), execPath); err != nil {
			return fmt.Errorf("replace binary: %w", err)
		}
	}

	fmt.Printf("✅ Updated to %s → %s\n", latest, execPath)
	return nil
}

func getLatestVersion() (string, error) {
	release, err := getLatestRelease()
	if err != nil {
		return "", err
	}
	return release.TagName, nil
}

func getLatestRelease() (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github api: %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func getBinaryAssetName() string {
	goos := runtime.GOOS
	if goos == "darwin" {
		goos = "darwin"
	}
	goarch := runtime.GOARCH
	if goarch == "amd64" {
		goarch = "amd64"
	}
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("nlook-router-%s-%s%s", goos, goarch, ext)
}

func downloadFile(url string, dest *os.File) error {
	client := &http.Client{Timeout: 120 * time.Second}

	// Follow redirects manually for GitHub releases
	var finalURL string
	for i := 0; i < 5; i++ {
		resp, err := client.Head(url)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode == 302 || resp.StatusCode == 301 {
			url = resp.Header.Get("Location")
			continue
		}
		finalURL = url
		break
	}
	if finalURL == "" {
		finalURL = url
	}

	resp, err := client.Get(finalURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: %d", resp.StatusCode)
	}

	_, err = io.Copy(dest, resp.Body)
	return err
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}

