package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadReadsSelfHostingEnvironment(t *testing.T) {
	t.Setenv("SLIDETALK_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("SLIDETALK_ADDR", "127.0.0.1:18097")
	t.Setenv("SLIDETALK_PUBLIC_URL", "https://talk.example.test")
	t.Setenv("SLIDETALK_DEV", "1")
	t.Setenv("SLIDETALK_SLIDE_UPLOAD_LIMIT", "12345")
	t.Setenv("SLIDETALK_AUDIO_FILE_UPLOAD_LIMIT", "12m")
	t.Setenv("SLIDETALK_AUDIO_FILES_GC_AFTER", "48h")
	t.Setenv("SLIDETALK_MIN_FREE_SPACE", "1GB")

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
	if cfg.AudioMaxBytes != 12*1024*1024 {
		t.Fatalf("AudioMaxBytes = %d, want 12 MiB", cfg.AudioMaxBytes)
	}
	if cfg.AudioFilesGCAfter != 48*time.Hour {
		t.Fatalf("AudioFilesGCAfter = %s, want 48h", cfg.AudioFilesGCAfter)
	}
	if cfg.MinFreeSpaceBytes != 1024*1024*1024 {
		t.Fatalf("MinFreeSpaceBytes = %d, want 1 GiB", cfg.MinFreeSpaceBytes)
	}
}

func TestLoadFallsBackToDeprecatedSlideMaxBytes(t *testing.T) {
	t.Setenv("SLIDETALK_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("SLIDETALK_SLIDE_MAX_BYTES", "54321")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.SlideMaxBytes != 54321 {
		t.Fatalf("SlideMaxBytes = %d, want deprecated fallback", cfg.SlideMaxBytes)
	}
}

func TestLoadPrefersNewSlideUploadLimit(t *testing.T) {
	t.Setenv("SLIDETALK_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("SLIDETALK_SLIDE_MAX_BYTES", "111")
	t.Setenv("SLIDETALK_SLIDE_UPLOAD_LIMIT", "222")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.SlideMaxBytes != 222 {
		t.Fatalf("SlideMaxBytes = %d, want new env var", cfg.SlideMaxBytes)
	}
}

func TestLoadParsesDayDurationForAudioGC(t *testing.T) {
	t.Setenv("SLIDETALK_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("SLIDETALK_AUDIO_FILES_GC_AFTER", "7d")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.AudioFilesGCAfter != 7*24*time.Hour {
		t.Fatalf("AudioFilesGCAfter = %s, want 7 days", cfg.AudioFilesGCAfter)
	}
}
