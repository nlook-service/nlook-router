package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_NoFile_ReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIURL != Default().APIURL {
		t.Errorf("APIURL want %s got %s", Default().APIURL, cfg.APIURL)
	}
	if cfg.Port != Default().Port {
		t.Errorf("Port want %d got %d", Default().Port, cfg.Port)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := Default()
	cfg.APIKey = "test-key"
	cfg.APIURL = "https://example.com"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.APIKey != cfg.APIKey || loaded.APIURL != cfg.APIURL {
		t.Errorf("loaded config mismatch: got %+v", loaded)
	}
}

func TestApplyEnv(t *testing.T) {
	cfg := Default()
	cfg.APIKey = "original"
	os.Setenv(EnvAPIKey, "env-key")
	defer os.Unsetenv(EnvAPIKey)
	ApplyEnv(cfg)
	if cfg.APIKey != "env-key" {
		t.Errorf("ApplyEnv: want api_key env-key got %s", cfg.APIKey)
	}
}
