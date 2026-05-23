package rooms

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestCreateRoomDefaultsHandRaiseModeToManual(t *testing.T) {
	ctx := context.Background()
	db := openRoomsTestDB(t)
	authService := auth.NewService(db, t.TempDir())
	user := namedUser(t, ctx, authService, "creator-token", "Ada")
	service := NewService(db)

	room, err := service.Create(ctx, user.ID, CreateInput{Title: "Weekly roundtable"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	var mode string
	if err := db.QueryRowContext(ctx, `select raise_hand_mode from rooms where id = ?`, room.ID).Scan(&mode); err != nil {
		t.Fatalf("read raise hand mode: %v", err)
	}
	if mode != "manual" {
		t.Fatalf("raise hand mode = %q, want manual", mode)
	}
}

func TestCreateRoomDefaultsExpirationInFuture(t *testing.T) {
	ctx := context.Background()
	db := openRoomsTestDB(t)
	authService := auth.NewService(db, t.TempDir())
	user := namedUser(t, ctx, authService, "creator-token", "Ada")
	service := NewService(db)

	room, err := service.Create(ctx, user.ID, CreateInput{Title: "Weekly roundtable"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	var raw string
	if err := db.QueryRowContext(ctx, `select expires_at from rooms where id = ?`, room.ID).Scan(&raw); err != nil {
		t.Fatalf("read room expiration: %v", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("parse expiration: %v", err)
	}
	remaining := time.Until(expiresAt)
	if remaining < 6*24*time.Hour || remaining > 8*24*time.Hour {
		t.Fatalf("expiration remaining = %v, want about 7 days", remaining)
	}
}

func TestUpdateRetentionEnforcesModAndAdminLimits(t *testing.T) {
	ctx := context.Background()
	db := openRoomsTestDB(t)
	authService := auth.NewService(db, t.TempDir())
	mod := namedUser(t, ctx, authService, "creator-token", "Ada")
	participant := namedUser(t, ctx, authService, "guest-token", "Grace")
	service := NewService(db)
	room, err := service.Create(ctx, mod.ID, CreateInput{Title: "Retention"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	if _, err := service.Join(ctx, room.ID, participant.ID, JoinInput{}); err != nil {
		t.Fatalf("join room: %v", err)
	}

	if err := service.UpdateRetention(ctx, room.ID, participant.ID, false, time.Now().UTC().Add(3*24*time.Hour), false); !errors.Is(err, ErrNotModerator) {
		t.Fatalf("participant retention update error = %v, want not moderator", err)
	}
	if err := service.UpdateRetention(ctx, room.ID, mod.ID, false, time.Now().UTC().Add(11*24*time.Hour), false); !errors.Is(err, ErrInvalidRetention) {
		t.Fatalf("mod over-extension error = %v, want invalid retention", err)
	}
	if err := service.UpdateRetention(ctx, room.ID, mod.ID, false, time.Now().UTC().Add(9*24*time.Hour), false); err != nil {
		t.Fatalf("mod retention extension: %v", err)
	}
	if err := service.UpdateRetention(ctx, room.ID, mod.ID, false, time.Now().UTC().Add(2*24*time.Hour), false); !errors.Is(err, ErrInvalidRetention) {
		t.Fatalf("mod shortening error = %v, want invalid retention", err)
	}
	if err := service.UpdateRetention(ctx, room.ID, mod.ID, true, time.Now().UTC().Add(23*time.Hour), false); !errors.Is(err, ErrInvalidRetention) {
		t.Fatalf("admin too-short retention error = %v, want invalid retention", err)
	}
	if err := service.UpdateRetention(ctx, room.ID, mod.ID, true, time.Time{}, true); err != nil {
		t.Fatalf("admin never-expire retention: %v", err)
	}

	var expiresAt any
	if err := db.QueryRowContext(ctx, `select expires_at from rooms where id = ?`, room.ID).Scan(&expiresAt); err != nil {
		t.Fatalf("read expiration: %v", err)
	}
	if expiresAt != nil {
		t.Fatalf("expires_at = %#v, want null", expiresAt)
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

func TestExistingPasswordRoomMemberRejoinsWithoutPassword(t *testing.T) {
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
	first, err := service.Join(ctx, room.ID, guest.ID, JoinInput{Password: "correct horse"})
	if err != nil {
		t.Fatalf("initial join: %v", err)
	}

	rejoined, err := service.Join(ctx, room.ID, guest.ID, JoinInput{})
	if err != nil {
		t.Fatalf("rejoin without password: %v", err)
	}
	if rejoined != first {
		t.Fatalf("rejoined membership = %+v, want %+v", rejoined, first)
	}
}

func TestKickedPasswordRoomMemberCannotRejoinWithPassword(t *testing.T) {
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
	if _, err := service.Join(ctx, room.ID, guest.ID, JoinInput{Password: "correct horse"}); err != nil {
		t.Fatalf("join room: %v", err)
	}
	if err := service.MarkKickedForTest(ctx, room.ID, guest.ID); err != nil {
		t.Fatalf("kick member: %v", err)
	}

	if _, err := service.Join(ctx, room.ID, guest.ID, JoinInput{Password: "correct horse"}); err == nil {
		t.Fatal("kicked password room member rejoined")
	}
}

func TestMigrationLinkCanJoinPasswordRoom(t *testing.T) {
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
	link, err := service.IssueMigrationLink(ctx, room.ID, creator.ID)
	if err != nil {
		t.Fatalf("issue migration link: %v", err)
	}

	membership, err := service.Join(ctx, room.ID, guest.ID, JoinInput{MigrationID: link.MigrationID})
	if err != nil {
		t.Fatalf("join with migration link: %v", err)
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

func TestIssueMigrationLinkStoresOnlyHashedBearerSecret(t *testing.T) {
	ctx := context.Background()
	db := openRoomsTestDB(t)
	authService := auth.NewService(db, t.TempDir())
	creator := namedUser(t, ctx, authService, "creator-token", "Ada")
	service := NewService(db)

	room, err := service.Create(ctx, creator.ID, CreateInput{Title: "Private"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	link, err := service.IssueMigrationLink(ctx, room.ID, creator.ID)
	if err != nil {
		t.Fatalf("issue migration link: %v", err)
	}

	if link.MigrationID == "" {
		t.Fatal("expected migration id")
	}
	if link.RoomID != room.ID {
		t.Fatalf("room id = %q, want %q", link.RoomID, room.ID)
	}
	if link.ExpiresAt == "" {
		t.Fatal("expected expiration")
	}
	var storedHash string
	if err := db.QueryRowContext(ctx, `select migration_id_hash from room_migration_links where room_id = ?`, room.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored migration link: %v", err)
	}
	if storedHash == link.MigrationID {
		t.Fatal("stored raw migration id")
	}
	if !service.validMigrationLinkForTest(ctx, room.ID, link.MigrationID) {
		t.Fatal("issued migration link did not validate")
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
