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

func TestStoreAcceptsM4AAudio(t *testing.T) {
	ctx := context.Background()
	fixture := newAudioFixture(t)
	content := m4aBytes()

	status, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sha256Hex(content),
		OriginalName: "song.m4a",
		MIMEType:     "audio/x-m4a",
		File:         bytes.NewReader(content),
		IsAdmin:      true,
	})
	if err != nil {
		t.Fatalf("store m4a: %v", err)
	}
	if status.MIMEType != "audio/mp4" {
		t.Fatalf("mime type = %q, want audio/mp4", status.MIMEType)
	}
}

func TestStoreAcceptsM4AWhenBrowserSendsOctetStream(t *testing.T) {
	ctx := context.Background()
	fixture := newAudioFixture(t)
	content := m4aBytes()

	if _, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sha256Hex(content),
		OriginalName: "song.m4a",
		MIMEType:     "application/octet-stream",
		File:         bytes.NewReader(content),
		IsAdmin:      true,
	}); err != nil {
		t.Fatalf("store octet-stream m4a: %v", err)
	}
}

func TestStoreRejectsVideoMP4WithoutAudioExtension(t *testing.T) {
	ctx := context.Background()
	fixture := newAudioFixture(t)
	content := m4aBytes()

	_, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sha256Hex(content),
		OriginalName: "clip.mp4",
		MIMEType:     "video/mp4",
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

func TestCanStoreRejectsWhenDiskFloorWouldBeViolated(t *testing.T) {
	fixture := newAudioFixture(t)
	fixture.service.freeSpace = func(string) (int64, error) { return 15, nil }

	err := fixture.service.CanStore(6)

	if !errors.Is(err, ErrInsufficientFreeSpace) {
		t.Fatalf("error = %v, want insufficient free space", err)
	}
}

func TestCleanupRemovesTracksAfterRoomExpiration(t *testing.T) {
	ctx := context.Background()
	fixture := newAudioFixture(t)
	content := []byte("ID3old audio")
	sum := sha256Hex(content)
	status, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sum,
		OriginalName: "old.mp3",
		MIMEType:     "audio/mpeg",
		File:         bytes.NewReader(content),
		IsAdmin:      true,
	})
	if err != nil {
		t.Fatalf("store audio: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `update rooms set expires_at = ?, audio_current_track_id = ?, audio_state = 'playing', audio_position_seconds = 42, audio_started_at = ? where id = ?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano), status.ID, time.Now().UTC().Format(time.RFC3339Nano), fixture.room.ID); err != nil {
		t.Fatalf("expire room: %v", err)
	}

	if err := fixture.service.Cleanup(ctx, time.Now().UTC(), 7*24*time.Hour); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(fixture.audioDir, sum+".mp3")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected audio file deleted, got %v", err)
	}
	var currentTrack any
	var state string
	var position int
	var startedAt any
	if err := fixture.db.QueryRowContext(ctx, `select audio_current_track_id, audio_state, audio_position_seconds, audio_started_at from rooms where id = ?`, fixture.room.ID).Scan(&currentTrack, &state, &position, &startedAt); err != nil {
		t.Fatalf("read room audio state: %v", err)
	}
	if currentTrack != nil || state != "paused" || position != 0 || startedAt != nil {
		t.Fatalf("audio state = current %#v state %q position %d startedAt %#v, want cleared paused state", currentTrack, state, position, startedAt)
	}
}

func TestCleanupKeepsAudioForNeverExpireRoomPastFallbackCutoff(t *testing.T) {
	ctx := context.Background()
	fixture := newAudioFixture(t)
	content := []byte("ID3kept audio")
	sum := sha256Hex(content)
	if _, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sum,
		OriginalName: "kept.mp3",
		MIMEType:     "audio/mpeg",
		File:         bytes.NewReader(content),
		IsAdmin:      true,
	}); err != nil {
		t.Fatalf("store audio: %v", err)
	}
	now := time.Now().UTC()
	if _, err := fixture.db.ExecContext(ctx, `update rooms set expires_at = null, created_at = ? where id = ?`, now.Add(-30*24*time.Hour).Format(time.RFC3339Nano), fixture.room.ID); err != nil {
		t.Fatalf("mark room never expire: %v", err)
	}

	if err := fixture.service.Cleanup(ctx, now, 7*24*time.Hour); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(fixture.audioDir, sum+".mp3")); err != nil {
		t.Fatalf("expected audio file kept, got %v", err)
	}
	var tracks int
	if err := fixture.db.QueryRowContext(ctx, `select count(*) from room_audio_tracks where room_id = ?`, fixture.room.ID).Scan(&tracks); err != nil {
		t.Fatalf("count audio tracks: %v", err)
	}
	if tracks != 1 {
		t.Fatalf("tracks = %d, want 1", tracks)
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

func m4aBytes() []byte {
	return append([]byte{0, 0, 0, 24}, []byte("ftypM4A \x00\x00\x00\x00M4A mp42isom")...)
}
