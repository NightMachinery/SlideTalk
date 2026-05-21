package config

import (
	"path/filepath"
	"testing"
)

func TestLoadReadsSelfHostingEnvironment(t *testing.T) {
	t.Setenv("SLIDETALK_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("SLIDETALK_ADDR", "127.0.0.1:18097")
	t.Setenv("SLIDETALK_PUBLIC_URL", "https://talk.example.test")
	t.Setenv("SLIDETALK_DEV", "1")
	t.Setenv("SLIDETALK_SLIDE_MAX_BYTES", "12345")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.DataDir == "" {
		t.Fatal("DataDir is empty")
	}
	if cfg.Addr != "127.0.0.1:18097" {
		t.Fatalf("Addr = %q, want custom address", cfg.Addr)
	}
	if cfg.PublicURL != "https://talk.example.test" {
		t.Fatalf("PublicURL = %q, want configured public URL", cfg.PublicURL)
	}
	if !cfg.DevMode {
		t.Fatal("DevMode = false, want true")
	}
	if cfg.SlideMaxBytes != 12345 {
		t.Fatalf("SlideMaxBytes = %d, want 12345", cfg.SlideMaxBytes)
	}
}
