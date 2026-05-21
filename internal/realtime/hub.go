package realtime

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
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

	mu       sync.Mutex
	tickets  map[string]TicketClaims
	versions map[string]int64
	clients  map[*Client]bool
}

// TicketClaims bind a WebSocket ticket to one room and user.
type TicketClaims struct {
	RoomID    string
	UserID    string
	ExpiresAt time.Time
}

// NewHub creates a realtime hub.
func NewHub(db *store.DB, authService *auth.Service, roomService *rooms.Service) *Hub {
	return &Hub{
		db:       db,
		auth:     authService,
		rooms:    roomService,
		tickets:  make(map[string]TicketClaims),
		versions: make(map[string]int64),
		clients:  make(map[*Client]bool),
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
}

// Unregister removes a socket client from the hub.
func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[client] {
		delete(h.clients, client)
		close(client.Send)
	}
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
		ID                       string
		Title                    string
		PasswordHash             sql.NullString
		NoSlideMode              bool
		AllowParticipantMarkdown bool
		SlidePage                int
		SharedNavigationEnabled  bool
		Markdown                 string
		MarkdownUpdatedByUserID  sql.NullString
		MarkdownUpdatedByName    sql.NullString
		MarkdownUpdatedAt        sql.NullString
		CurrentSpeakerUserID     sql.NullString
		TimerState               string
		TimerDurationSeconds     int
		TimerStartedAt           sql.NullString
		RaiseHandMode            string
	}
	if err := h.db.QueryRowContext(ctx, `select r.id, r.title, r.password_hash, r.no_slide_mode, r.allow_participant_markdown, r.slide_page, r.shared_navigation_enabled, r.markdown, r.markdown_updated_by_user_id, u.display_name, r.markdown_updated_at, r.current_speaker_user_id, r.timer_state, r.timer_duration_seconds, r.timer_started_at, r.raise_hand_mode from rooms r left join users u on u.id = r.markdown_updated_by_user_id where r.id = ?`, roomID).Scan(&roomRow.ID, &roomRow.Title, &roomRow.PasswordHash, &roomRow.NoSlideMode, &roomRow.AllowParticipantMarkdown, &roomRow.SlidePage, &roomRow.SharedNavigationEnabled, &roomRow.Markdown, &roomRow.MarkdownUpdatedByUserID, &roomRow.MarkdownUpdatedByName, &roomRow.MarkdownUpdatedAt, &roomRow.CurrentSpeakerUserID, &roomRow.TimerState, &roomRow.TimerDurationSeconds, &roomRow.TimerStartedAt, &roomRow.RaiseHandMode); err != nil {
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
			ID:                       roomRow.ID,
			Title:                    roomRow.Title,
			HasPassword:              roomRow.PasswordHash.Valid,
			NoSlideMode:              roomRow.NoSlideMode,
			AllowParticipantMarkdown: roomRow.AllowParticipantMarkdown,
			RaiseHandMode:            roomRow.RaiseHandMode,
			SlidePage:                roomRow.SlidePage,
			SharedNavigationEnabled:  roomRow.SharedNavigationEnabled,
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
			UserID:       member.UserID,
			DisplayName:  member.DisplayName,
			Role:         member.Role,
			DisplayOrder: member.DisplayOrder,
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
		if !isMod {
			return ErrForbidden
		}
		var payload struct {
			UserID string `json:"userId"`
			Role   string `json:"role"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if err := h.rooms.SetRole(ctx, roomID, payload.UserID, payload.Role); err != nil {
			return err
		}
		if err := h.cleanMemberTurnState(ctx, roomID, payload.UserID); err != nil {
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
			RaiseHandMode            *string `json:"raiseHandMode"`
			SharedNavigationEnabled  *bool   `json:"sharedNavigationEnabled"`
			NoSlideMode              *bool   `json:"noSlideMode"`
			AllowParticipantMarkdown *bool   `json:"allowParticipantMarkdown"`
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
		if payload.NoSlideMode != nil {
			if _, err := h.db.ExecContext(ctx, `update rooms set no_slide_mode = ? where id = ?`, *payload.NoSlideMode, roomID); err != nil {
				return fmt.Errorf("update no slide mode: %w", err)
			}
		}
		if payload.AllowParticipantMarkdown != nil {
			if _, err := h.db.ExecContext(ctx, `update rooms set allow_participant_markdown = ? where id = ?`, *payload.AllowParticipantMarkdown, roomID); err != nil {
				return fmt.Errorf("update participant markdown setting: %w", err)
			}
		}
		if payload.RaiseHandMode != nil && *payload.RaiseHandMode == RaiseHandModeOff {
			if _, err := h.db.ExecContext(ctx, `delete from raised_hands where room_id = ?`, roomID); err != nil {
				return fmt.Errorf("clear hands: %w", err)
			}
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
	var hands []SnapshotHand
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
		`select rs.sha256, rs.original_name, rs.expires_at, sf.stored_path, sf.missing_at
		 from room_slides rs
		 join slide_files sf on sf.sha256 = rs.sha256
		 where rs.room_id = ?`,
		roomID,
	).Scan(&slide.SHA256, &slide.OriginalName, &slide.ExpiresAt, &storedPath, &missingAt)
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
