package store

import (
	"context"
	"database/sql"
	"fmt"
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
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}
