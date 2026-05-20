package store

import (
	"context"
	"testing"
)

func TestMigrateCreatesSeedTables(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := Migrate(ctx, db.DB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"users", "rooms", "room_members", "slide_files", "room_slides"} {
		t.Run(table, func(t *testing.T) {
			var name string
			err := db.QueryRowContext(ctx, "select name from sqlite_master where type = 'table' and name = ?", table).Scan(&name)
			if err != nil {
				t.Fatalf("expected table %s to exist: %v", table, err)
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
