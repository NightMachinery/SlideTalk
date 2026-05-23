// Package config loads SlideTalk runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddr                = "127.0.0.1:8097"
	defaultDir                 = ".slidetalk"
	defaultSlideMaxBytes       = 200 * 1024 * 1024
	defaultAudioMaxBytes       = 50 * 1024 * 1024
	defaultAudioFilesGCAfter   = 7 * 24 * time.Hour
	defaultRoomGCAfter         = 7 * 24 * time.Hour
	defaultAudioDriftThreshold = 3 * time.Second
	defaultMinFreeSpaceBytes   = 512 * 1024 * 1024
)

// Config contains process-level settings for the SlideTalk server.
type Config struct {
	DataDir             string
	Addr                string
	PublicURL           string
	DevMode             bool
	SlideMaxBytes       int64
	AudioMaxBytes       int64
	AudioFilesGCAfter   time.Duration
	RoomGCAfter         time.Duration
	AudioDriftThreshold time.Duration
	MinFreeSpaceBytes   int64
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

	maxBytes := int64(defaultSlideMaxBytes)
	if raw := os.Getenv("SLIDETALK_SLIDE_UPLOAD_LIMIT"); raw != "" {
		parsed, err := parseBytes(raw)
		if err != nil {
			return Config{}, err
		}
		maxBytes = parsed
	}
	audioMaxBytes := int64(defaultAudioMaxBytes)
	if raw := os.Getenv("SLIDETALK_AUDIO_FILE_UPLOAD_LIMIT"); raw != "" {
		parsed, err := parseBytes(raw)
		if err != nil {
			return Config{}, err
		}
		audioMaxBytes = parsed
	}
	gcAfter := defaultAudioFilesGCAfter
	if raw := os.Getenv("SLIDETALK_AUDIO_FILES_GC_AFTER"); raw != "" {
		parsed, err := parseDuration(raw)
		if err != nil {
			return Config{}, err
		}
		gcAfter = parsed
	}
	roomGCAfter := defaultRoomGCAfter
	if raw := os.Getenv("SLIDETALK_ROOM_GC_AFTER"); raw != "" {
		parsed, err := parseDuration(raw)
		if err != nil {
			return Config{}, err
		}
		roomGCAfter = parsed
	} else if raw := os.Getenv("SLIDETALK_AUDIO_FILES_GC_AFTER"); raw != "" {
		roomGCAfter = gcAfter
	}
	audioDriftThreshold := defaultAudioDriftThreshold
	if raw := os.Getenv("SLIDETALK_AUDIO_DRIFT_THRESHOLD"); raw != "" {
		parsed, err := parseDuration(raw)
		if err != nil {
			return Config{}, err
		}
		audioDriftThreshold = parsed
	}
	minFree := int64(defaultMinFreeSpaceBytes)
	if raw := os.Getenv("SLIDETALK_MIN_FREE_SPACE"); raw != "" {
		parsed, err := parseBytes(raw)
		if err != nil {
			return Config{}, err
		}
		minFree = parsed
	}

	return Config{
		DataDir:             dataDir,
		Addr:                addr,
		PublicURL:           os.Getenv("SLIDETALK_PUBLIC_URL"),
		DevMode:             os.Getenv("SLIDETALK_DEV") == "1",
		SlideMaxBytes:       maxBytes,
		AudioMaxBytes:       audioMaxBytes,
		AudioFilesGCAfter:   gcAfter,
		RoomGCAfter:         roomGCAfter,
		AudioDriftThreshold: audioDriftThreshold,
		MinFreeSpaceBytes:   minFree,
	}, nil
}

func parseDuration(raw string) (time.Duration, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if strings.HasSuffix(value, "d") {
		days, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "d")), 64)
		if err != nil {
			return 0, fmt.Errorf("parse duration %q: %w", raw, err)
		}
		if days <= 0 {
			return 0, fmt.Errorf("duration %q must be positive", raw)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", raw)
	}
	return parsed, nil
}

func parseBytes(raw string) (int64, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	units := map[string]int64{
		"kb": 1024,
		"k":  1024,
		"mb": 1024 * 1024,
		"m":  1024 * 1024,
		"gb": 1024 * 1024 * 1024,
		"g":  1024 * 1024 * 1024,
	}
	multiplier := int64(1)
	for suffix, unit := range units {
		if strings.HasSuffix(value, suffix) {
			value = strings.TrimSpace(strings.TrimSuffix(value, suffix))
			multiplier = unit
			break
		}
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse byte size %q: %w", raw, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("byte size %q must be positive", raw)
	}
	return int64(parsed * float64(multiplier)), nil
}
