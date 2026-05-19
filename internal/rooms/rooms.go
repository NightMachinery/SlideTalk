// Package rooms manages room creation, joins, and membership state.
package rooms

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	RoleMod         = "mod"
	RoleParticipant = "participant"
	RoleObserver    = "observer"
)

// Service provides room operations.
type Service struct {
	db *store.DB
}

// NewService creates a room service.
func NewService(db *store.DB) *Service {
	return &Service{db: db}
}

// Room is public room metadata.
type Room struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	HasPassword bool   `json:"hasPassword"`
}

// Membership describes the caller's room membership.
type Membership struct {
	RoomID       string `json:"roomId"`
	UserID       string `json:"userId"`
	Role         string `json:"role"`
	DisplayOrder int    `json:"displayOrder"`
}

// Details contains room metadata and caller membership.
type Details struct {
	Room       Room       `json:"room"`
	Membership Membership `json:"membership"`
}

// CreateInput is the room creation payload.
type CreateInput struct {
	Title    string
	Password string
}

// JoinInput is the room join payload.
type JoinInput struct {
	Password string
}

// Create creates a room and makes the creator a moderator.
func (s *Service) Create(ctx context.Context, creatorUserID string, input CreateInput) (Room, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" || len([]rune(title)) > 120 {
		return Room{}, ErrInvalidTitle
	}
	if err := s.requireDisplayName(ctx, creatorUserID); err != nil {
		return Room{}, err
	}
	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		return Room{}, err
	}
	id, err := randomID()
	if err != nil {
		return Room{}, err
	}
	now := nowText()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, fmt.Errorf("begin create room: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(
		ctx,
		`insert into rooms (id, title, password_hash, no_slide_mode, markdown, allow_participant_markdown, created_by_user_id, created_at, updated_at)
		 values (?, ?, ?, 0, '', 0, ?, ?, ?)`,
		id,
		title,
		nullableString(passwordHash),
		creatorUserID,
		now,
		now,
	); err != nil {
		return Room{}, fmt.Errorf("insert room: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`insert into room_members (room_id, user_id, role, display_order, joined_at) values (?, ?, ?, 0, ?)`,
		id,
		creatorUserID,
		RoleMod,
		now,
	); err != nil {
		return Room{}, fmt.Errorf("insert creator membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Room{}, fmt.Errorf("commit create room: %w", err)
	}
	return Room{ID: id, Title: title, HasPassword: passwordHash != ""}, nil
}

// Join joins or rejoins a room.
func (s *Service) Join(ctx context.Context, roomID string, userID string, input JoinInput) (Membership, error) {
	if err := s.requireDisplayName(ctx, userID); err != nil {
		return Membership{}, err
	}
	var passwordHash sql.NullString
	err := s.db.QueryRowContext(ctx, `select password_hash from rooms where id = ?`, roomID).Scan(&passwordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Membership{}, ErrNotFound
		}
		return Membership{}, fmt.Errorf("read room password: %w", err)
	}
	if passwordHash.Valid && bcrypt.CompareHashAndPassword([]byte(passwordHash.String), []byte(input.Password)) != nil {
		return Membership{}, ErrInvalidPassword
	}

	var existing Membership
	var kickedAt sql.NullString
	err = s.db.QueryRowContext(ctx, `select room_id, user_id, role, display_order, kicked_at from room_members where room_id = ? and user_id = ?`, roomID, userID).Scan(&existing.RoomID, &existing.UserID, &existing.Role, &existing.DisplayOrder, &kickedAt)
	if err == nil {
		if kickedAt.Valid {
			return Membership{}, ErrKicked
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Membership{}, fmt.Errorf("read membership: %w", err)
	}

	var nextOrder int
	if err := s.db.QueryRowContext(ctx, `select coalesce(max(display_order) + 1, 0) from room_members where room_id = ?`, roomID).Scan(&nextOrder); err != nil {
		return Membership{}, fmt.Errorf("next display order: %w", err)
	}
	now := nowText()
	if _, err := s.db.ExecContext(ctx, `insert into room_members (room_id, user_id, role, display_order, joined_at) values (?, ?, ?, ?, ?)`, roomID, userID, RoleParticipant, nextOrder, now); err != nil {
		return Membership{}, fmt.Errorf("insert membership: %w", err)
	}
	return Membership{RoomID: roomID, UserID: userID, Role: RoleParticipant, DisplayOrder: nextOrder}, nil
}

// GetForUser returns room metadata with the caller membership.
func (s *Service) GetForUser(ctx context.Context, roomID string, userID string) (Details, error) {
	var details Details
	var passwordHash sql.NullString
	err := s.db.QueryRowContext(ctx, `select id, title, password_hash from rooms where id = ?`, roomID).Scan(&details.Room.ID, &details.Room.Title, &passwordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Details{}, ErrNotFound
		}
		return Details{}, fmt.Errorf("read room: %w", err)
	}
	details.Room.HasPassword = passwordHash.Valid
	var kickedAt sql.NullString
	err = s.db.QueryRowContext(ctx, `select room_id, user_id, role, display_order, kicked_at from room_members where room_id = ? and user_id = ?`, roomID, userID).Scan(&details.Membership.RoomID, &details.Membership.UserID, &details.Membership.Role, &details.Membership.DisplayOrder, &kickedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Details{}, ErrNotMember
		}
		return Details{}, fmt.Errorf("read membership: %w", err)
	}
	if kickedAt.Valid {
		return Details{}, ErrKicked
	}
	return details, nil
}

// MarkKickedForTest records a kicked member. Realtime mod controls replace this in the next milestone.
func (s *Service) MarkKickedForTest(ctx context.Context, roomID string, userID string) error {
	_, err := s.db.ExecContext(ctx, `update room_members set kicked_at = ? where room_id = ? and user_id = ?`, nowText(), roomID, userID)
	return err
}

func (s *Service) requireDisplayName(ctx context.Context, userID string) error {
	var displayName string
	err := s.db.QueryRowContext(ctx, `select display_name from users where id = ?`, userID).Scan(&displayName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read display name: %w", err)
	}
	if strings.TrimSpace(displayName) == "" {
		return ErrDisplayNameRequired
	}
	return nil
}

func hashPassword(password string) (string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return "", nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func randomID() (string, error) {
	return randomToken(12)
}

func randomToken(byteCount int) (string, error) {
	bytes := make([]byte, byteCount)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

var (
	ErrInvalidTitle        = errors.New("room title must be 1 to 120 characters")
	ErrDisplayNameRequired = errors.New("display name required")
	ErrInvalidPassword     = errors.New("invalid room password")
	ErrKicked              = errors.New("member was kicked")
	ErrNotFound            = errors.New("not found")
	ErrNotMember           = errors.New("not a room member")
)
