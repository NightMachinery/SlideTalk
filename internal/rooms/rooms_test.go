package rooms

import (
	"context"
	"testing"

	"github.com/NightMachinery/SlideTalk/internal/auth"
	"github.com/NightMachinery/SlideTalk/internal/store"
)

func TestCreateRoomMakesCreatorModerator(t *testing.T) {
	ctx := context.Background()
	db := openRoomsTestDB(t)
	authService := auth.NewService(db, t.TempDir())
	user := namedUser(t, ctx, authService, "creator-token", "Ada")
	service := NewService(db)

	room, err := service.Create(ctx, user.ID, CreateInput{Title: "Weekly roundtable"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	details, err := service.GetForUser(ctx, room.ID, user.ID)
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if details.Membership.Role != RoleMod {
		t.Fatalf("expected creator role %q, got %q", RoleMod, details.Membership.Role)
	}
}

func TestPasswordRoomRequiresCorrectPassword(t *testing.T) {
	ctx := context.Background()
	db := openRoomsTestDB(t)
	authService := auth.NewService(db, t.TempDir())
	creator := namedUser(t, ctx, authService, "creator-token", "Ada")
	guest := namedUser(t, ctx, authService, "guest-token", "Grace")
	service := NewService(db)

	room, err := service.Create(ctx, creator.ID, CreateInput{Title: "Private", Password: "correct horse"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	if _, err := service.Join(ctx, room.ID, guest.ID, JoinInput{}); err == nil {
		t.Fatal("missing password joined room")
	}
	if _, err := service.Join(ctx, room.ID, guest.ID, JoinInput{Password: "wrong"}); err == nil {
		t.Fatal("wrong password joined room")
	}
	membership, err := service.Join(ctx, room.ID, guest.ID, JoinInput{Password: "correct horse"})
	if err != nil {
		t.Fatalf("correct password join: %v", err)
	}
	if membership.Role != RoleParticipant {
		t.Fatalf("expected participant role, got %q", membership.Role)
	}
}

func TestKickedMemberCannotRejoin(t *testing.T) {
	ctx := context.Background()
	db := openRoomsTestDB(t)
	authService := auth.NewService(db, t.TempDir())
	creator := namedUser(t, ctx, authService, "creator-token", "Ada")
	guest := namedUser(t, ctx, authService, "guest-token", "Grace")
	service := NewService(db)

	room, err := service.Create(ctx, creator.ID, CreateInput{Title: "Private"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	if _, err := service.Join(ctx, room.ID, guest.ID, JoinInput{}); err != nil {
		t.Fatalf("join room: %v", err)
	}
	if err := service.MarkKickedForTest(ctx, room.ID, guest.ID); err != nil {
		t.Fatalf("kick member: %v", err)
	}
	if _, err := service.Join(ctx, room.ID, guest.ID, JoinInput{}); err == nil {
		t.Fatal("kicked member rejoined")
	}
}

func namedUser(t *testing.T, ctx context.Context, service *auth.Service, token string, name string) auth.User {
	t.Helper()
	user, err := service.EnsureUser(ctx, token)
	if err != nil {
		t.Fatalf("ensure user: %v", err)
	}
	if err := service.UpdateDisplayName(ctx, user.ID, name); err != nil {
		t.Fatalf("update display name: %v", err)
	}
	updated, err := service.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	return updated
}

func openRoomsTestDB(t *testing.T) *store.DB {
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
