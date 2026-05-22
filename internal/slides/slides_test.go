package slides

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

func TestStoreRejectsNonPDFExtension(t *testing.T) {
	ctx := context.Background()
	fixture := newSlideFixture(t)
	sum := sha256Hex([]byte("%PDF-1.7\n"))

	_, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sum,
		OriginalName: "slides.txt",
		ExpiresAt:    time.Now().UTC().Add(24 * time.Hour),
		MIMEType:     "application/pdf",
		File:         bytes.NewReader([]byte("%PDF-1.7\n")),
	})

	if !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("expected unsupported file error, got %v", err)
	}
}

func TestStoreAcceptsImageSlideAndUsesValidatedExtensionAndMIME(t *testing.T) {
	ctx := context.Background()
	fixture := newSlideFixture(t)
	content := []byte("\x89PNG\r\n\x1a\nimage\n")
	sum := sha256Hex(content)

	_, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sum,
		OriginalName: "diagram.png",
		ExpiresAt:    time.Now().UTC().Add(24 * time.Hour),
		MIMEType:     "image/png",
		File:         bytes.NewReader(content),
	})
	if err != nil {
		t.Fatalf("store image slide: %v", err)
	}

	if _, err := os.Stat(filepath.Join(fixture.slideDir, sum+".png")); err != nil {
		t.Fatalf("stored image path: %v", err)
	}
	file, err := fixture.service.CurrentRoomFile(ctx, fixture.room.ID)
	if err != nil {
		t.Fatalf("current room file: %v", err)
	}
	if file.MIMEType != "image/png" {
		t.Fatalf("mime type = %q, want image/png", file.MIMEType)
	}
}

func TestStorePreservesJPGExtensionForJPEGSlides(t *testing.T) {
	ctx := context.Background()
	fixture := newSlideFixture(t)
	content := []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43, 0x00}
	sum := sha256Hex(content)

	_, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sum,
		OriginalName: "photo.jpg",
		ExpiresAt:    time.Now().UTC().Add(24 * time.Hour),
		MIMEType:     "image/jpeg",
		File:         bytes.NewReader(content),
	})
	if err != nil {
		t.Fatalf("store jpg slide: %v", err)
	}

	if _, err := os.Stat(filepath.Join(fixture.slideDir, sum+".jpg")); err != nil {
		t.Fatalf("stored jpg path: %v", err)
	}
}

func TestStoreRejectsMismatchedImageExtensionAndMIME(t *testing.T) {
	ctx := context.Background()
	fixture := newSlideFixture(t)
	content := []byte("not really a jpeg")
	sum := sha256Hex(content)

	_, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sum,
		OriginalName: "diagram.png",
		ExpiresAt:    time.Now().UTC().Add(24 * time.Hour),
		MIMEType:     "image/jpeg",
		File:         bytes.NewReader(content),
	})

	if !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("expected unsupported file error, got %v", err)
	}
}

func TestStoreRejectsHashMismatch(t *testing.T) {
	ctx := context.Background()
	fixture := newSlideFixture(t)

	_, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sha256Hex([]byte("different")),
		OriginalName: "slides.pdf",
		ExpiresAt:    time.Now().UTC().Add(24 * time.Hour),
		MIMEType:     "application/pdf",
		File:         bytes.NewReader([]byte("%PDF-1.7\n")),
	})

	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
}

func TestDuplicateUploadCreatesOnePhysicalFileAndMultipleRoomReferences(t *testing.T) {
	ctx := context.Background()
	fixture := newSlideFixture(t)
	otherRoom, err := fixture.rooms.Create(ctx, fixture.admin.ID, rooms.CreateInput{Title: "Second room"})
	if err != nil {
		t.Fatalf("create second room: %v", err)
	}
	content := []byte("%PDF-1.7\nsame deck\n")
	sum := sha256Hex(content)
	expiresAt := time.Now().UTC().Add(24 * time.Hour)

	first, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sum,
		OriginalName: "first.pdf",
		ExpiresAt:    expiresAt,
		MIMEType:     "application/pdf",
		File:         bytes.NewReader(content),
	})
	if err != nil {
		t.Fatalf("store first: %v", err)
	}
	second, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       otherRoom.ID,
		SHA256:       sum,
		OriginalName: "second.pdf",
		ExpiresAt:    expiresAt,
		MIMEType:     "application/pdf",
		File:         bytes.NewReader(content),
	})
	if err != nil {
		t.Fatalf("store second: %v", err)
	}

	if first.AlreadyUploaded {
		t.Fatal("first upload should not be marked duplicate")
	}
	if !second.AlreadyUploaded {
		t.Fatal("second upload should be marked duplicate")
	}
	entries, err := os.ReadDir(fixture.slideDir)
	if err != nil {
		t.Fatalf("read slide dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one physical file, got %d", len(entries))
	}
	var refs int
	if err := fixture.db.QueryRowContext(ctx, `select count(*) from room_slides where sha256 = ?`, sum).Scan(&refs); err != nil {
		t.Fatalf("count refs: %v", err)
	}
	if refs != 2 {
		t.Fatalf("expected two room references, got %d", refs)
	}
}

func TestCleanupKeepsFileWhileAnyUnexpiredReferenceExists(t *testing.T) {
	ctx := context.Background()
	fixture := newSlideFixture(t)
	otherRoom, err := fixture.rooms.Create(ctx, fixture.admin.ID, rooms.CreateInput{Title: "Second room"})
	if err != nil {
		t.Fatalf("create second room: %v", err)
	}
	content := []byte("%PDF-1.7\ncleanup\n")
	sum := sha256Hex(content)
	now := time.Now().UTC()
	for _, input := range []StoreInput{
		{RoomID: fixture.room.ID, SHA256: sum, OriginalName: "old.pdf", ExpiresAt: now.Add(time.Hour), MIMEType: "application/pdf", File: bytes.NewReader(content)},
		{RoomID: otherRoom.ID, SHA256: sum, OriginalName: "fresh.pdf", ExpiresAt: now.Add(time.Hour), MIMEType: "application/pdf", File: bytes.NewReader(content)},
	} {
		if _, err := fixture.service.Store(ctx, fixture.admin.ID, input); err != nil {
			t.Fatalf("store slide: %v", err)
		}
	}
	if _, err := fixture.db.ExecContext(ctx, `update room_slides set expires_at = ? where room_id = ?`, now.Add(-time.Hour).Format(time.RFC3339Nano), fixture.room.ID); err != nil {
		t.Fatalf("age first ref: %v", err)
	}

	if err := fixture.service.Cleanup(ctx, now); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(fixture.slideDir, sum+".pdf")); err != nil {
		t.Fatalf("expected file to remain: %v", err)
	}
	var refs int
	if err := fixture.db.QueryRowContext(ctx, `select count(*) from room_slides where sha256 = ?`, sum).Scan(&refs); err != nil {
		t.Fatalf("count refs: %v", err)
	}
	if refs != 1 {
		t.Fatalf("expected one unexpired ref, got %d", refs)
	}
}

func TestCleanupRemovesPhysicalFileAfterFinalReferenceExpires(t *testing.T) {
	ctx := context.Background()
	fixture := newSlideFixture(t)
	content := []byte("%PDF-1.7\nexpired\n")
	sum := sha256Hex(content)
	now := time.Now().UTC()
	if _, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sum,
		OriginalName: "expired.pdf",
		ExpiresAt:    now.Add(time.Hour),
		MIMEType:     "application/pdf",
		File:         bytes.NewReader(content),
	}); err != nil {
		t.Fatalf("store slide: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `update room_slides set expires_at = ? where room_id = ?`, now.Add(-time.Hour).Format(time.RFC3339Nano), fixture.room.ID); err != nil {
		t.Fatalf("age ref: %v", err)
	}

	if err := fixture.service.Cleanup(ctx, now); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(fixture.slideDir, sum+".pdf")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file deleted, got %v", err)
	}
}

func TestMissingPhysicalFileIsReportedAsManuallyDeleted(t *testing.T) {
	ctx := context.Background()
	fixture := newSlideFixture(t)
	content := []byte("%PDF-1.7\nmissing\n")
	sum := sha256Hex(content)
	if _, err := fixture.service.Store(ctx, fixture.admin.ID, StoreInput{
		RoomID:       fixture.room.ID,
		SHA256:       sum,
		OriginalName: "missing.pdf",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		MIMEType:     "application/pdf",
		File:         bytes.NewReader(content),
	}); err != nil {
		t.Fatalf("store slide: %v", err)
	}
	if err := os.Remove(filepath.Join(fixture.slideDir, sum+".pdf")); err != nil {
		t.Fatalf("remove physical file: %v", err)
	}

	status, err := fixture.service.Status(ctx, sum)
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if !status.Exists || !status.AlreadyUploaded || !status.Missing {
		t.Fatalf("expected missing uploaded status, got %+v", status)
	}
}

type slideFixture struct {
	db       *store.DB
	rooms    *rooms.Service
	service  *Service
	admin    auth.User
	room     rooms.Room
	slideDir string
}

func newSlideFixture(t *testing.T) slideFixture {
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
	if _, err := db.ExecContext(ctx, `update users set is_admin = 1 where id = ?`, admin.ID); err != nil {
		t.Fatalf("promote user: %v", err)
	}
	admin, err = authService.GetUser(ctx, admin.ID)
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	roomService := rooms.NewService(db)
	room, err := roomService.Create(ctx, admin.ID, rooms.CreateInput{Title: "Deck room"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	slideDir := filepath.Join(dataDir, "slides")
	service, err := NewService(db, slideDir, 1024*1024)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return slideFixture{db: db, rooms: roomService, service: service, admin: admin, room: room, slideDir: slideDir}
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
