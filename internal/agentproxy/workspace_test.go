package agentproxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateWorkspace(t *testing.T) {
	// Create temp dirs for testing
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed-repo")
	os.MkdirAll(filepath.Join(allowedDir, ".git"), 0755)

	outsideDir := filepath.Join(tmpDir, "outside-repo")
	os.MkdirAll(filepath.Join(outsideDir, ".git"), 0755)

	noGitDir := filepath.Join(tmpDir, "no-git")
	os.MkdirAll(noGitDir, 0755)

	allowed := []string{allowedDir}

	tests := []struct {
		name      string
		requested string
		wantErr   bool
	}{
		{"allowed path", allowedDir, false},
		{"outside path", outsideDir, true},
		{"no .git dir", noGitDir, true},
		{"nonexistent path", filepath.Join(tmpDir, "missing"), true},
		{"empty allowed list fails", allowedDir, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := allowed
			if tt.name == "empty allowed list fails" {
				list = nil
			}
			_, err := ValidateWorkspace(tt.requested, list)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWorkspace(%q) error = %v, wantErr %v", tt.requested, err, tt.wantErr)
			}
		})
	}
}

func TestValidateWorkspace_Symlink(t *testing.T) {
	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real-repo")
	os.MkdirAll(filepath.Join(realDir, ".git"), 0755)

	symlink := filepath.Join(tmpDir, "link-to-repo")
	if err := os.Symlink(realDir, symlink); err != nil {
		t.Skip("cannot create symlink")
	}

	// Resolve realDir too (macOS /var → /private/var)
	realDirResolved, _ := filepath.EvalSymlinks(realDir)

	// Symlink to allowed dir should pass
	resolved, err := ValidateWorkspace(symlink, []string{realDir})
	if err != nil {
		t.Fatalf("expected symlink to allowed dir to pass: %v", err)
	}
	if resolved != realDirResolved {
		t.Errorf("expected resolved=%s, got %s", realDirResolved, resolved)
	}

	// Symlink outside allowed list should fail
	otherDir := filepath.Join(tmpDir, "other-repo")
	os.MkdirAll(filepath.Join(otherDir, ".git"), 0755)
	outsideLink := filepath.Join(tmpDir, "link-outside")
	os.Symlink(otherDir, outsideLink)

	_, err = ValidateWorkspace(outsideLink, []string{realDir})
	if err == nil {
		t.Error("expected symlink outside allowed dirs to fail")
	}
}
