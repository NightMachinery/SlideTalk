package realtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NightMachinery/SlideTalk/internal/auth"
	"github.com/NightMachinery/SlideTalk/internal/rooms"
	"github.com/NightMachinery/SlideTalk/internal/store"
)

func TestTicketIssueConsumeAndRejectReuse(t *testing.T) {
	hub, _, _, roomID, modID, _ := setupRealtimeTest(t)
	ctx := context.Background()

	ticket, err := hub.IssueTicket(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	claims, err := hub.ConsumeTicket(ticket.Ticket)
	if err != nil {
		t.Fatalf("consume ticket: %v", err)
	}
	if claims.RoomID != roomID || claims.UserID != modID {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if _, err := hub.ConsumeTicket(ticket.Ticket); err == nil {
		t.Fatal("reused ticket was accepted")
	}
	if _, err := hub.ConsumeTicket("missing"); err == nil {
		t.Fatal("missing ticket was accepted")
	}
}

func TestSnapshotOrdersParticipantsAndObservers(t *testing.T) {
	hub, _, _, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()

	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:      CommandPeopleSetRole,
		RequestID: "role-1",
		Payload:   mustJSON(t, map[string]string{"userId": participantID, "role": rooms.RoleObserver}),
	}); err != nil {
		t.Fatalf("set observer: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.Participants) != 1 || snapshot.Participants[0].UserID != modID {
		t.Fatalf("unexpected participants: %+v", snapshot.Participants)
	}
	if len(snapshot.Observers) != 1 || snapshot.Observers[0].UserID != participantID {
		t.Fatalf("unexpected observers: %+v", snapshot.Observers)
	}
}

func TestReorderChangesOrder(t *testing.T) {
	hub, _, roomService, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()
	authService := auth.NewService(hub.db, t.TempDir())
	third := namedUser(t, ctx, authService, "third", "Linus")
	if _, err := roomService.Join(ctx, roomID, third.ID, rooms.JoinInput{}); err != nil {
		t.Fatalf("join third: %v", err)
	}

	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:      CommandPeopleReorder,
		RequestID: "reorder-1",
		Payload: mustJSON(t, map[string][]string{
			"orderedUserIds":  {third.ID, modID, participantID},
			"observerUserIds": {},
		}),
	}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if got := snapshot.Participants[0].UserID; got != third.ID {
		t.Fatalf("first participant = %q, want %q", got, third.ID)
	}
}

func TestNonModCommandForbiddenAndLastModProtected(t *testing.T) {
	hub, _, _, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()

	err := hub.HandleCommand(ctx, roomID, participantID, Command{
		Type:      CommandPeopleKick,
		RequestID: "kick-1",
		Payload:   mustJSON(t, map[string]string{"userId": modID}),
	})
	if err == nil {
		t.Fatal("non-mod command succeeded")
	}

	err = hub.HandleCommand(ctx, roomID, modID, Command{
		Type:      CommandPeopleSetRole,
		RequestID: "role-1",
		Payload:   mustJSON(t, map[string]string{"userId": modID, "role": rooms.RoleParticipant}),
	})
	if err == nil {
		t.Fatal("last mod demotion succeeded")
	}

	err = hub.HandleCommand(ctx, roomID, modID, Command{
		Type:      CommandPeopleKick,
		RequestID: "kick-2",
		Payload:   mustJSON(t, map[string]string{"userId": modID}),
	})
	if err == nil {
		t.Fatal("last mod kick succeeded")
	}
}

func setupRealtimeTest(t *testing.T) (*Hub, *auth.Service, *rooms.Service, string, string, string) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
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
	authService := auth.NewService(db, t.TempDir())
	roomService := rooms.NewService(db)
	mod := namedUser(t, ctx, authService, "mod", "Ada")
	participant := namedUser(t, ctx, authService, "participant", "Grace")
	room, err := roomService.Create(ctx, mod.ID, rooms.CreateInput{Title: "Realtime"})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	if _, err := roomService.Join(ctx, room.ID, participant.ID, rooms.JoinInput{}); err != nil {
		t.Fatalf("join room: %v", err)
	}
	return NewHub(db, authService, roomService), authService, roomService, room.ID, mod.ID, participant.ID
}

func namedUser(t *testing.T, ctx context.Context, service *auth.Service, token string, name string) auth.User {
	t.Helper()
	user, err := service.EnsureUser(ctx, token)
	if err != nil {
		t.Fatalf("ensure user: %v", err)
	}
	if err := service.UpdateDisplayName(ctx, user.ID, name); err != nil {
		t.Fatalf("update name: %v", err)
	}
	updated, err := service.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	return updated
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	bytes, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return bytes
}
