package store

import (
	"context"
	"slices"
	"testing"
)

func TestMigrateCreatesSeedTables(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := Migrate(ctx, db.DB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"users", "rooms", "room_members", "slide_files", "room_slides", "audio_files", "room_audio_tracks", "audio_download_tokens", "room_migration_links"} {
		t.Run(table, func(t *testing.T) {
			var name string
			err := db.QueryRowContext(ctx, "select name from sqlite_master where type = 'table' and name = ?", table).Scan(&name)
			if err != nil {
				t.Fatalf("expected table %s to exist: %v", table, err)
			}
		})
	}
	for _, column := range []string{"slide_page", "shared_navigation_enabled", "markdown_updated_by_user_id", "markdown_updated_at"} {
		t.Run("rooms."+column, func(t *testing.T) {
			columns, err := columns(ctx, db.DB, "rooms")
			if err != nil {
				t.Fatalf("read room columns: %v", err)
			}
			if !slices.Contains(columns, column) {
				t.Fatalf("expected rooms.%s to exist", column)
			}
		})
	}
	for _, item := range []struct {
		table  string
		column string
	}{
		{"audio_files", "metadata_title"},
		{"audio_files", "duration_seconds"},
		{"audio_files", "cover_path"},
		{"audio_files", "cover_mime_type"},
		{"room_audio_tracks", "title"},
		{"room_audio_tracks", "uploader_display_name"},
	} {
		t.Run(item.table+"."+item.column, func(t *testing.T) {
			columns, err := columns(ctx, db.DB, item.table)
			if err != nil {
				t.Fatalf("read %s columns: %v", item.table, err)
			}
			if !slices.Contains(columns, item.column) {
				t.Fatalf("expected %s.%s to exist", item.table, item.column)
			}
		})
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	return db
}
