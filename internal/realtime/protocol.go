// Package realtime coordinates live room state over WebSockets.
package realtime

import (
	"encoding/json"
	"time"
)

const (
	EventSnapshot = "room.snapshot"
	EventError    = "error"

	CommandPeopleReorder = "people.reorder"
	CommandPeopleSetRole = "people.setRole"
	CommandPeopleKick    = "people.kick"
)

// Command is the client-to-server realtime envelope.
type Command struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId"`
	Payload   json.RawMessage `json:"payload"`
}

// Event is the server-to-client realtime envelope.
type Event struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId,omitempty"`
	RoomID    string `json:"roomId,omitempty"`
	Version   int64  `json:"version,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Payload   any    `json:"payload,omitempty"`
}

// Snapshot is the full room state sent to a connected client.
type Snapshot struct {
	Room         SnapshotRoom     `json:"room"`
	Caller       SnapshotCaller   `json:"caller"`
	Participants []SnapshotMember `json:"participants"`
	Observers    []SnapshotMember `json:"observers"`
	CurrentTurn  any              `json:"currentTurn"`
	Timer        any              `json:"timer"`
	Hands        []any            `json:"hands"`
	Slide        any              `json:"slide"`
	Markdown     string           `json:"markdown"`
}

// SnapshotRoom is room metadata in realtime snapshots.
type SnapshotRoom struct {
	ID                       string `json:"id"`
	Title                    string `json:"title"`
	NoSlideMode              bool   `json:"noSlideMode"`
	AllowParticipantMarkdown bool   `json:"allowParticipantMarkdown"`
}

// SnapshotCaller identifies the receiving user.
type SnapshotCaller struct {
	UserID  string `json:"userId"`
	Role    string `json:"role"`
	IsAdmin bool   `json:"isAdmin"`
}

// SnapshotMember is a participant or observer row.
type SnapshotMember struct {
	UserID       string `json:"userId"`
	DisplayName  string `json:"displayName"`
	Role         string `json:"role"`
	DisplayOrder int    `json:"displayOrder"`
}

// WSTicket is a short-lived room-scoped connection token.
type WSTicket struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expiresAt"`
}
