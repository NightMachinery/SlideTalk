package store

import (
	"context"
	"database/sql"
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
		{"room_members", "allow_audio_upload"},
		{"room_members", "allow_audio_control"},
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

func TestMigrateAddsRoomMemberAudioPermissionColumnsToExistingDatabase(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.ExecContext(ctx, `create table users (
		id text primary key,
		token_hash text unique not null,
		display_name text not null,
		is_admin integer not null default 0,
		created_at text not null,
		updated_at text not null
	)`); err != nil {
		t.Fatalf("create users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `create table rooms (
		id text primary key,
		title text not null,
		created_by_user_id text not null,
		created_at text not null,
		updated_at text not null
	)`); err != nil {
		t.Fatalf("create rooms: %v", err)
	}
	if _, err := db.ExecContext(ctx, `create table room_members (
		room_id text not null,
		user_id text not null,
		role text not null,
		display_order integer not null,
		joined_at text not null,
		kicked_at text,
		primary key(room_id, user_id)
	)`); err != nil {
		t.Fatalf("create old room_members: %v", err)
	}
	if _, err := db.ExecContext(ctx, `insert into users (id, token_hash, display_name, created_at, updated_at) values ('user-one', 'hash-one', 'Ada', '2026-05-26T00:00:00Z', '2026-05-26T00:00:00Z')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `insert into rooms (id, title, created_by_user_id, created_at, updated_at) values ('room-one', 'Room One', 'user-one', '2026-05-26T00:00:00Z', '2026-05-26T00:00:00Z')`); err != nil {
		t.Fatalf("insert room: %v", err)
	}
	if _, err := db.ExecContext(ctx, `insert into room_members (room_id, user_id, role, display_order, joined_at) values ('room-one', 'user-one', 'participant', 0, '2026-05-26T00:00:00Z')`); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	if err := Migrate(ctx, db.DB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var allowUpload, allowControl bool
	if err := db.QueryRowContext(ctx, `select allow_audio_upload, allow_audio_control from room_members where room_id = 'room-one' and user_id = 'user-one'`).Scan(&allowUpload, &allowControl); err != nil {
		t.Fatalf("read member audio permission defaults: %v", err)
	}
	if allowUpload || allowControl {
		t.Fatalf("audio permission defaults = upload %v control %v, want both false", allowUpload, allowControl)
	}

	columnInfo := map[string]struct {
		notNull      bool
		defaultValue string
	}{}
	rows, err := db.QueryContext(ctx, `pragma table_info(room_members)`)
	if err != nil {
		t.Fatalf("read room_members table info: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan room_members table info: %v", err)
		}
		if name == "allow_audio_upload" || name == "allow_audio_control" {
			columnInfo[name] = struct {
				notNull      bool
				defaultValue string
			}{notNull: notNull == 1, defaultValue: defaultValue.String}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate room_members table info: %v", err)
	}
	for _, column := range []string{"allow_audio_upload", "allow_audio_control"} {
		info, ok := columnInfo[column]
		if !ok {
			t.Fatalf("expected %s column to exist", column)
		}
		if !info.notNull || info.defaultValue != "0" {
			t.Fatalf("%s constraints = notNull %v default %q, want not null default 0", column, info.notNull, info.defaultValue)
		}
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
