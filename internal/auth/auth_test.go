package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/NightMachinery/SlideTalk/internal/store"
)

func TestEnsureUserReturnsSameUserForSameTokenAndStoresHash(t *testing.T) {
	ctx := context.Background()
	db := openAuthTestDB(t)
	service := NewService(db, t.TempDir())

	first, err := service.EnsureUser(ctx, "raw-local-token")
	if err != nil {
		t.Fatalf("ensure first user: %v", err)
	}
	second, err := service.EnsureUser(ctx, "raw-local-token")
	if err != nil {
		t.Fatalf("ensure second user: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same user id, got %q and %q", first.ID, second.ID)
	}

	var storedHash string
	if err := db.QueryRowContext(ctx, "select token_hash from users where id = ?", first.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored hash: %v", err)
	}
	if storedHash == "raw-local-token" {
		t.Fatal("raw token was stored instead of a hash")
	}
}

func TestAdminTokenPromotion(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db := openAuthTestDB(t)
	service := NewService(db, dataDir)

	if err := service.EnsureAdminToken(ctx); err != nil {
		t.Fatalf("ensure admin token: %v", err)
	}
	tokenPath := filepath.Join(dataDir, "admin_token")
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if len(tokenBytes) == 0 {
		t.Fatal("expected admin token file to contain a token")
	}

	user, err := service.EnsureUser(ctx, "candidate-token")
	if err != nil {
		t.Fatalf("ensure user: %v", err)
	}
	if promoted, err := service.PromoteWithAdminToken(ctx, user.ID, "wrong-token"); err != nil {
		t.Fatalf("wrong token returned error: %v", err)
	} else if promoted {
		t.Fatal("wrong token promoted user")
	}

	if promoted, err := service.PromoteWithAdminToken(ctx, user.ID, string(tokenBytes)); err != nil {
		t.Fatalf("correct token returned error: %v", err)
	} else if !promoted {
		t.Fatal("correct token did not promote user")
	}

	updated, err := service.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !updated.IsAdmin {
		t.Fatal("user was not marked admin")
	}
}

func openAuthTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(context.Background(), db.DB); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	return db
}
