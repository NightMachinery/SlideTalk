package realtime

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/auth"
	"github.com/NightMachinery/SlideTalk/internal/rooms"
	"github.com/NightMachinery/SlideTalk/internal/store"
)

// Hub coordinates realtime room state and short-lived WebSocket tickets.
type Hub struct {
	db    *store.DB
	auth  *auth.Service
	rooms *rooms.Service

	mu             sync.Mutex
	tickets        map[string]TicketClaims
	versions       map[string]int64
	clients        map[*Client]bool
	audioLocalMode map[string]map[string]bool
}

// TicketClaims bind a WebSocket ticket to one room and user.
type TicketClaims struct {
	RoomID    string
	UserID    string
	ExpiresAt time.Time
}

// NewHub creates a realtime hub.
func NewHub(db *store.DB, authService *auth.Service, roomService *rooms.Service) *Hub {
	_, _ = db.ExecContext(context.Background(), `delete from room_online_members`)
	return &Hub{
		db:             db,
		auth:           authService,
		rooms:          roomService,
		tickets:        make(map[string]TicketClaims),
		versions:       make(map[string]int64),
		clients:        make(map[*Client]bool),
		audioLocalMode: make(map[string]map[string]bool),
	}
}

// Client is a connected realtime socket.
type Client struct {
	RoomID string
	UserID string
	Send   chan Event
}

// Register adds a socket client to the hub.
func (h *Hub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client] = true
	_, _ = h.db.ExecContext(context.Background(), `insert into room_online_members (room_id, user_id, connection_count, updated_at) values (?, ?, 1, ?) on conflict(room_id, user_id) do update set connection_count = connection_count + 1, updated_at = excluded.updated_at`, client.RoomID, client.UserID, time.Now().UTC().Format(time.RFC3339Nano))
}

// Unregister removes a socket client from the hub.
func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[client] {
		delete(h.clients, client)
		close(client.Send)
		_, _ = h.db.ExecContext(context.Background(), `update room_online_members set connection_count = max(connection_count - 1, 0), updated_at = ? where room_id = ? and user_id = ?`, time.Now().UTC().Format(time.RFC3339Nano), client.RoomID, client.UserID)
		_, _ = h.db.ExecContext(context.Background(), `delete from room_online_members where connection_count <= 0`)
		if !h.userHasClientLocked(client.RoomID, client.UserID) {
			if roomPresence, ok := h.audioLocalMode[client.RoomID]; ok {
				delete(roomPresence, client.UserID)
				if len(roomPresence) == 0 {
					delete(h.audioLocalMode, client.RoomID)
				}
			}
		}
	}
}

func (h *Hub) userHasClientLocked(roomID string, userID string) bool {
	for client := range h.clients {
		if client.RoomID == roomID && client.UserID == userID {
			return true
		}
	}
	return false
}

func (h *Hub) isAudioLocalMode(roomID string, userID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.audioLocalMode[roomID][userID]
}

func (h *Hub) setAudioLocalMode(roomID string, userID string, enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if enabled {
		if h.audioLocalMode[roomID] == nil {
			h.audioLocalMode[roomID] = make(map[string]bool)
		}
		h.audioLocalMode[roomID][userID] = true
		return
	}
	if roomPresence, ok := h.audioLocalMode[roomID]; ok {
		delete(roomPresence, userID)
		if len(roomPresence) == 0 {
			delete(h.audioLocalMode, roomID)
		}
	}
}

// IsUserOnline returns true if there is an active client connection for the user in the given room.
func (h *Hub) IsUserOnline(roomID string, userID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		if client.RoomID == roomID && client.UserID == userID {
			return true
		}
	}
	return false
}

// IssueTicket returns a one-time WebSocket ticket for a current room member.
func (h *Hub) IssueTicket(ctx context.Context, roomID string, userID string) (WSTicket, error) {
	if _, err := h.rooms.GetForUser(ctx, roomID, userID); err != nil {
		return WSTicket{}, err
	}
	ticket, err := randomToken(32)
	if err != nil {
		return WSTicket{}, err
	}
	claims := TicketClaims{RoomID: roomID, UserID: userID, ExpiresAt: time.Now().UTC().Add(60 * time.Second)}
	h.mu.Lock()
	h.tickets[ticket] = claims
	h.mu.Unlock()
	return WSTicket{Ticket: ticket, ExpiresAt: claims.ExpiresAt}, nil
}

// ConsumeTicket validates and consumes a WebSocket ticket.
func (h *Hub) ConsumeTicket(ticket string) (TicketClaims, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	claims, ok := h.tickets[ticket]
	if !ok {
		return TicketClaims{}, ErrInvalidTicket
	}
	delete(h.tickets, ticket)
	if time.Now().UTC().After(claims.ExpiresAt) {
		return TicketClaims{}, ErrInvalidTicket
	}
	return claims, nil
}

// Snapshot returns the current room state for callerUserID.
func (h *Hub) Snapshot(ctx context.Context, roomID string, callerUserID string) (Snapshot, error) {
	details, err := h.rooms.GetForUser(ctx, roomID, callerUserID)
	if err != nil {
		return Snapshot{}, err
	}
	caller, err := h.auth.GetUser(ctx, callerUserID)
	if err != nil {
		return Snapshot{}, err
	}
	var roomRow struct {
		ID                        string
		Title                     string
		PasswordHash              sql.NullString
		RoomMode                  string
		AllowParticipantMarkdown  bool
		SlidePage                 int
		SharedNavigationEnabled   bool
		AllowAudienceAudioUpload  bool
		AllowAudienceAudioControl bool
		ShowAudioStarCounts       bool
		AllowAudienceAudioTagging bool
		AudioFilterScope          string
		AudioFilterUpdatedBy      sql.NullString
		AudioFilterUpdatedAt      sql.NullString
		ExpiresAt                 sql.NullString
		AudioCurrentTrackID       sql.NullString
		AudioState                string
		AudioPositionSeconds      int
		AudioStartedAt            sql.NullString
		AudioPlaybackMode         string
		Markdown                  string
		MarkdownUpdatedByUserID   sql.NullString
		MarkdownUpdatedByName     sql.NullString
		MarkdownUpdatedAt         sql.NullString
		CurrentSpeakerUserID      sql.NullString
		TimerState                string
		TimerDurationSeconds      int
		TimerStartedAt            sql.NullString
		RaiseHandMode             string
	}
	if err := h.db.QueryRowContext(ctx, `select r.id, r.title, r.password_hash, r.room_mode, r.allow_participant_markdown, r.slide_page, r.shared_navigation_enabled, r.allow_audience_audio_upload, r.allow_audience_audio_control, r.show_audio_star_counts, r.allow_audience_audio_tagging, r.audio_filter_scope, r.audio_filter_updated_by_user_id, r.audio_filter_updated_at, r.expires_at, r.audio_current_track_id, r.audio_state, r.audio_position_seconds, r.audio_started_at, r.audio_playback_mode, r.markdown, r.markdown_updated_by_user_id, u.display_name, r.markdown_updated_at, r.current_speaker_user_id, r.timer_state, r.timer_duration_seconds, r.timer_started_at, r.raise_hand_mode from rooms r left join users u on u.id = r.markdown_updated_by_user_id where r.id = ?`, roomID).Scan(&roomRow.ID, &roomRow.Title, &roomRow.PasswordHash, &roomRow.RoomMode, &roomRow.AllowParticipantMarkdown, &roomRow.SlidePage, &roomRow.SharedNavigationEnabled, &roomRow.AllowAudienceAudioUpload, &roomRow.AllowAudienceAudioControl, &roomRow.ShowAudioStarCounts, &roomRow.AllowAudienceAudioTagging, &roomRow.AudioFilterScope, &roomRow.AudioFilterUpdatedBy, &roomRow.AudioFilterUpdatedAt, &roomRow.ExpiresAt, &roomRow.AudioCurrentTrackID, &roomRow.AudioState, &roomRow.AudioPositionSeconds, &roomRow.AudioStartedAt, &roomRow.AudioPlaybackMode, &roomRow.Markdown, &roomRow.MarkdownUpdatedByUserID, &roomRow.MarkdownUpdatedByName, &roomRow.MarkdownUpdatedAt, &roomRow.CurrentSpeakerUserID, &roomRow.TimerState, &roomRow.TimerDurationSeconds, &roomRow.TimerStartedAt, &roomRow.RaiseHandMode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Snapshot{}, rooms.ErrNotFound
		}
		return Snapshot{}, fmt.Errorf("read snapshot room: %w", err)
	}
	members, err := h.rooms.ListMembers(ctx, roomID)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{
		Room: SnapshotRoom{
			ID:                        roomRow.ID,
			Title:                     roomRow.Title,
			HasPassword:               roomRow.PasswordHash.Valid,
			RoomMode:                  roomRow.RoomMode,
			AllowParticipantMarkdown:  roomRow.AllowParticipantMarkdown,
			RaiseHandMode:             roomRow.RaiseHandMode,
			SlidePage:                 roomRow.SlidePage,
			SharedNavigationEnabled:   roomRow.SharedNavigationEnabled,
			AllowAudienceAudioUpload:  roomRow.AllowAudienceAudioUpload,
			AllowAudienceAudioControl: roomRow.AllowAudienceAudioControl,
			ShowAudioStarCounts:       roomRow.ShowAudioStarCounts,
			AllowAudienceAudioTagging: roomRow.AllowAudienceAudioTagging,
			NeverExpires:              !roomRow.ExpiresAt.Valid,
		},
		Caller: SnapshotCaller{
			UserID:  callerUserID,
			Role:    details.Membership.Role,
			IsAdmin: caller.IsAdmin,
		},
		Participants: []SnapshotMember{},
		Observers:    []SnapshotMember{},
		Hands:        []SnapshotHand{},
		Timer: SnapshotTimer{
			State:           roomRow.TimerState,
			DurationSeconds: roomRow.TimerDurationSeconds,
			ServerNow:       time.Now().UTC().Format(time.RFC3339Nano),
		},
		Markdown: roomRow.Markdown,
		Audio: SnapshotAudio{
			Tracks:          []SnapshotAudioTrack{},
			State:           roomRow.AudioState,
			PositionSeconds: roomRow.AudioPositionSeconds,
			ServerNow:       time.Now().UTC().Format(time.RFC3339Nano),
			PlaybackMode:    roomRow.AudioPlaybackMode,
			FilterScope:     parseAudioFilterScope(roomRow.AudioFilterScope, roomRow.AudioFilterUpdatedBy, roomRow.AudioFilterUpdatedAt),
		},
	}
	if roomRow.AudioCurrentTrackID.Valid {
		snapshot.Audio.CurrentTrackID = roomRow.AudioCurrentTrackID.String
	}
	if roomRow.ExpiresAt.Valid {
		snapshot.Room.ExpiresAt = roomRow.ExpiresAt.String
	}
	if roomRow.AudioStartedAt.Valid {
		snapshot.Audio.StartedAt = &roomRow.AudioStartedAt.String
	}
	if roomRow.MarkdownUpdatedByUserID.Valid {
		snapshot.MarkdownUpdatedByUserID = roomRow.MarkdownUpdatedByUserID.String
	}
	if roomRow.MarkdownUpdatedByName.Valid {
		snapshot.MarkdownUpdatedByName = roomRow.MarkdownUpdatedByName.String
	}
	if roomRow.MarkdownUpdatedAt.Valid {
		snapshot.MarkdownUpdatedAt = roomRow.MarkdownUpdatedAt.String
	}
	if roomRow.TimerStartedAt.Valid {
		snapshot.Timer.StartedAt = &roomRow.TimerStartedAt.String
	}
	for _, member := range members {
		snapshotMember := SnapshotMember{
			UserID:            member.UserID,
			DisplayName:       member.DisplayName,
			Role:              member.Role,
			DisplayOrder:      member.DisplayOrder,
			IsOnline:          h.IsUserOnline(roomID, member.UserID),
			AllowAudioUpload:  member.AllowAudioUpload,
			AllowAudioControl: member.AllowAudioControl,
			AllowAudioTagging: member.AllowAudioTagging,
			AudioLocalMode:    h.isAudioLocalMode(roomID, member.UserID),
		}
		if member.Role == rooms.RoleObserver {
			snapshot.Observers = append(snapshot.Observers, snapshotMember)
		} else {
			snapshot.Participants = append(snapshot.Participants, snapshotMember)
		}
	}
	if roomRow.CurrentSpeakerUserID.Valid && participantIndex(snapshot.Participants, roomRow.CurrentSpeakerUserID.String) >= 0 {
		snapshot.CurrentTurn.CurrentSpeakerUserID = roomRow.CurrentSpeakerUserID.String
		snapshot.CurrentTurn.NextSpeakerUserID = nextParticipant(snapshot.Participants, roomRow.CurrentSpeakerUserID.String, 1)
	} else if len(snapshot.Participants) > 0 {
		snapshot.CurrentTurn.NextSpeakerUserID = snapshot.Participants[0].UserID
	}
	hands, err := h.listHands(ctx, roomID)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Hands = hands
	slide, err := h.roomSlide(ctx, roomID)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Slide = slide
	audioTracks, err := h.listAudioTracks(ctx, roomID, callerUserID, roomRow.ShowAudioStarCounts)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Audio.Tracks = audioTracks
	scopedAudioTracks := audioTracksMatchingScope(audioTracks, snapshot.Audio.FilterScope)
	snapshot.Audio.NextTrackID = nextAudioTrackID(roomID, scopedAudioTracks, snapshot.Audio.CurrentTrackID, snapshot.Audio.PlaybackMode)
	return snapshot, nil
}

// HandleCommand applies one realtime command.
func (h *Hub) HandleCommand(ctx context.Context, roomID string, callerUserID string, command Command) error {
	details, err := h.rooms.GetForUser(ctx, roomID, callerUserID)
	if err != nil {
		return err
	}
	isMod := details.Membership.Role == rooms.RoleMod
	switch command.Type {
	case CommandPresenceAudioLocalMode:
		var payload struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		h.setAudioLocalMode(roomID, callerUserID, payload.Enabled)
	case CommandPeopleReorder:
		if !isMod {
			return ErrForbidden
		}
		var payload struct {
			OrderedUserIDs  []string `json:"orderedUserIds"`
			ObserverUserIDs []string `json:"observerUserIds"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if err := h.rooms.Reorder(ctx, roomID, payload.OrderedUserIDs, payload.ObserverUserIDs); err != nil {
			return err
		}
	case CommandPeopleSetRole:
		var payload struct {
			UserID string `json:"userId"`
			Role   string `json:"role"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		isObserverSelfRejoin := details.Membership.Role == rooms.RoleObserver && payload.UserID == callerUserID && payload.Role == rooms.RoleParticipant
		if !isMod && !isObserverSelfRejoin {
			return ErrForbidden
		}
		if err := h.rooms.SetRole(ctx, roomID, payload.UserID, payload.Role); err != nil {
			return err
		}
		if err := h.cleanMemberTurnState(ctx, roomID, payload.UserID); err != nil {
			return err
		}
	case CommandPeopleAudioPermission:
		if !isMod {
			return ErrForbidden
		}
		var payload struct {
			UserID            string `json:"userId"`
			AllowAudioUpload  *bool  `json:"allowAudioUpload"`
			AllowAudioControl *bool  `json:"allowAudioControl"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil || payload.UserID == "" || (payload.AllowAudioUpload == nil && payload.AllowAudioControl == nil) {
			return ErrBadCommand
		}
		if err := h.setMemberAudioPermissions(ctx, roomID, payload.UserID, payload.AllowAudioUpload, payload.AllowAudioControl); err != nil {
			return err
		}
	case CommandPeopleTagPermission:
		if !isMod {
			return ErrForbidden
		}
		var payload struct {
			UserID            string `json:"userId"`
			AllowAudioTagging *bool  `json:"allowAudioTagging"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil || payload.UserID == "" || payload.AllowAudioTagging == nil {
			return ErrBadCommand
		}
		if err := h.setMemberAudioTaggingPermission(ctx, roomID, payload.UserID, *payload.AllowAudioTagging); err != nil {
			return err
		}
	case CommandPeopleKick:
		if !isMod {
			return ErrForbidden
		}
		var payload struct {
			UserID string `json:"userId"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if err := h.rooms.Kick(ctx, roomID, payload.UserID); err != nil {
			return err
		}
		if err := h.cleanMemberTurnState(ctx, roomID, payload.UserID); err != nil {
			return err
		}
		h.SendToUser(roomID, payload.UserID, Event{Type: EventKicked, RoomID: roomID, Code: "removed", Message: "You've been removed from that room."})
	case CommandTurnNext:
		if !isMod {
			return ErrForbidden
		}
		if err := h.nextTurn(ctx, roomID); err != nil {
			return err
		}
	case CommandTurnPrevious:
		if !isMod {
			return ErrForbidden
		}
		if err := h.previousTurn(ctx, roomID); err != nil {
			return err
		}
	case CommandTurnSetCurrent:
		if !isMod {
			return ErrForbidden
		}
		var payload struct {
			UserID string `json:"userId"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if err := h.setCurrentSpeaker(ctx, roomID, payload.UserID); err != nil {
			return err
		}
	case CommandTimerStart:
		if !isMod {
			return ErrForbidden
		}
		var payload struct {
			DurationSeconds int `json:"durationSeconds"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if payload.DurationSeconds < 1 || payload.DurationSeconds > 86400 {
			return ErrBadCommand
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := h.db.ExecContext(ctx, `update rooms set timer_state = ?, timer_duration_seconds = ?, timer_started_at = ? where id = ?`, TimerStateRunning, payload.DurationSeconds, now, roomID); err != nil {
			return fmt.Errorf("start timer: %w", err)
		}
	case CommandTimerStop:
		if !isMod {
			return ErrForbidden
		}
		if err := h.stopTimer(ctx, roomID); err != nil {
			return err
		}
	case CommandTimerReset:
		if !isMod {
			return ErrForbidden
		}
		if _, err := h.db.ExecContext(ctx, `update rooms set timer_state = ?, timer_duration_seconds = 0, timer_started_at = null where id = ?`, TimerStateStopped, roomID); err != nil {
			return fmt.Errorf("reset timer: %w", err)
		}
	case CommandHandRaise:
		if details.Membership.Role == rooms.RoleObserver {
			return ErrForbidden
		}
		if err := h.raiseHand(ctx, roomID, callerUserID); err != nil {
			return err
		}
	case CommandHandLower:
		var payload struct {
			UserID string `json:"userId"`
		}
		if len(command.Payload) > 0 {
			if err := json.Unmarshal(command.Payload, &payload); err != nil {
				return ErrBadCommand
			}
		}
		targetUserID := payload.UserID
		if targetUserID == "" {
			targetUserID = callerUserID
		}
		if !isMod && targetUserID != callerUserID {
			return ErrForbidden
		}
		if _, err := h.db.ExecContext(ctx, `delete from raised_hands where room_id = ? and user_id = ?`, roomID, targetUserID); err != nil {
			return fmt.Errorf("lower hand: %w", err)
		}
	case CommandSlideNavigate:
		if !isMod {
			return ErrForbidden
		}
		var payload struct {
			Page                       int  `json:"page"`
			ModSharedNavigationEnabled bool `json:"modSharedNavigationEnabled"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if payload.Page < 1 {
			return ErrBadCommand
		}
		if !payload.ModSharedNavigationEnabled {
			return ErrNoBroadcast
		}
		if _, err := h.db.ExecContext(ctx, `update rooms set slide_page = ? where id = ?`, payload.Page, roomID); err != nil {
			return fmt.Errorf("navigate slide: %w", err)
		}
	case CommandMarkdownUpdate:
		if !isMod {
			var allow bool
			if err := h.db.QueryRowContext(ctx, `select allow_participant_markdown from rooms where id = ?`, roomID).Scan(&allow); err != nil {
				return fmt.Errorf("read markdown setting: %w", err)
			}
			if !allow || details.Membership.Role == rooms.RoleObserver {
				return ErrForbidden
			}
		}
		var payload struct {
			Markdown string `json:"markdown"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if len([]byte(payload.Markdown)) > 64*1024 {
			return ErrBadCommand
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := h.db.ExecContext(ctx, `update rooms set markdown = ?, markdown_updated_by_user_id = ?, markdown_updated_at = ? where id = ?`, payload.Markdown, callerUserID, now, roomID); err != nil {
			return fmt.Errorf("update markdown: %w", err)
		}
	case CommandSettingsUpdate:
		if !isMod {
			return ErrForbidden
		}
		var payload struct {
			RaiseHandMode             *string `json:"raiseHandMode"`
			SharedNavigationEnabled   *bool   `json:"sharedNavigationEnabled"`
			RoomMode                  *string `json:"roomMode"`
			AllowParticipantMarkdown  *bool   `json:"allowParticipantMarkdown"`
			AllowAudienceAudioUpload  *bool   `json:"allowAudienceAudioUpload"`
			AllowAudienceAudioControl *bool   `json:"allowAudienceAudioControl"`
			ShowAudioStarCounts       *bool   `json:"showAudioStarCounts"`
			AllowAudienceAudioTagging *bool   `json:"allowAudienceAudioTagging"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if payload.RaiseHandMode != nil {
			if *payload.RaiseHandMode != RaiseHandModeOff && *payload.RaiseHandMode != RaiseHandModeManual && *payload.RaiseHandMode != RaiseHandModeQueue {
				return ErrBadCommand
			}
			if _, err := h.db.ExecContext(ctx, `update rooms set raise_hand_mode = ? where id = ?`, *payload.RaiseHandMode, roomID); err != nil {
				return fmt.Errorf("update raise hand mode: %w", err)
			}
		}
		if payload.SharedNavigationEnabled != nil {
			if _, err := h.db.ExecContext(ctx, `update rooms set shared_navigation_enabled = ? where id = ?`, *payload.SharedNavigationEnabled, roomID); err != nil {
				return fmt.Errorf("update shared navigation: %w", err)
			}
		}
		if payload.RoomMode != nil {
			if !validRoomMode(*payload.RoomMode) {
				return ErrBadCommand
			}
			if _, err := h.db.ExecContext(ctx, `update rooms set room_mode = ? where id = ?`, *payload.RoomMode, roomID); err != nil {
				return fmt.Errorf("update room mode: %w", err)
			}
		}
		if payload.AllowParticipantMarkdown != nil {
			if _, err := h.db.ExecContext(ctx, `update rooms set allow_participant_markdown = ? where id = ?`, *payload.AllowParticipantMarkdown, roomID); err != nil {
				return fmt.Errorf("update participant markdown setting: %w", err)
			}
		}
		if payload.AllowAudienceAudioUpload != nil {
			if _, err := h.db.ExecContext(ctx, `update rooms set allow_audience_audio_upload = ? where id = ?`, *payload.AllowAudienceAudioUpload, roomID); err != nil {
				return fmt.Errorf("update audience audio upload: %w", err)
			}
		}
		if payload.AllowAudienceAudioControl != nil {
			if _, err := h.db.ExecContext(ctx, `update rooms set allow_audience_audio_control = ? where id = ?`, *payload.AllowAudienceAudioControl, roomID); err != nil {
				return fmt.Errorf("update audience audio control: %w", err)
			}
		}
		if payload.ShowAudioStarCounts != nil {
			if _, err := h.db.ExecContext(ctx, `update rooms set show_audio_star_counts = ? where id = ?`, *payload.ShowAudioStarCounts, roomID); err != nil {
				return fmt.Errorf("update audio star count setting: %w", err)
			}
		}
		if payload.AllowAudienceAudioTagging != nil {
			if _, err := h.db.ExecContext(ctx, `update rooms set allow_audience_audio_tagging = ? where id = ?`, *payload.AllowAudienceAudioTagging, roomID); err != nil {
				return fmt.Errorf("update audience audio tagging: %w", err)
			}
		}
		if payload.RaiseHandMode != nil && *payload.RaiseHandMode == RaiseHandModeOff {
			if _, err := h.db.ExecContext(ctx, `delete from raised_hands where room_id = ?`, roomID); err != nil {
				return fmt.Errorf("clear hands: %w", err)
			}
		}
	case CommandAudioPlay:
		if err := h.requireAudioControl(ctx, roomID, callerUserID, details.Membership.Role, isMod); err != nil {
			return err
		}
		var payload struct {
			TrackID         string                    `json:"trackId"`
			PositionSeconds int                       `json:"positionSeconds"`
			FilterScope     *SnapshotAudioFilterScope `json:"filterScope"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if payload.TrackID == "" {
			payload.TrackID = currentAudioTrackID(ctx, h.db, roomID)
		}
		if payload.TrackID == "" || payload.PositionSeconds < 0 {
			return ErrBadCommand
		}
		if err := h.ensureAudioTrack(ctx, roomID, payload.TrackID); err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		filterJSON := ""
		if payload.FilterScope != nil {
			filterJSON = marshalAudioFilterScope(*payload.FilterScope)
		}
		if _, err := h.db.ExecContext(ctx, `update rooms set audio_current_track_id = ?, audio_state = ?, audio_position_seconds = ?, audio_started_at = ?, audio_filter_scope = ?, audio_filter_updated_by_user_id = ?, audio_filter_updated_at = ? where id = ?`, payload.TrackID, AudioStatePlaying, payload.PositionSeconds, now, filterJSON, callerUserID, now, roomID); err != nil {
			return fmt.Errorf("play audio: %w", err)
		}
	case CommandAudioPause:
		if err := h.requireAudioControl(ctx, roomID, callerUserID, details.Membership.Role, isMod); err != nil {
			return err
		}
		position, err := h.currentAudioPosition(ctx, roomID)
		if err != nil {
			return err
		}
		if _, err := h.db.ExecContext(ctx, `update rooms set audio_state = ?, audio_position_seconds = ?, audio_started_at = null where id = ?`, AudioStatePaused, position, roomID); err != nil {
			return fmt.Errorf("pause audio: %w", err)
		}
	case CommandAudioSeek:
		if err := h.requireAudioControl(ctx, roomID, callerUserID, details.Membership.Role, isMod); err != nil {
			return err
		}
		var payload struct {
			PositionSeconds int `json:"positionSeconds"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil || payload.PositionSeconds < 0 {
			return ErrBadCommand
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := h.db.ExecContext(ctx, `update rooms set audio_position_seconds = ?, audio_started_at = case when audio_state = ? then ? else null end where id = ?`, payload.PositionSeconds, AudioStatePlaying, now, roomID); err != nil {
			return fmt.Errorf("seek audio: %w", err)
		}
	case CommandAudioSelect:
		if err := h.requireAudioControl(ctx, roomID, callerUserID, details.Membership.Role, isMod); err != nil {
			return err
		}
		var payload struct {
			TrackID string `json:"trackId"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil || payload.TrackID == "" {
			return ErrBadCommand
		}
		if err := h.ensureAudioTrack(ctx, roomID, payload.TrackID); err != nil {
			return err
		}
		if _, err := h.db.ExecContext(ctx, `update rooms set audio_current_track_id = ?, audio_state = ?, audio_position_seconds = 0, audio_started_at = null where id = ?`, payload.TrackID, AudioStatePaused, roomID); err != nil {
			return fmt.Errorf("select audio: %w", err)
		}
	case CommandAudioReorder:
		if !isMod {
			return ErrForbidden
		}
		var payload struct {
			TrackIDs        []string `json:"trackIds"`
			VisibleTrackIDs []string `json:"visibleTrackIds"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if err := h.reorderAudio(ctx, roomID, payload.TrackIDs, payload.VisibleTrackIDs); err != nil {
			return err
		}
	case CommandAudioMode:
		if err := h.requireAudioControl(ctx, roomID, callerUserID, details.Membership.Role, isMod); err != nil {
			return err
		}
		var payload struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil || !validAudioMode(payload.Mode) {
			return ErrBadCommand
		}
		if _, err := h.db.ExecContext(ctx, `update rooms set audio_playback_mode = ? where id = ?`, payload.Mode, roomID); err != nil {
			return fmt.Errorf("update audio mode: %w", err)
		}
	case CommandAudioEnded:
		if err := h.advanceAudioEnded(ctx, roomID); err != nil {
			return err
		}
	case CommandAudioFilterScope:
		if err := h.requireAudioControl(ctx, roomID, callerUserID, details.Membership.Role, isMod); err != nil {
			return err
		}
		var payload SnapshotAudioFilterScope
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := h.db.ExecContext(ctx, `update rooms set audio_filter_scope = ?, audio_filter_updated_by_user_id = ?, audio_filter_updated_at = ? where id = ?`, marshalAudioFilterScope(payload), callerUserID, now, roomID); err != nil {
			return fmt.Errorf("update audio filter scope: %w", err)
		}
	case CommandAudioTag:
		var payload struct {
			TrackID string `json:"trackId"`
			Tag     string `json:"tag"`
			Tagged  bool   `json:"tagged"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil || payload.TrackID == "" || strings.TrimSpace(payload.Tag) == "" {
			return ErrBadCommand
		}
		if err := h.setAudioTag(ctx, roomID, payload.TrackID, callerUserID, details.Membership.Role, isMod, payload.Tag, payload.Tagged); err != nil {
			return err
		}
	case CommandAudioStar:
		var payload struct {
			TrackID string `json:"trackId"`
			Starred bool   `json:"starred"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil || payload.TrackID == "" {
			return ErrBadCommand
		}
		if details.Membership.Role == rooms.RoleObserver {
			return ErrForbidden
		}
		if err := h.setAudioStar(ctx, roomID, payload.TrackID, callerUserID, payload.Starred); err != nil {
			return err
		}
	default:
		return ErrBadCommand
	}
	h.incrementVersion(roomID)
	return nil
}

func (h *Hub) nextTurn(ctx context.Context, roomID string) error {
	mode, err := h.raiseHandMode(ctx, roomID)
	if err != nil {
		return err
	}
	if mode == RaiseHandModeQueue {
		hand, ok, err := h.earliestRaisedHand(ctx, roomID)
		if err != nil {
			return err
		}
		if ok {
			if err := h.setCurrentSpeaker(ctx, roomID, hand.UserID); err != nil {
				return err
			}
			if _, err := h.db.ExecContext(ctx, `delete from raised_hands where room_id = ? and user_id = ?`, roomID, hand.UserID); err != nil {
				return fmt.Errorf("clear queued hand: %w", err)
			}
			return nil
		}
	}
	participants, current, err := h.turnParticipants(ctx, roomID)
	if err != nil {
		return err
	}
	if len(participants) == 0 {
		return nil
	}
	next := participants[0].UserID
	if current.Valid {
		next = nextParticipant(participants, current.String, 1)
		if next == "" {
			next = participants[0].UserID
		}
	}
	return h.setCurrentSpeaker(ctx, roomID, next)
}

func (h *Hub) previousTurn(ctx context.Context, roomID string) error {
	participants, current, err := h.turnParticipants(ctx, roomID)
	if err != nil {
		return err
	}
	if len(participants) == 0 {
		return nil
	}
	previous := participants[len(participants)-1].UserID
	if current.Valid {
		previous = nextParticipant(participants, current.String, -1)
		if previous == "" {
			previous = participants[len(participants)-1].UserID
		}
	}
	return h.setCurrentSpeaker(ctx, roomID, previous)
}

func (h *Hub) turnParticipants(ctx context.Context, roomID string) ([]SnapshotMember, sql.NullString, error) {
	var current sql.NullString
	if err := h.db.QueryRowContext(ctx, `select current_speaker_user_id from rooms where id = ?`, roomID).Scan(&current); err != nil {
		return nil, sql.NullString{}, fmt.Errorf("read current speaker: %w", err)
	}
	members, err := h.rooms.ListMembers(ctx, roomID)
	if err != nil {
		return nil, sql.NullString{}, err
	}
	participants := make([]SnapshotMember, 0, len(members))
	for _, member := range members {
		if member.Role == rooms.RoleObserver {
			continue
		}
		participants = append(participants, SnapshotMember{UserID: member.UserID, DisplayName: member.DisplayName, Role: member.Role, DisplayOrder: member.DisplayOrder})
	}
	return participants, current, nil
}

func (h *Hub) setCurrentSpeaker(ctx context.Context, roomID string, userID string) error {
	if userID == "" {
		return ErrBadCommand
	}
	var role string
	err := h.db.QueryRowContext(ctx, `select role from room_members where room_id = ? and user_id = ? and kicked_at is null`, roomID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return rooms.ErrNotMember
	}
	if err != nil {
		return fmt.Errorf("read current speaker member: %w", err)
	}
	if role == rooms.RoleObserver {
		return ErrBadCommand
	}
	if _, err := h.db.ExecContext(ctx, `update rooms set current_speaker_user_id = ? where id = ?`, userID, roomID); err != nil {
		return fmt.Errorf("set current speaker: %w", err)
	}
	return nil
}

func (h *Hub) raiseHand(ctx context.Context, roomID string, userID string) error {
	mode, err := h.raiseHandMode(ctx, roomID)
	if err != nil {
		return err
	}
	if mode == RaiseHandModeOff {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.db.ExecContext(ctx, `insert into raised_hands (room_id, user_id, raised_at) values (?, ?, ?) on conflict(room_id, user_id) do nothing`, roomID, userID, now); err != nil {
		return fmt.Errorf("raise hand: %w", err)
	}
	return nil
}

func (h *Hub) stopTimer(ctx context.Context, roomID string) error {
	var state string
	var durationSeconds int
	var startedAt sql.NullString
	if err := h.db.QueryRowContext(ctx, `select timer_state, timer_duration_seconds, timer_started_at from rooms where id = ?`, roomID).Scan(&state, &durationSeconds, &startedAt); err != nil {
		return fmt.Errorf("read timer: %w", err)
	}
	remaining := durationSeconds
	if state == TimerStateRunning && startedAt.Valid {
		started, err := time.Parse(time.RFC3339Nano, startedAt.String)
		if err != nil {
			return ErrBadCommand
		}
		elapsed := int(time.Since(started).Seconds())
		remaining = max(durationSeconds-elapsed, 0)
	}
	if _, err := h.db.ExecContext(ctx, `update rooms set timer_state = ?, timer_duration_seconds = ?, timer_started_at = null where id = ?`, TimerStateStopped, remaining, roomID); err != nil {
		return fmt.Errorf("stop timer: %w", err)
	}
	return nil
}

func (h *Hub) raiseHandMode(ctx context.Context, roomID string) (string, error) {
	var mode string
	if err := h.db.QueryRowContext(ctx, `select raise_hand_mode from rooms where id = ?`, roomID).Scan(&mode); err != nil {
		return "", fmt.Errorf("read raise hand mode: %w", err)
	}
	return mode, nil
}

func (h *Hub) earliestRaisedHand(ctx context.Context, roomID string) (SnapshotHand, bool, error) {
	hands, err := h.listHands(ctx, roomID)
	if err != nil {
		return SnapshotHand{}, false, err
	}
	if len(hands) == 0 {
		return SnapshotHand{}, false, nil
	}
	return hands[0], true, nil
}

func (h *Hub) listHands(ctx context.Context, roomID string) ([]SnapshotHand, error) {
	rows, err := h.db.QueryContext(
		ctx,
		`select rh.user_id, u.display_name, rh.raised_at
		 from raised_hands rh
		 join room_members rm on rm.room_id = rh.room_id and rm.user_id = rh.user_id
		 join users u on u.id = rh.user_id
		 where rh.room_id = ? and rm.kicked_at is null and rm.role <> ?
		 order by rh.raised_at asc`,
		roomID,
		rooms.RoleObserver,
	)
	if err != nil {
		return nil, fmt.Errorf("list hands: %w", err)
	}
	defer rows.Close()
	hands := []SnapshotHand{}
	for rows.Next() {
		var hand SnapshotHand
		if err := rows.Scan(&hand.UserID, &hand.DisplayName, &hand.RaisedAt); err != nil {
			return nil, fmt.Errorf("scan hand: %w", err)
		}
		hands = append(hands, hand)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hands: %w", err)
	}
	return hands, nil
}

func (h *Hub) cleanMemberTurnState(ctx context.Context, roomID string, userID string) error {
	if _, err := h.db.ExecContext(ctx, `delete from raised_hands where room_id = ? and user_id = ?`, roomID, userID); err != nil {
		return fmt.Errorf("clean hand state: %w", err)
	}
	var current sql.NullString
	if err := h.db.QueryRowContext(ctx, `select current_speaker_user_id from rooms where id = ?`, roomID).Scan(&current); err != nil {
		return fmt.Errorf("read clean current speaker: %w", err)
	}
	if current.Valid && current.String == userID {
		if _, err := h.db.ExecContext(ctx, `update rooms set current_speaker_user_id = null where id = ?`, roomID); err != nil {
			return fmt.Errorf("clean current speaker: %w", err)
		}
	}
	return nil
}

func (h *Hub) roomSlide(ctx context.Context, roomID string) (*SnapshotSlide, error) {
	var slide SnapshotSlide
	var storedPath string
	var missingAt sql.NullString
	err := h.db.QueryRowContext(
		ctx,
		`select rs.sha256, rs.original_name, rs.expires_at, sf.mime_type, sf.stored_path, sf.missing_at
		 from room_slides rs
		 join slide_files sf on sf.sha256 = rs.sha256
		 where rs.room_id = ?`,
		roomID,
	).Scan(&slide.SHA256, &slide.OriginalName, &slide.ExpiresAt, &slide.MIMEType, &storedPath, &missingAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read room slide: %w", err)
	}
	if _, err := os.Stat(storedPath); errors.Is(err, os.ErrNotExist) {
		slide.Missing = true
		if !missingAt.Valid {
			if _, err := h.db.ExecContext(ctx, `update slide_files set missing_at = ? where sha256 = ?`, time.Now().UTC().Format(time.RFC3339Nano), slide.SHA256); err != nil {
				return nil, fmt.Errorf("mark snapshot slide missing: %w", err)
			}
		}
	} else if err != nil {
		return nil, fmt.Errorf("stat room slide: %w", err)
	}
	return &slide, nil
}

func (h *Hub) listAudioTracks(ctx context.Context, roomID string, callerUserID string, showStarCounts bool) ([]SnapshotAudioTrack, error) {
	rows, err := h.db.QueryContext(ctx, `select rat.id, rat.sha256, rat.original_name, rat.title, af.metadata_title, af.mime_type, af.size_bytes, af.duration_seconds, af.cover_path, rat.uploaded_by_user_id, u.display_name, rat.uploader_display_name, rat.display_order, af.stored_path, af.missing_at
		from room_audio_tracks rat join audio_files af on af.sha256 = rat.sha256
		left join users u on u.id = rat.uploaded_by_user_id
		where rat.room_id = ?
		order by rat.display_order asc, rat.created_at asc`, roomID)
	if err != nil {
		return nil, fmt.Errorf("list snapshot audio: %w", err)
	}
	defer rows.Close()
	tracks := []SnapshotAudioTrack{}
	for rows.Next() {
		var track SnapshotAudioTrack
		var storedPath string
		var coverPath string
		var missingAt sql.NullString
		if err := rows.Scan(&track.ID, &track.SHA256, &track.OriginalName, &track.Title, &track.MetadataTitle, &track.MIMEType, &track.SizeBytes, &track.DurationSeconds, &coverPath, &track.UploadedByUserID, &track.UploadedByName, &track.UploaderDisplayName, &track.DisplayOrder, &storedPath, &missingAt); err != nil {
			return nil, fmt.Errorf("scan snapshot audio: %w", err)
		}
		track.HasCover = coverPath != ""
		if _, err := os.Stat(storedPath); errors.Is(err, os.ErrNotExist) {
			track.Missing = true
			if !missingAt.Valid {
				if _, err := h.db.ExecContext(ctx, `update audio_files set missing_at = ? where sha256 = ?`, time.Now().UTC().Format(time.RFC3339Nano), track.SHA256); err != nil {
					return nil, fmt.Errorf("mark snapshot audio missing: %w", err)
				}
			}
		} else if err != nil {
			return nil, fmt.Errorf("stat snapshot audio: %w", err)
		}
		tracks = append(tracks, track)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot audio: %w", err)
	}
	if callerUserID != "" && len(tracks) > 0 {
		starredRows, err := h.db.QueryContext(ctx, `select track_id from room_audio_track_stars where room_id = ? and user_id = ?`, roomID, callerUserID)
		if err != nil {
			return nil, fmt.Errorf("list caller audio stars: %w", err)
		}
		starred := map[string]bool{}
		for starredRows.Next() {
			var trackID string
			if err := starredRows.Scan(&trackID); err != nil {
				_ = starredRows.Close()
				return nil, fmt.Errorf("scan caller audio star: %w", err)
			}
			starred[trackID] = true
		}
		if err := starredRows.Close(); err != nil {
			return nil, fmt.Errorf("close caller audio stars: %w", err)
		}
		for index := range tracks {
			tracks[index].StarredByCaller = starred[tracks[index].ID]
		}
	}
	if err := h.attachAudioTags(ctx, roomID, tracks); err != nil {
		return nil, err
	}
	if showStarCounts && len(tracks) > 0 {
		countRows, err := h.db.QueryContext(ctx, `select track_id, count(*) from room_audio_track_stars where room_id = ? group by track_id`, roomID)
		if err != nil {
			return nil, fmt.Errorf("list audio star counts: %w", err)
		}
		counts := map[string]int{}
		for countRows.Next() {
			var trackID string
			var count int
			if err := countRows.Scan(&trackID, &count); err != nil {
				_ = countRows.Close()
				return nil, fmt.Errorf("scan audio star count: %w", err)
			}
			counts[trackID] = count
		}
		if err := countRows.Close(); err != nil {
			return nil, fmt.Errorf("close audio star counts: %w", err)
		}
		for index := range tracks {
			tracks[index].StarCount = counts[tracks[index].ID]
		}
	}
	return tracks, nil
}

func (h *Hub) setMemberAudioPermissions(ctx context.Context, roomID string, userID string, allowUpload *bool, allowControl *bool) error {
	var role string
	err := h.db.QueryRowContext(ctx, `select role from room_members where room_id = ? and user_id = ? and kicked_at is null`, roomID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return rooms.ErrNotMember
	}
	if err != nil {
		return fmt.Errorf("read audio permission member: %w", err)
	}
	if role == rooms.RoleObserver {
		return ErrBadCommand
	}
	if allowUpload != nil {
		if _, err := h.db.ExecContext(ctx, `update room_members set allow_audio_upload = ? where room_id = ? and user_id = ? and kicked_at is null`, *allowUpload, roomID, userID); err != nil {
			return fmt.Errorf("update member audio upload permission: %w", err)
		}
	}
	if allowControl != nil {
		if _, err := h.db.ExecContext(ctx, `update room_members set allow_audio_control = ? where room_id = ? and user_id = ? and kicked_at is null`, *allowControl, roomID, userID); err != nil {
			return fmt.Errorf("update member audio control permission: %w", err)
		}
	}
	return nil
}

func (h *Hub) requireAudioControl(ctx context.Context, roomID string, userID string, role string, isMod bool) error {
	if isMod {
		return nil
	}
	if role == rooms.RoleObserver {
		return ErrForbidden
	}
	var allow, personalAllow bool
	if err := h.db.QueryRowContext(ctx, `select r.allow_audience_audio_control, rm.allow_audio_control from rooms r join room_members rm on rm.room_id = r.id where r.id = ? and rm.user_id = ? and rm.kicked_at is null`, roomID, userID).Scan(&allow, &personalAllow); err != nil {
		return fmt.Errorf("read audio control setting: %w", err)
	}
	if !allow && !personalAllow {
		return ErrForbidden
	}
	return nil
}

func (h *Hub) ensureAudioTrack(ctx context.Context, roomID string, trackID string) error {
	var exists int
	err := h.db.QueryRowContext(ctx, `select 1 from room_audio_tracks where room_id = ? and id = ?`, roomID, trackID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBadCommand
	}
	if err != nil {
		return fmt.Errorf("read audio track: %w", err)
	}
	return nil
}

func (h *Hub) currentAudioPosition(ctx context.Context, roomID string) (int, error) {
	var state string
	var position int
	var startedAt sql.NullString
	if err := h.db.QueryRowContext(ctx, `select audio_state, audio_position_seconds, audio_started_at from rooms where id = ?`, roomID).Scan(&state, &position, &startedAt); err != nil {
		return 0, fmt.Errorf("read audio position: %w", err)
	}
	if state == AudioStatePlaying && startedAt.Valid {
		started, err := time.Parse(time.RFC3339Nano, startedAt.String)
		if err != nil {
			return 0, ErrBadCommand
		}
		position += int(time.Since(started).Seconds())
	}
	return max(position, 0), nil
}

func (h *Hub) reorderAudio(ctx context.Context, roomID string, trackIDs []string, visibleTrackIDs []string) error {
	existing, err := h.listAudioTracks(ctx, roomID, "", false)
	if err != nil {
		return err
	}
	if len(visibleTrackIDs) > 0 {
		trackIDs = mergeVisibleAudioReorder(existing, trackIDs, visibleTrackIDs)
	}
	if len(trackIDs) != len(existing) {
		return ErrBadCommand
	}
	known := map[string]bool{}
	for _, track := range existing {
		known[track.ID] = true
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin audio reorder: %w", err)
	}
	defer rollback(tx)
	for index, trackID := range trackIDs {
		if !known[trackID] {
			return ErrBadCommand
		}
		delete(known, trackID)
		if _, err := tx.ExecContext(ctx, `update room_audio_tracks set display_order = ?, updated_at = ? where room_id = ? and id = ?`, index, time.Now().UTC().Format(time.RFC3339Nano), roomID, trackID); err != nil {
			return fmt.Errorf("reorder audio: %w", err)
		}
	}
	if len(known) != 0 {
		return ErrBadCommand
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit audio reorder: %w", err)
	}
	return nil
}

func mergeVisibleAudioReorder(existing []SnapshotAudioTrack, orderedVisibleIDs []string, previousVisibleIDs []string) []string {
	if len(orderedVisibleIDs) != len(previousVisibleIDs) {
		return orderedVisibleIDs
	}
	visibleSet := map[string]bool{}
	for _, id := range previousVisibleIDs {
		visibleSet[id] = true
	}
	ordered := slices.Clone(orderedVisibleIDs)
	merged := make([]string, 0, len(existing))
	visibleIndex := 0
	for _, track := range existing {
		if visibleSet[track.ID] {
			if visibleIndex >= len(ordered) {
				return orderedVisibleIDs
			}
			merged = append(merged, ordered[visibleIndex])
			visibleIndex++
		} else {
			merged = append(merged, track.ID)
		}
	}
	return merged
}

func (h *Hub) advanceAudioEnded(ctx context.Context, roomID string) error {
	var currentID string
	var mode string
	var rawScope string
	var updatedBy sql.NullString
	var updatedAt sql.NullString
	if err := h.db.QueryRowContext(ctx, `select coalesce(audio_current_track_id, ''), audio_playback_mode, audio_filter_scope, audio_filter_updated_by_user_id, audio_filter_updated_at from rooms where id = ?`, roomID).Scan(&currentID, &mode, &rawScope, &updatedBy, &updatedAt); err != nil {
		return fmt.Errorf("read audio ended state: %w", err)
	}
	trackCallerID := ""
	if updatedBy.Valid {
		trackCallerID = updatedBy.String
	}
	tracks, err := h.listAudioTracks(ctx, roomID, trackCallerID, false)
	if err != nil {
		return err
	}
	tracks = audioTracksMatchingScope(tracks, parseAudioFilterScope(rawScope, updatedBy, updatedAt))
	nextID := ""
	if mode == AudioModeRepeatOne {
		nextID = currentID
	} else if len(tracks) > 0 && (mode == AudioModeNext || mode == AudioModeRepeatForward || mode == AudioModePrevious || mode == AudioModeRepeatBackward || mode == AudioModeShuffle) {
		nextID = nextAudioTrackID(roomID, tracks, currentID, mode)
		if nextID == "" && mode == AudioModeRepeatForward {
			nextID = tracks[0].ID
		}
		if nextID == "" && mode == AudioModeRepeatBackward {
			nextID = tracks[len(tracks)-1].ID
		}
	}
	if nextID == "" || mode == AudioModeStop {
		if _, err := h.db.ExecContext(ctx, `update rooms set audio_state = ?, audio_position_seconds = 0, audio_started_at = null where id = ?`, AudioStatePaused, roomID); err != nil {
			return fmt.Errorf("stop ended audio: %w", err)
		}
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.db.ExecContext(ctx, `update rooms set audio_current_track_id = ?, audio_state = ?, audio_position_seconds = 0, audio_started_at = ? where id = ?`, nextID, AudioStatePlaying, now, roomID); err != nil {
		return fmt.Errorf("advance ended audio: %w", err)
	}
	return nil
}

func (h *Hub) setAudioStar(ctx context.Context, roomID string, trackID string, userID string, starred bool) error {
	if err := h.ensureAudioTrack(ctx, roomID, trackID); err != nil {
		return err
	}
	if starred {
		if _, err := h.db.ExecContext(ctx, `insert into room_audio_track_stars (room_id, track_id, user_id, starred_at) values (?, ?, ?, ?) on conflict(room_id, track_id, user_id) do nothing`, roomID, trackID, userID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("star audio track: %w", err)
		}
		return nil
	}
	if _, err := h.db.ExecContext(ctx, `delete from room_audio_track_stars where room_id = ? and track_id = ? and user_id = ?`, roomID, trackID, userID); err != nil {
		return fmt.Errorf("unstar audio track: %w", err)
	}
	return nil
}

func nextAudioTrackID(roomID string, tracks []SnapshotAudioTrack, currentID string, mode string) string {
	if len(tracks) == 0 {
		return ""
	}
	if mode == AudioModeStop {
		return ""
	}
	if mode == AudioModeRepeatOne {
		return currentID
	}
	if mode == AudioModeShuffle {
		return nextShuffledAudioTrackID(roomID, tracks, currentID)
	}
	index := -1
	for i, track := range tracks {
		if track.ID == currentID {
			index = i
			break
		}
	}
	if index < 0 {
		return tracks[0].ID
	}
	if mode == AudioModePrevious {
		if index == 0 {
			return ""
		}
		return tracks[index-1].ID
	}
	if mode == AudioModeRepeatBackward {
		if index == 0 {
			return ""
		}
		return tracks[index-1].ID
	}
	if index+1 >= len(tracks) {
		return ""
	}
	return tracks[index+1].ID
}

func previousAudioTrackID(roomID string, tracks []SnapshotAudioTrack, currentID string, mode string) string {
	if mode == AudioModeShuffle {
		return shuffledAudioTrackID(roomID, tracks, currentID, -1)
	}
	reversed := slices.Clone(tracks)
	slices.Reverse(reversed)
	return nextAudioTrackID(roomID, reversed, currentID, mode)
}

func nextShuffledAudioTrackID(roomID string, tracks []SnapshotAudioTrack, currentID string) string {
	return shuffledAudioTrackID(roomID, tracks, currentID, 1)
}

func shuffledAudioTrackID(roomID string, tracks []SnapshotAudioTrack, currentID string, direction int) string {
	if len(tracks) == 0 {
		return ""
	}
	if len(tracks) == 1 {
		return tracks[0].ID
	}
	shuffled := slices.Clone(tracks)
	slices.SortFunc(shuffled, func(a, b SnapshotAudioTrack) int {
		aKey := deterministicShuffleKey(roomID, a.ID)
		bKey := deterministicShuffleKey(roomID, b.ID)
		if aKey < bKey {
			return -1
		}
		if aKey > bKey {
			return 1
		}
		return cmp.Compare(a.ID, b.ID)
	})
	index := -1
	for i, track := range shuffled {
		if track.ID == currentID {
			index = i
			break
		}
	}
	if index < 0 {
		return shuffled[0].ID
	}
	nextIndex := (index + direction + len(shuffled)) % len(shuffled)
	return shuffled[nextIndex].ID
}

func deterministicShuffleKey(roomID string, trackID string) uint64 {
	sum := sha256.Sum256([]byte(roomID + ":" + trackID))
	return binary.BigEndian.Uint64(sum[:8])
}

func (h *Hub) setMemberAudioTaggingPermission(ctx context.Context, roomID string, userID string, allowTagging bool) error {
	var role string
	err := h.db.QueryRowContext(ctx, `select role from room_members where room_id = ? and user_id = ? and kicked_at is null`, roomID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return rooms.ErrNotMember
	}
	if err != nil {
		return fmt.Errorf("read audio tagging permission member: %w", err)
	}
	if role == rooms.RoleObserver {
		return ErrBadCommand
	}
	if _, err := h.db.ExecContext(ctx, `update room_members set allow_audio_tagging = ? where room_id = ? and user_id = ? and kicked_at is null`, allowTagging, roomID, userID); err != nil {
		return fmt.Errorf("update member audio tagging permission: %w", err)
	}
	return nil
}

func (h *Hub) requireAudioTagging(ctx context.Context, roomID string, trackID string, userID string, role string, isMod bool) (string, error) {
	if isMod {
		return "moderator", nil
	}
	if role == rooms.RoleObserver {
		return "", ErrForbidden
	}
	var roomAllow bool
	var memberAllow bool
	var uploader string
	if err := h.db.QueryRowContext(ctx, `select r.allow_audience_audio_tagging, rm.allow_audio_tagging, rat.uploaded_by_user_id from rooms r join room_members rm on rm.room_id = r.id join room_audio_tracks rat on rat.room_id = r.id where r.id = ? and rm.user_id = ? and rm.kicked_at is null and rat.id = ?`, roomID, userID, trackID).Scan(&roomAllow, &memberAllow, &uploader); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrBadCommand
		}
		return "", fmt.Errorf("read audio tagging permission: %w", err)
	}
	if uploader == userID {
		return "uploader", nil
	}
	if roomAllow || memberAllow {
		return "participant", nil
	}
	return "", ErrForbidden
}

func (h *Hub) setAudioTag(ctx context.Context, roomID string, trackID string, userID string, role string, isMod bool, label string, tagged bool) error {
	if err := h.ensureAudioTrack(ctx, roomID, trackID); err != nil {
		return err
	}
	slug, cleanLabel := normalizeAudioTag(label)
	if slug == "" {
		return ErrBadCommand
	}
	source, err := h.requireAudioTagging(ctx, roomID, trackID, userID, role, isMod)
	if err != nil {
		return err
	}
	if tagged {
		if _, err := h.db.ExecContext(ctx, `insert into room_audio_track_tag_claims (room_id, track_id, tag_slug, tag_label, claimed_by_user_id, claim_source, claimed_at) values (?, ?, ?, ?, ?, ?, ?) on conflict(room_id, track_id, tag_slug, claimed_by_user_id, claim_source) do update set tag_label = excluded.tag_label, claimed_at = excluded.claimed_at`, roomID, trackID, slug, cleanLabel, userID, source, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("tag audio track: %w", err)
		}
		return nil
	}
	if isMod {
		if _, err := h.db.ExecContext(ctx, `delete from room_audio_track_tag_claims where room_id = ? and track_id = ? and tag_slug = ?`, roomID, trackID, slug); err != nil {
			return fmt.Errorf("untag audio track: %w", err)
		}
		return nil
	}
	if _, err := h.db.ExecContext(ctx, `delete from room_audio_track_tag_claims where room_id = ? and track_id = ? and tag_slug = ? and claimed_by_user_id = ? and claim_source = ?`, roomID, trackID, slug, userID, source); err != nil {
		return fmt.Errorf("untag own audio track claim: %w", err)
	}
	return nil
}

func normalizeAudioTag(label string) (string, string) {
	clean := strings.Join(strings.Fields(strings.TrimSpace(label)), " ")
	if clean == "" || len([]rune(clean)) > 40 {
		return "", ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(clean) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "", ""
	}
	return slug, clean
}

func (h *Hub) attachAudioTags(ctx context.Context, roomID string, tracks []SnapshotAudioTrack) error {
	if len(tracks) == 0 {
		return nil
	}
	rows, err := h.db.QueryContext(ctx, `select track_id, tag_slug, tag_label, claimed_by_user_id, claim_source from room_audio_track_tag_claims where room_id = ? order by lower(tag_label), claim_source, claimed_at`, roomID)
	if err != nil {
		return fmt.Errorf("list audio tag claims: %w", err)
	}
	defer rows.Close()
	byTrack := map[string]map[string]*SnapshotAudioTag{}
	for rows.Next() {
		var trackID, slug, label, userID, source string
		if err := rows.Scan(&trackID, &slug, &label, &userID, &source); err != nil {
			return fmt.Errorf("scan audio tag claim: %w", err)
		}
		if byTrack[trackID] == nil {
			byTrack[trackID] = map[string]*SnapshotAudioTag{}
		}
		tag := byTrack[trackID][slug]
		if tag == nil {
			tag = &SnapshotAudioTag{Slug: slug, Label: label, Claims: []SnapshotAudioTagClaim{}}
			byTrack[trackID][slug] = tag
		}
		tag.Claims = append(tag.Claims, SnapshotAudioTagClaim{UserID: userID, Source: source})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate audio tag claims: %w", err)
	}
	for index := range tracks {
		trackTags := byTrack[tracks[index].ID]
		if len(trackTags) == 0 {
			tracks[index].Tags = []SnapshotAudioTag{}
			continue
		}
		for _, tag := range trackTags {
			tracks[index].Tags = append(tracks[index].Tags, *tag)
		}
		slices.SortFunc(tracks[index].Tags, func(a, b SnapshotAudioTag) int {
			return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
		})
	}
	return nil
}

func parseAudioFilterScope(raw string, updatedBy sql.NullString, updatedAt sql.NullString) SnapshotAudioFilterScope {
	var scope SnapshotAudioFilterScope
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &scope)
	}
	scope = normalizeAudioFilterScope(scope)
	if updatedBy.Valid {
		scope.UpdatedByUserID = updatedBy.String
	}
	if updatedAt.Valid {
		scope.UpdatedAt = updatedAt.String
	}
	return scope
}

func marshalAudioFilterScope(scope SnapshotAudioFilterScope) string {
	scope = normalizeAudioFilterScope(scope)
	bytes, err := json.Marshal(scope)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func normalizeAudioFilterScope(scope SnapshotAudioFilterScope) SnapshotAudioFilterScope {
	scope.Search = strings.TrimSpace(scope.Search)
	for i := range scope.IncludeGroups {
		seen := map[string]bool{}
		var tags []string
		for _, tag := range scope.IncludeGroups[i].Tags {
			slug, _ := normalizeAudioTag(tag)
			if slug != "" && !seen[slug] {
				seen[slug] = true
				tags = append(tags, slug)
			}
		}
		scope.IncludeGroups[i].Tags = tags
	}
	groups := scope.IncludeGroups[:0]
	for _, group := range scope.IncludeGroups {
		if len(group.Tags) > 0 {
			groups = append(groups, group)
		}
	}
	scope.IncludeGroups = groups
	seenExclude := map[string]bool{}
	exclude := scope.ExcludeTags[:0]
	for _, tag := range scope.ExcludeTags {
		slug, _ := normalizeAudioTag(tag)
		if slug != "" && !seenExclude[slug] {
			seenExclude[slug] = true
			exclude = append(exclude, slug)
		}
	}
	scope.ExcludeTags = exclude
	return scope
}

func audioTracksMatchingScope(tracks []SnapshotAudioTrack, scope SnapshotAudioFilterScope) []SnapshotAudioTrack {
	scope = normalizeAudioFilterScope(scope)
	if scope.Search == "" && len(scope.IncludeGroups) == 0 && len(scope.ExcludeTags) == 0 && !scope.StarredOnly {
		return tracks
	}
	filtered := make([]SnapshotAudioTrack, 0, len(tracks))
	for _, track := range tracks {
		if audioTrackMatchesScope(track, scope) {
			filtered = append(filtered, track)
		}
	}
	return filtered
}

func audioTrackMatchesScope(track SnapshotAudioTrack, scope SnapshotAudioFilterScope) bool {
	if scope.StarredOnly && !track.StarredByCaller {
		return false
	}
	tagSet := map[string]bool{}
	var labels []string
	for _, tag := range track.Tags {
		tagSet[tag.Slug] = true
		labels = append(labels, tag.Label)
	}
	for _, excluded := range scope.ExcludeTags {
		if tagSet[excluded] {
			return false
		}
	}
	if len(scope.IncludeGroups) > 0 {
		matchedGroup := false
		for _, group := range scope.IncludeGroups {
			matchedAll := true
			for _, tag := range group.Tags {
				if !tagSet[tag] {
					matchedAll = false
					break
				}
			}
			if matchedAll {
				matchedGroup = true
				break
			}
		}
		if !matchedGroup {
			return false
		}
	}
	query := strings.ToLower(strings.TrimSpace(scope.Search))
	if query == "" {
		return true
	}
	searchable := strings.ToLower(strings.Join([]string{track.Title, track.MetadataTitle, track.OriginalName, track.UploadedByName, track.UploaderDisplayName, strings.Join(labels, " ")}, " "))
	return strings.Contains(searchable, query)
}

func currentAudioTrackID(ctx context.Context, db *store.DB, roomID string) string {
	var current sql.NullString
	if err := db.QueryRowContext(ctx, `select audio_current_track_id from rooms where id = ?`, roomID).Scan(&current); err != nil || !current.Valid {
		return ""
	}
	return current.String
}

func validAudioMode(mode string) bool {
	switch mode {
	case AudioModeStop, AudioModeNext, AudioModePrevious, AudioModeRepeatOne, AudioModeRepeatForward, AudioModeRepeatBackward, AudioModeShuffle:
		return true
	default:
		return false
	}
}

func validRoomMode(mode string) bool {
	return mode == RoomModeSlides || mode == RoomModeMarkdown || mode == RoomModeAudio
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

func participantIndex(participants []SnapshotMember, userID string) int {
	for index, participant := range participants {
		if participant.UserID == userID {
			return index
		}
	}
	return -1
}

func nextParticipant(participants []SnapshotMember, currentUserID string, direction int) string {
	if len(participants) == 0 {
		return ""
	}
	index := participantIndex(participants, currentUserID)
	if index < 0 {
		return ""
	}
	next := (index + direction + len(participants)) % len(participants)
	return participants[next].UserID
}

// BroadcastSnapshot sends a fresh snapshot to every client in roomID.
func (h *Hub) BroadcastSnapshot(ctx context.Context, roomID string, requestID string) {
	h.mu.Lock()
	clients := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		if client.RoomID == roomID {
			clients = append(clients, client)
		}
	}
	h.mu.Unlock()
	slices.SortFunc(clients, func(a *Client, b *Client) int {
		if a.UserID < b.UserID {
			return -1
		}
		if a.UserID > b.UserID {
			return 1
		}
		return 0
	})
	for _, client := range clients {
		snapshot, err := h.Snapshot(ctx, roomID, client.UserID)
		if err != nil {
			h.Unregister(client)
			continue
		}
		event := Event{Type: EventSnapshot, RequestID: requestID, RoomID: roomID, Version: h.Version(roomID), Payload: snapshot}
		select {
		case client.Send <- event:
		default:
		}
	}
}

// SendToUser sends one event to connected clients for userID in roomID.
func (h *Hub) SendToUser(roomID string, userID string, event Event) {
	h.mu.Lock()
	clients := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		if client.RoomID == roomID && client.UserID == userID {
			clients = append(clients, client)
		}
	}
	h.mu.Unlock()
	for _, client := range clients {
		select {
		case client.Send <- event:
		default:
		}
	}
}

// Version returns the current in-memory room version.
func (h *Hub) Version(roomID string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.versions[roomID]
}

func (h *Hub) incrementVersion(roomID string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.versions[roomID]++
	return h.versions[roomID]
}

func randomToken(byteCount int) (string, error) {
	bytes := make([]byte, byteCount)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

var (
	ErrInvalidTicket = errors.New("invalid websocket ticket")
	ErrForbidden     = errors.New("forbidden")
	ErrBadCommand    = errors.New("bad command")
	ErrNoBroadcast   = errors.New("no broadcast")
)
