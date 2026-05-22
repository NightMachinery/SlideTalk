package audio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/auth"
	"github.com/NightMachinery/SlideTalk/internal/rooms"
	"github.com/NightMachinery/SlideTalk/internal/store"
)

func TestStoreRejectsNonAudioMIME(t *testing.T) {
	ctx := context.Background()
	fixture := newAudioFixture(t)
	content := []byte("not audio")

	_, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sha256Hex(content),
		OriginalName: "clip.txt",
		MIMEType:     "text/plain",
		File:         bytes.NewReader(content),
		IsAdmin:      true,
	})

	if !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("expected unsupported file error, got %v", err)
	}
}

func TestStoreAppliesLimitOnlyToNonAdmins(t *testing.T) {
	ctx := context.Background()
	fixture := newAudioFixture(t)
	content := []byte("ID3oversized audio")

	_, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sha256Hex(content),
		OriginalName: "big.mp3",
		MIMEType:     "audio/mpeg",
		File:         bytes.NewReader(content),
		IsAdmin:      false,
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("non-admin error = %v, want too large", err)
	}

	_, err = fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sha256Hex(content),
		OriginalName: "big.mp3",
		MIMEType:     "audio/mpeg",
		File:         bytes.NewReader(content),
		IsAdmin:      true,
	})
	if err != nil {
		t.Fatalf("admin upload should bypass per-file limit: %v", err)
	}
}

func TestStoreRejectsWhenDiskFloorWouldBeViolated(t *testing.T) {
	ctx := context.Background()
	fixture := newAudioFixture(t)
	fixture.service.freeSpace = func(string) (int64, error) { return 15, nil }
	content := []byte("ID3audio")

	_, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sha256Hex(content),
		OriginalName: "clip.mp3",
		MIMEType:     "audio/mpeg",
		File:         bytes.NewReader(content),
		IsAdmin:      true,
	})

	if !errors.Is(err, ErrInsufficientFreeSpace) {
		t.Fatalf("error = %v, want insufficient free space", err)
	}
}

func TestCleanupRemovesTracksAWeekAfterRoomCreation(t *testing.T) {
	ctx := context.Background()
	fixture := newAudioFixture(t)
	content := []byte("ID3old audio")
	sum := sha256Hex(content)
	if _, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sum,
		OriginalName: "old.mp3",
		MIMEType:     "audio/mpeg",
		File:         bytes.NewReader(content),
		IsAdmin:      true,
	}); err != nil {
		t.Fatalf("store audio: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `update rooms set created_at = ? where id = ?`, time.Now().UTC().Add(-8*24*time.Hour).Format(time.RFC3339Nano), fixture.room.ID); err != nil {
		t.Fatalf("age room: %v", err)
	}

	if err := fixture.service.Cleanup(ctx, time.Now().UTC(), 7*24*time.Hour); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(fixture.audioDir, sum+".mp3")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected audio file deleted, got %v", err)
	}
}

type audioFixture struct {
	db       *store.DB
	rooms    *rooms.Service
	service  *Service
	admin    auth.User
	room     rooms.Room
	audioDir string
}

func newAudioFixture(t *testing.T) audioFixture {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(ctx, db.DB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	authService := auth.NewService(db, dataDir)
	admin, err := authService.EnsureUser(ctx, "admin-token")
	if err != nil {
		t.Fatalf("ensure user: %v", err)
	}
	if err := authService.UpdateDisplayName(ctx, admin.ID, "Ada"); err != nil {
		t.Fatalf("update display name: %v", err)
	}
	roomService := rooms.NewService(db)
	room, err := roomService.Create(ctx, admin.ID, rooms.CreateInput{Title: "Audio room"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	audioDir := filepath.Join(dataDir, "audio")
	service, err := NewService(db, audioDir, 8, 10)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return audioFixture{db: db, rooms: roomService, service: service, admin: admin, room: room, audioDir: audioDir}
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
