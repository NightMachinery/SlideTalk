package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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

func TestTurnNextPreviousSkipsObservers(t *testing.T) {
	hub, authService, roomService, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()
	third := namedUser(t, ctx, authService, "third", "Linus")
	observer := namedUser(t, ctx, authService, "observer", "Marie")
	if _, err := roomService.Join(ctx, roomID, third.ID, rooms.JoinInput{}); err != nil {
		t.Fatalf("join third: %v", err)
	}
	if _, err := roomService.Join(ctx, roomID, observer.ID, rooms.JoinInput{}); err != nil {
		t.Fatalf("join observer: %v", err)
	}
	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandPeopleSetRole,
		Payload: mustJSON(t, map[string]string{"userId": observer.ID, "role": rooms.RoleObserver}),
	}); err != nil {
		t.Fatalf("set observer: %v", err)
	}

	for _, want := range []string{modID, participantID, third.ID, modID} {
		if err := hub.HandleCommand(ctx, roomID, modID, Command{Type: CommandTurnNext}); err != nil {
			t.Fatalf("next turn: %v", err)
		}
		snapshot, err := hub.Snapshot(ctx, roomID, modID)
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if snapshot.CurrentTurn.CurrentSpeakerUserID != want {
			t.Fatalf("current speaker = %q, want %q", snapshot.CurrentTurn.CurrentSpeakerUserID, want)
		}
	}
	if err := hub.HandleCommand(ctx, roomID, modID, Command{Type: CommandTurnPrevious}); err != nil {
		t.Fatalf("previous turn: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.CurrentTurn.CurrentSpeakerUserID != third.ID {
		t.Fatalf("previous current speaker = %q, want %q", snapshot.CurrentTurn.CurrentSpeakerUserID, third.ID)
	}
}

func TestQueueModePicksEarliestRaisedHandAndClearsIt(t *testing.T) {
	hub, authService, roomService, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()
	third := namedUser(t, ctx, authService, "third", "Linus")
	if _, err := roomService.Join(ctx, roomID, third.ID, rooms.JoinInput{}); err != nil {
		t.Fatalf("join third: %v", err)
	}
	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandSettingsUpdate,
		Payload: mustJSON(t, map[string]string{"raiseHandMode": RaiseHandModeQueue}),
	}); err != nil {
		t.Fatalf("set queue mode: %v", err)
	}
	if err := hub.HandleCommand(ctx, roomID, third.ID, Command{Type: CommandHandRaise}); err != nil {
		t.Fatalf("third raise: %v", err)
	}
	if err := hub.HandleCommand(ctx, roomID, participantID, Command{Type: CommandHandRaise}); err != nil {
		t.Fatalf("participant raise: %v", err)
	}

	if err := hub.HandleCommand(ctx, roomID, modID, Command{Type: CommandTurnNext}); err != nil {
		t.Fatalf("next turn: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.CurrentTurn.CurrentSpeakerUserID != third.ID {
		t.Fatalf("current speaker = %q, want earliest raised hand %q", snapshot.CurrentTurn.CurrentSpeakerUserID, third.ID)
	}
	if len(snapshot.Hands) != 1 || snapshot.Hands[0].UserID != participantID {
		t.Fatalf("hands after queue pick = %+v, want only %q", snapshot.Hands, participantID)
	}
}

func TestManualModeDoesNotAutoPickRaisedHands(t *testing.T) {
	hub, _, _, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()
	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandSettingsUpdate,
		Payload: mustJSON(t, map[string]string{"raiseHandMode": RaiseHandModeManual}),
	}); err != nil {
		t.Fatalf("set manual mode: %v", err)
	}
	if err := hub.HandleCommand(ctx, roomID, participantID, Command{Type: CommandHandRaise}); err != nil {
		t.Fatalf("raise hand: %v", err)
	}
	if err := hub.HandleCommand(ctx, roomID, modID, Command{Type: CommandTurnNext}); err != nil {
		t.Fatalf("next turn: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.CurrentTurn.CurrentSpeakerUserID != modID {
		t.Fatalf("current speaker = %q, want normal first speaker %q", snapshot.CurrentTurn.CurrentSpeakerUserID, modID)
	}
	if len(snapshot.Hands) != 1 || snapshot.Hands[0].UserID != participantID {
		t.Fatalf("manual hands = %+v, want raised hand retained", snapshot.Hands)
	}
}

func TestNonModTimerCommandsForbidden(t *testing.T) {
	hub, _, _, roomID, _, participantID := setupRealtimeTest(t)
	ctx := context.Background()

	err := hub.HandleCommand(ctx, roomID, participantID, Command{
		Type:    CommandTimerStart,
		Payload: mustJSON(t, map[string]int{"durationSeconds": 300}),
	})
	if err == nil {
		t.Fatal("non-mod timer command succeeded")
	}
}

func TestTimerSnapshotIncludesServerTiming(t *testing.T) {
	hub, _, _, roomID, modID, _ := setupRealtimeTest(t)
	ctx := context.Background()
	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandTimerStart,
		Payload: mustJSON(t, map[string]int{"durationSeconds": 300}),
	}); err != nil {
		t.Fatalf("start timer: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Timer.State != TimerStateRunning {
		t.Fatalf("timer state = %q, want %q", snapshot.Timer.State, TimerStateRunning)
	}
	if snapshot.Timer.DurationSeconds != 300 {
		t.Fatalf("timer duration = %d, want 300", snapshot.Timer.DurationSeconds)
	}
	if snapshot.Timer.StartedAt == nil || snapshot.Timer.ServerNow == "" {
		t.Fatalf("timer lacks server timing: %+v", snapshot.Timer)
	}
}

func TestTimerStopPreservesRemainingDuration(t *testing.T) {
	hub, _, _, roomID, modID, _ := setupRealtimeTest(t)
	ctx := context.Background()
	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandTimerStart,
		Payload: mustJSON(t, map[string]int{"durationSeconds": 300}),
	}); err != nil {
		t.Fatalf("start timer: %v", err)
	}
	if _, err := hub.db.ExecContext(ctx, `update rooms set timer_started_at = ? where id = ?`, time.Now().UTC().Add(-80*time.Second).Format(time.RFC3339Nano), roomID); err != nil {
		t.Fatalf("age timer: %v", err)
	}

	if err := hub.HandleCommand(ctx, roomID, modID, Command{Type: CommandTimerStop}); err != nil {
		t.Fatalf("stop timer: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Timer.State != TimerStateStopped {
		t.Fatalf("timer state = %q, want stopped", snapshot.Timer.State)
	}
	if snapshot.Timer.StartedAt != nil {
		t.Fatalf("stopped timer kept started_at: %+v", snapshot.Timer)
	}
	if snapshot.Timer.DurationSeconds > 221 || snapshot.Timer.DurationSeconds < 219 {
		t.Fatalf("remaining duration = %d, want about 220", snapshot.Timer.DurationSeconds)
	}
}

func TestSlideNavigateUpdatesSharedPageOnlyWhenSharingEnabled(t *testing.T) {
	hub, _, _, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()

	err := hub.HandleCommand(ctx, roomID, participantID, Command{
		Type:    CommandSlideNavigate,
		Payload: mustJSON(t, map[string]any{"page": 2, "modSharedNavigationEnabled": true}),
	})
	if err == nil {
		t.Fatal("participant shared navigation succeeded")
	}
	err = hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandSlideNavigate,
		Payload: mustJSON(t, map[string]any{"page": 3, "modSharedNavigationEnabled": false}),
	})
	if !errors.Is(err, ErrNoBroadcast) {
		t.Fatalf("sharing-disabled navigate error = %v, want no broadcast sentinel", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Room.SlidePage != 1 {
		t.Fatalf("slide page = %d, want unchanged default 1", snapshot.Room.SlidePage)
	}

	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandSlideNavigate,
		Payload: mustJSON(t, map[string]any{"page": 4, "modSharedNavigationEnabled": true}),
	}); err != nil {
		t.Fatalf("shared navigate: %v", err)
	}
	snapshot, err = hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Room.SlidePage != 4 {
		t.Fatalf("slide page = %d, want 4", snapshot.Room.SlidePage)
	}
}

func TestParticipantMarkdownRequiresRoomSetting(t *testing.T) {
	hub, _, _, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()

	err := hub.HandleCommand(ctx, roomID, participantID, Command{
		Type:    CommandMarkdownUpdate,
		Payload: mustJSON(t, map[string]string{"markdown": "# Participant notes"}),
	})
	if err == nil {
		t.Fatal("participant markdown edit succeeded while disabled")
	}
	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandSettingsUpdate,
		Payload: mustJSON(t, map[string]any{"allowParticipantMarkdown": true, "noSlideMode": true, "sharedNavigationEnabled": false}),
	}); err != nil {
		t.Fatalf("enable participant markdown: %v", err)
	}
	if err := hub.HandleCommand(ctx, roomID, participantID, Command{
		Type:    CommandMarkdownUpdate,
		Payload: mustJSON(t, map[string]string{"markdown": "# Participant notes"}),
	}); err != nil {
		t.Fatalf("participant markdown edit after enable: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Markdown != "# Participant notes" {
		t.Fatalf("markdown = %q", snapshot.Markdown)
	}
	if snapshot.MarkdownUpdatedByUserID != participantID || snapshot.MarkdownUpdatedAt == "" {
		t.Fatalf("markdown metadata missing: %+v", snapshot)
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
