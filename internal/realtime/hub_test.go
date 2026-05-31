package realtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestObserverCanRejoinSelfButCannotChangeOtherRoles(t *testing.T) {
	hub, authService, roomService, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()
	observer := namedUser(t, ctx, authService, "observer", "Marie")
	if _, err := roomService.Join(ctx, roomID, observer.ID, rooms.JoinInput{}); err != nil {
		t.Fatalf("join observer: %v", err)
	}
	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandPeopleSetRole,
		Payload: mustJSON(t, map[string]string{"userId": observer.ID, "role": rooms.RoleObserver}),
	}); err != nil {
		t.Fatalf("set observer: %v", err)
	}

	if err := hub.HandleCommand(ctx, roomID, observer.ID, Command{
		Type:    CommandPeopleSetRole,
		Payload: mustJSON(t, map[string]string{"userId": observer.ID, "role": rooms.RoleParticipant}),
	}); err != nil {
		t.Fatalf("observer self rejoin: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, observer.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Caller.Role != rooms.RoleParticipant {
		t.Fatalf("caller role = %q, want participant", snapshot.Caller.Role)
	}

	err = hub.HandleCommand(ctx, roomID, observer.ID, Command{
		Type:    CommandPeopleSetRole,
		Payload: mustJSON(t, map[string]string{"userId": participantID, "role": rooms.RoleObserver}),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("observer changed another member role: %v, want forbidden", err)
	}
	err = hub.HandleCommand(ctx, roomID, observer.ID, Command{
		Type:    CommandPeopleSetRole,
		Payload: mustJSON(t, map[string]string{"userId": observer.ID, "role": rooms.RoleMod}),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("observer self promoted: %v, want forbidden", err)
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

func TestModeratorCanRaiseOwnHand(t *testing.T) {
	hub, _, _, roomID, modID, _ := setupRealtimeTest(t)
	ctx := context.Background()

	if err := hub.HandleCommand(ctx, roomID, modID, Command{Type: CommandHandRaise}); err != nil {
		t.Fatalf("mod raise hand: %v", err)
	}

	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.Hands) != 1 || snapshot.Hands[0].UserID != modID {
		t.Fatalf("hands = %+v, want moderator hand", snapshot.Hands)
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

func TestKickSendsTargetedRemovalEvent(t *testing.T) {
	hub, _, _, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()
	modClient := &Client{RoomID: roomID, UserID: modID, Send: make(chan Event, 4)}
	participantClient := &Client{RoomID: roomID, UserID: participantID, Send: make(chan Event, 4)}
	hub.Register(modClient)
	hub.Register(participantClient)
	defer hub.Unregister(modClient)
	defer hub.Unregister(participantClient)

	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandPeopleKick,
		Payload: mustJSON(t, map[string]string{"userId": participantID}),
	}); err != nil {
		t.Fatalf("kick participant: %v", err)
	}

	select {
	case event := <-participantClient.Send:
		if event.Type != EventKicked || event.RoomID != roomID {
			t.Fatalf("participant event = %+v, want kicked event for room", event)
		}
	case <-time.After(time.Second):
		t.Fatal("participant did not receive kicked event")
	}
	select {
	case event := <-modClient.Send:
		t.Fatalf("mod received unexpected targeted event: %+v", event)
	default:
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

func TestSnapshotSlideIncludesMIMEType(t *testing.T) {
	hub, _, _, roomID, modID, _ := setupRealtimeTest(t)
	ctx := context.Background()
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	slidePath := filepath.Join(t.TempDir(), sha+".png")
	if err := os.WriteFile(slidePath, []byte("\x89PNG\r\n\x1a\n"), 0o600); err != nil {
		t.Fatalf("write slide file: %v", err)
	}
	if _, err := hub.db.ExecContext(ctx, `insert into slide_files (sha256, ext, size_bytes, mime_type, stored_path, uploaded_by_user_id, created_at, missing_at) values (?, ?, ?, ?, ?, ?, ?, null)`, sha, "png", 8, "image/png", slidePath, modID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert slide file: %v", err)
	}
	if _, err := hub.db.ExecContext(ctx, `insert into room_slides (room_id, sha256, original_name, expires_at, uploaded_by_user_id, created_at, updated_at) values (?, ?, ?, ?, ?, ?, ?)`, roomID, sha, "diagram.png", time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), modID, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert room slide: %v", err)
	}

	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Slide == nil || snapshot.Slide.MIMEType != "image/png" {
		t.Fatalf("slide = %+v, want image/png MIME type", snapshot.Slide)
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
		Payload: mustJSON(t, map[string]any{"allowParticipantMarkdown": true, "roomMode": RoomModeMarkdown, "sharedNavigationEnabled": false}),
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

func TestRoomModeSettingIsEnum(t *testing.T) {
	hub, _, _, roomID, modID, _ := setupRealtimeTest(t)
	ctx := context.Background()

	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandSettingsUpdate,
		Payload: mustJSON(t, map[string]string{"roomMode": RoomModeAudio}),
	}); err != nil {
		t.Fatalf("set audio mode: %v", err)
	}

	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Room.RoomMode != RoomModeAudio {
		t.Fatalf("room mode = %q, want audio", snapshot.Room.RoomMode)
	}

	err = hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandSettingsUpdate,
		Payload: mustJSON(t, map[string]string{"roomMode": "video"}),
	})
	if !errors.Is(err, ErrBadCommand) {
		t.Fatalf("invalid room mode error = %v, want bad command", err)
	}
}

func TestRepeatBackwardWrapsToLastTrack(t *testing.T) {
	hub, _, _, roomID, modID, _ := setupRealtimeTest(t)
	ctx := context.Background()
	firstID := insertAudioTrack(t, ctx, hub, roomID, modID, "first")
	lastID := insertAudioTrack(t, ctx, hub, roomID, modID, "last")
	if _, err := hub.db.ExecContext(ctx, `update rooms set audio_current_track_id = ?, audio_state = ?, audio_playback_mode = ? where id = ?`, firstID, AudioStatePlaying, AudioModeRepeatBackward, roomID); err != nil {
		t.Fatalf("seed audio state: %v", err)
	}

	if err := hub.HandleCommand(ctx, roomID, modID, Command{Type: CommandAudioEnded}); err != nil {
		t.Fatalf("audio ended: %v", err)
	}

	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Audio.CurrentTrackID != lastID || snapshot.Audio.State != AudioStatePlaying {
		t.Fatalf("audio state = %+v, want wrapped to last track %q", snapshot.Audio, lastID)
	}
}

func TestSnapshotExposesNextAudioTrackID(t *testing.T) {
	hub, _, _, roomID, modID, _ := setupRealtimeTest(t)
	ctx := context.Background()
	firstID := insertAudioTrack(t, ctx, hub, roomID, modID, "first")
	secondID := insertAudioTrack(t, ctx, hub, roomID, modID, "second")
	if _, err := hub.db.ExecContext(ctx, `update rooms set audio_current_track_id = ?, audio_state = ?, audio_playback_mode = ? where id = ?`, firstID, AudioStatePlaying, AudioModeNext, roomID); err != nil {
		t.Fatalf("seed audio state: %v", err)
	}

	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Audio.NextTrackID != secondID {
		t.Fatalf("next track id = %q, want %q", snapshot.Audio.NextTrackID, secondID)
	}
}

func TestAudioEndedUsesSnapshotNextTrackIDForShuffle(t *testing.T) {
	hub, _, _, roomID, modID, _ := setupRealtimeTest(t)
	ctx := context.Background()
	firstID := insertAudioTrack(t, ctx, hub, roomID, modID, "first")
	insertAudioTrack(t, ctx, hub, roomID, modID, "second")
	insertAudioTrack(t, ctx, hub, roomID, modID, "third")
	if _, err := hub.db.ExecContext(ctx, `update rooms set audio_current_track_id = ?, audio_state = ?, audio_playback_mode = ? where id = ?`, firstID, AudioStatePlaying, AudioModeShuffle, roomID); err != nil {
		t.Fatalf("seed audio state: %v", err)
	}

	before, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot before ended: %v", err)
	}
	if before.Audio.NextTrackID == "" || before.Audio.NextTrackID == firstID {
		t.Fatalf("shuffle next track id = %q, want non-current track", before.Audio.NextTrackID)
	}
	again, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot again: %v", err)
	}
	if again.Audio.NextTrackID != before.Audio.NextTrackID {
		t.Fatalf("shuffle next track id changed from %q to %q", before.Audio.NextTrackID, again.Audio.NextTrackID)
	}

	if err := hub.HandleCommand(ctx, roomID, modID, Command{Type: CommandAudioEnded}); err != nil {
		t.Fatalf("audio ended: %v", err)
	}
	after, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot after ended: %v", err)
	}
	if after.Audio.CurrentTrackID != before.Audio.NextTrackID {
		t.Fatalf("audio current track = %q, want prior next track %q", after.Audio.CurrentTrackID, before.Audio.NextTrackID)
	}
}

func TestAudioTrackStarsArePrivateWithOptionalCounts(t *testing.T) {
	hub, _, _, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()
	trackID := insertAudioTrack(t, ctx, hub, roomID, modID, "first")

	if err := hub.HandleCommand(ctx, roomID, participantID, Command{
		Type:    CommandAudioStar,
		Payload: mustJSON(t, map[string]any{"trackId": trackID, "starred": true}),
	}); err != nil {
		t.Fatalf("star track: %v", err)
	}

	modSnapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("mod snapshot: %v", err)
	}
	if modSnapshot.Audio.Tracks[0].StarredByCaller {
		t.Fatal("mod sees participant private star as caller star")
	}
	if modSnapshot.Audio.Tracks[0].StarCount != 0 {
		t.Fatalf("star count = %d, want hidden zero", modSnapshot.Audio.Tracks[0].StarCount)
	}
	participantSnapshot, err := hub.Snapshot(ctx, roomID, participantID)
	if err != nil {
		t.Fatalf("participant snapshot: %v", err)
	}
	if !participantSnapshot.Audio.Tracks[0].StarredByCaller {
		t.Fatal("participant does not see own star")
	}

	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandSettingsUpdate,
		Payload: mustJSON(t, map[string]any{"showAudioStarCounts": true}),
	}); err != nil {
		t.Fatalf("show star counts: %v", err)
	}
	modSnapshot, err = hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("mod snapshot after count setting: %v", err)
	}
	if modSnapshot.Audio.Tracks[0].StarCount != 1 {
		t.Fatalf("star count = %d, want 1", modSnapshot.Audio.Tracks[0].StarCount)
	}
}

func TestParticipantWithAudioControlCanSetFinishMode(t *testing.T) {
	hub, _, _, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()
	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandSettingsUpdate,
		Payload: mustJSON(t, map[string]any{"allowAudienceAudioControl": true}),
	}); err != nil {
		t.Fatalf("enable audience audio control: %v", err)
	}

	if err := hub.HandleCommand(ctx, roomID, participantID, Command{
		Type:    CommandAudioMode,
		Payload: mustJSON(t, map[string]string{"mode": AudioModeShuffle}),
	}); err != nil {
		t.Fatalf("participant set audio mode: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Audio.PlaybackMode != AudioModeShuffle {
		t.Fatalf("playback mode = %q, want shuffle", snapshot.Audio.PlaybackMode)
	}
}

func TestModeratorCanGrantParticipantAudioPermissions(t *testing.T) {
	hub, _, _, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()

	err := hub.HandleCommand(ctx, roomID, participantID, Command{
		Type:    CommandPeopleAudioPermission,
		Payload: mustJSON(t, map[string]any{"userId": participantID, "allowAudioControl": true}),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-mod permission update error = %v, want forbidden", err)
	}

	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandPeopleAudioPermission,
		Payload: mustJSON(t, map[string]any{"userId": participantID, "allowAudioUpload": true, "allowAudioControl": true}),
	}); err != nil {
		t.Fatalf("grant participant audio permissions: %v", err)
	}
	snapshot, err := hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.Participants) < 2 || !snapshot.Participants[1].AllowAudioUpload || !snapshot.Participants[1].AllowAudioControl {
		t.Fatalf("participant grants not reflected in snapshot: %+v", snapshot.Participants)
	}

	if err := hub.HandleCommand(ctx, roomID, participantID, Command{
		Type:    CommandAudioMode,
		Payload: mustJSON(t, map[string]string{"mode": AudioModeShuffle}),
	}); err != nil {
		t.Fatalf("participant with individual control grant set audio mode: %v", err)
	}

	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandPeopleAudioPermission,
		Payload: mustJSON(t, map[string]any{"userId": participantID, "allowAudioUpload": false, "allowAudioControl": false}),
	}); err != nil {
		t.Fatalf("revoke participant audio permissions: %v", err)
	}
	snapshot, err = hub.Snapshot(ctx, roomID, modID)
	if err != nil {
		t.Fatalf("snapshot after revoke: %v", err)
	}
	if snapshot.Participants[1].AllowAudioUpload || snapshot.Participants[1].AllowAudioControl {
		t.Fatalf("participant grants still set after revoke: %+v", snapshot.Participants[1])
	}
}

func TestModeratorCannotGrantObserverAudioPermissions(t *testing.T) {
	hub, _, _, roomID, modID, participantID := setupRealtimeTest(t)
	ctx := context.Background()
	if err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandPeopleSetRole,
		Payload: mustJSON(t, map[string]string{"userId": participantID, "role": rooms.RoleObserver}),
	}); err != nil {
		t.Fatalf("set observer: %v", err)
	}

	err := hub.HandleCommand(ctx, roomID, modID, Command{
		Type:    CommandPeopleAudioPermission,
		Payload: mustJSON(t, map[string]any{"userId": participantID, "allowAudioControl": true}),
	})
	if !errors.Is(err, ErrBadCommand) {
		t.Fatalf("observer grant error = %v, want bad command", err)
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

func insertAudioTrack(t *testing.T, ctx context.Context, hub *Hub, roomID string, userID string, name string) string {
	t.Helper()
	content := []byte("ID3" + name)
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	path := filepath.Join(t.TempDir(), sha+".mp3")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write audio file: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := hub.db.ExecContext(ctx, `insert into audio_files (sha256, ext, size_bytes, mime_type, stored_path, uploaded_by_user_id, created_at, missing_at) values (?, 'mp3', ?, 'audio/mpeg', ?, ?, ?, null)`, sha, len(content), path, userID, now); err != nil {
		t.Fatalf("insert audio file: %v", err)
	}
	trackID := name + "-track"
	var nextOrder int
	if err := hub.db.QueryRowContext(ctx, `select coalesce(max(display_order) + 1, 0) from room_audio_tracks where room_id = ?`, roomID).Scan(&nextOrder); err != nil {
		t.Fatalf("next audio order: %v", err)
	}
	if _, err := hub.db.ExecContext(ctx, `insert into room_audio_tracks (id, room_id, sha256, original_name, display_order, uploaded_by_user_id, created_at, updated_at) values (?, ?, ?, ?, ?, ?, ?, ?)`, trackID, roomID, sha, name+".mp3", nextOrder, userID, now, now); err != nil {
		t.Fatalf("insert audio track: %v", err)
	}
	return trackID
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	bytes, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return bytes
}
