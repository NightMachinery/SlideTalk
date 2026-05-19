// Package config loads SlideTalk runtime configuration from environment variables.
package config

import (
	"os"
	"path/filepath"
)

const (
	defaultAddr = "127.0.0.1:8097"
	defaultDir  = ".slidetalk"
)

// Config contains process-level settings for the SlideTalk server.
type Config struct {
	DataDir   string
	Addr      string
	PublicURL string
	DevMode   bool
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	dataDir := os.Getenv("SLIDETALK_DATA_DIR")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, err
		}
		dataDir = filepath.Join(home, defaultDir)
	}

	addr := os.Getenv("SLIDETALK_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	return Config{
		DataDir:   dataDir,
		Addr:      addr,
		PublicURL: os.Getenv("SLIDETALK_PUBLIC_URL"),
		DevMode:   os.Getenv("SLIDETALK_DEV") == "1",
	}, nil
}
