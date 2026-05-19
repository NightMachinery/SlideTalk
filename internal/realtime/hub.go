package realtime

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
		NoSlideMode              bool
		AllowParticipantMarkdown bool
		Markdown                 string
	}
	if err := h.db.QueryRowContext(ctx, `select id, title, no_slide_mode, allow_participant_markdown, markdown from rooms where id = ?`, roomID).Scan(&roomRow.ID, &roomRow.Title, &roomRow.NoSlideMode, &roomRow.AllowParticipantMarkdown, &roomRow.Markdown); err != nil {
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
			NoSlideMode:              roomRow.NoSlideMode,
			AllowParticipantMarkdown: roomRow.AllowParticipantMarkdown,
		},
		Caller: SnapshotCaller{
			UserID:  callerUserID,
			Role:    details.Membership.Role,
			IsAdmin: caller.IsAdmin,
		},
		Participants: []SnapshotMember{},
		Observers:    []SnapshotMember{},
		Hands:        []any{},
		Markdown:     roomRow.Markdown,
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
	return snapshot, nil
}

// HandleCommand applies one realtime command.
func (h *Hub) HandleCommand(ctx context.Context, roomID string, callerUserID string, command Command) error {
	details, err := h.rooms.GetForUser(ctx, roomID, callerUserID)
	if err != nil {
		return err
	}
	if details.Membership.Role != rooms.RoleMod {
		return ErrForbidden
	}
	switch command.Type {
	case CommandPeopleReorder:
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
		if err := h.rooms.SetRole(ctx, roomID, payload.UserID, payload.Role); err != nil {
			return err
		}
	case CommandPeopleKick:
		var payload struct {
			UserID string `json:"userId"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return ErrBadCommand
		}
		if err := h.rooms.Kick(ctx, roomID, payload.UserID); err != nil {
			return err
		}
	default:
		return ErrBadCommand
	}
	h.incrementVersion(roomID)
	return nil
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
)
