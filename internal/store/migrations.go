package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
)

// Migrate applies the seed schema.
func Migrate(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`create table if not exists users (
			id text primary key,
			token_hash text unique not null,
			display_name text not null,
			is_admin integer not null default 0,
			created_at text not null,
			updated_at text not null
		)`,
		`create table if not exists rooms (
			id text primary key,
			title text not null,
			password_hash text,
			no_slide_mode integer not null default 0,
			markdown text not null default '',
			allow_participant_markdown integer not null default 0,
			slide_page integer not null default 1,
			shared_navigation_enabled integer not null default 1,
			markdown_updated_by_user_id text,
			markdown_updated_at text,
			current_speaker_user_id text,
			timer_state text not null default 'stopped',
			timer_duration_seconds integer not null default 0,
			timer_started_at text,
			raise_hand_mode text not null default 'off',
			created_by_user_id text not null,
			created_at text not null,
			updated_at text not null,
			foreign key(created_by_user_id) references users(id)
		)`,
		`create table if not exists room_members (
			room_id text not null,
			user_id text not null,
			role text not null,
			display_order integer not null,
			joined_at text not null,
			kicked_at text,
			primary key(room_id, user_id),
			foreign key(room_id) references rooms(id),
			foreign key(user_id) references users(id)
		)`,
		`create table if not exists raised_hands (
			room_id text not null,
			user_id text not null,
			raised_at text not null,
			primary key(room_id, user_id),
			foreign key(room_id) references rooms(id),
			foreign key(user_id) references users(id)
		)`,
		`create table if not exists slide_files (
			sha256 text primary key,
			ext text not null,
			size_bytes integer not null,
			mime_type text not null,
			stored_path text not null,
			uploaded_by_user_id text not null,
			created_at text not null,
			missing_at text,
			foreign key(uploaded_by_user_id) references users(id)
		)`,
		`create table if not exists room_slides (
			room_id text primary key,
			sha256 text not null,
			original_name text not null,
			expires_at text not null,
			uploaded_by_user_id text not null,
			created_at text not null,
			updated_at text not null,
			foreign key(room_id) references rooms(id),
			foreign key(sha256) references slide_files(sha256),
			foreign key(uploaded_by_user_id) references users(id)
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	roomColumns, err := columns(ctx, db, "rooms")
	if err != nil {
		return err
	}
	alterStatements := map[string]string{
		"current_speaker_user_id":     `alter table rooms add column current_speaker_user_id text`,
		"timer_state":                 `alter table rooms add column timer_state text not null default 'stopped'`,
		"timer_duration_seconds":      `alter table rooms add column timer_duration_seconds integer not null default 0`,
		"timer_started_at":            `alter table rooms add column timer_started_at text`,
		"raise_hand_mode":             `alter table rooms add column raise_hand_mode text not null default 'off'`,
		"slide_page":                  `alter table rooms add column slide_page integer not null default 1`,
		"shared_navigation_enabled":   `alter table rooms add column shared_navigation_enabled integer not null default 1`,
		"markdown_updated_by_user_id": `alter table rooms add column markdown_updated_by_user_id text`,
		"markdown_updated_at":         `alter table rooms add column markdown_updated_at text`,
	}
	for column, statement := range alterStatements {
		if !slices.Contains(roomColumns, column) {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("migrate add %s: %w", column, err)
			}
		}
	}
	return nil
}

func columns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `pragma table_info(`+table+`)`)
	if err != nil {
		return nil, fmt.Errorf("read %s columns: %w", table, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("scan %s column: %w", table, err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s columns: %w", table, err)
	}
	return names, nil
}
