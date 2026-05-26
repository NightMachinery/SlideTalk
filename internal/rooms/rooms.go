// Package rooms manages room creation, joins, and membership state.
package rooms

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
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

	RoomModeSlides   = "slides"
	RoomModeMarkdown = "markdown"
	RoomModeAudio    = "audio"

	DefaultRoomRetention  = 7 * 24 * time.Hour
	MaxModeratorRetention = 10 * 24 * time.Hour
	MinAdminRetention     = 24 * time.Hour
)

// Service provides room operations.
type Service struct {
	db               *store.DB
	defaultRetention time.Duration
}

// NewService creates a room service.
func NewService(db *store.DB) *Service {
	return NewServiceWithRetention(db, DefaultRoomRetention)
}

// NewServiceWithRetention creates a room service with a configurable new-room retention period.
func NewServiceWithRetention(db *store.DB, defaultRetention time.Duration) *Service {
	if defaultRetention <= 0 {
		defaultRetention = DefaultRoomRetention
	}
	return &Service{db: db, defaultRetention: defaultRetention}
}

// Room is public room metadata.
type Room struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	HasPassword  bool    `json:"hasPassword"`
	ExpiresAt    *string `json:"expiresAt"`
	NeverExpires bool    `json:"neverExpires"`
}

// Membership describes the caller's room membership.
type Membership struct {
	RoomID       string `json:"roomId"`
	UserID       string `json:"userId"`
	Role         string `json:"role"`
	DisplayOrder int    `json:"displayOrder"`
}

// Member is a room member with profile data.
type Member struct {
	UserID            string
	DisplayName       string
	Role              string
	DisplayOrder      int
	AllowAudioUpload  bool
	AllowAudioControl bool
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
	RoomMode string
}

// JoinInput is the room join payload.
type JoinInput struct {
	Password    string
	MigrationID string
}

// SettingsInput is a partial room settings update.
type SettingsInput struct {
	Title                     *string
	Password                  *string
	ClearPassword             bool
	AllowParticipantMarkdown  *bool
	SharedNavigationEnabled   *bool
	RoomMode                  *string
	AllowAudienceAudioUpload  *bool
	AllowAudienceAudioControl *bool
	RaiseHandMode             *string
}

// MigrationLink is a one-time-visible bearer secret for a future room migration.
type MigrationLink struct {
	RoomID      string `json:"roomId"`
	MigrationID string `json:"migrationId"`
	ExpiresAt   string `json:"expiresAt"`
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
	roomMode := strings.TrimSpace(input.RoomMode)
	if roomMode == "" {
		roomMode = RoomModeSlides
	}
	if !validRoomMode(roomMode) {
		return Room{}, ErrInvalidRoomMode
	}
	id, err := randomID()
	if err != nil {
		return Room{}, err
	}
	now := nowText()
	expiresAt := time.Now().UTC().Add(s.defaultRetention).Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, fmt.Errorf("begin create room: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(
		ctx,
		`insert into rooms (id, title, password_hash, room_mode, markdown, allow_participant_markdown, expires_at, created_by_user_id, created_at, updated_at)
		 values (?, ?, ?, ?, '', 0, ?, ?, ?, ?)`,
		id,
		title,
		nullableString(passwordHash),
		roomMode,
		expiresAt,
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
	return Room{ID: id, Title: title, HasPassword: passwordHash != "", ExpiresAt: &expiresAt}, nil
}

// IssueMigrationLink creates a time-limited room migration bearer secret.
func (s *Service) IssueMigrationLink(ctx context.Context, roomID string, requesterUserID string) (MigrationLink, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MigrationLink{}, fmt.Errorf("begin issue migration link: %w", err)
	}
	defer rollback(tx)
	role, err := memberRole(ctx, tx, roomID, requesterUserID)
	if err != nil {
		return MigrationLink{}, err
	}
	if role != RoleMod {
		return MigrationLink{}, ErrNotModerator
	}
	if _, err := tx.ExecContext(ctx, `delete from room_migration_links where expires_at <= ?`, nowText()); err != nil {
		return MigrationLink{}, fmt.Errorf("delete expired migration links: %w", err)
	}
	migrationID, err := randomToken(32)
	if err != nil {
		return MigrationLink{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(
		ctx,
		`insert into room_migration_links (migration_id_hash, room_id, created_by_user_id, expires_at, created_at)
		 values (?, ?, ?, ?, ?)`,
		hashBearerSecret(migrationID),
		roomID,
		requesterUserID,
		expiresAt,
		now.Format(time.RFC3339Nano),
	); err != nil {
		return MigrationLink{}, fmt.Errorf("insert migration link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return MigrationLink{}, fmt.Errorf("commit migration link: %w", err)
	}
	return MigrationLink{RoomID: roomID, MigrationID: migrationID, ExpiresAt: expiresAt}, nil
}

func (s *Service) validMigrationLinkForTest(ctx context.Context, roomID string, migrationID string) bool {
	return s.validMigrationLink(ctx, roomID, migrationID)
}

func (s *Service) validMigrationLink(ctx context.Context, roomID string, migrationID string) bool {
	var exists int
	err := s.db.QueryRowContext(
		ctx,
		`select 1 from room_migration_links where room_id = ? and migration_id_hash = ? and expires_at > ?`,
		roomID,
		hashBearerSecret(migrationID),
		nowText(),
	).Scan(&exists)
	return err == nil && exists == 1
}

// Join joins or rejoins a room.
func (s *Service) Join(ctx context.Context, roomID string, userID string, input JoinInput) (Membership, error) {
	if err := s.requireDisplayName(ctx, userID); err != nil {
		return Membership{}, err
	}
	var existing Membership
	var kickedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `select room_id, user_id, role, display_order, kicked_at from room_members where room_id = ? and user_id = ?`, roomID, userID).Scan(&existing.RoomID, &existing.UserID, &existing.Role, &existing.DisplayOrder, &kickedAt)
	if err == nil {
		if kickedAt.Valid {
			return Membership{}, ErrKicked
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Membership{}, fmt.Errorf("read membership: %w", err)
	}

	var passwordHash sql.NullString
	err = s.db.QueryRowContext(ctx, `select password_hash from rooms where id = ?`, roomID).Scan(&passwordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Membership{}, ErrNotFound
		}
		return Membership{}, fmt.Errorf("read room password: %w", err)
	}
	if passwordHash.Valid && bcrypt.CompareHashAndPassword([]byte(passwordHash.String), []byte(input.Password)) != nil {
		if strings.TrimSpace(input.MigrationID) == "" || !s.validMigrationLink(ctx, roomID, input.MigrationID) {
			return Membership{}, ErrInvalidPassword
		}
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
	var expiresAt sql.NullString
	err := s.db.QueryRowContext(ctx, `select id, title, password_hash, expires_at from rooms where id = ?`, roomID).Scan(&details.Room.ID, &details.Room.Title, &passwordHash, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Details{}, ErrNotFound
		}
		return Details{}, fmt.Errorf("read room: %w", err)
	}
	details.Room.HasPassword = passwordHash.Valid
	if expiresAt.Valid {
		details.Room.ExpiresAt = &expiresAt.String
	} else {
		details.Room.NeverExpires = true
	}
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

// UpdateSettings applies moderator-controlled room settings.
func (s *Service) UpdateSettings(ctx context.Context, roomID string, input SettingsInput) (Room, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, fmt.Errorf("begin update room settings: %w", err)
	}
	defer rollback(tx)

	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" || len([]rune(title)) > 120 {
			return Room{}, ErrInvalidTitle
		}
		if _, err := tx.ExecContext(ctx, `update rooms set title = ?, updated_at = ? where id = ?`, title, nowText(), roomID); err != nil {
			return Room{}, fmt.Errorf("update room title: %w", err)
		}
	}
	if input.Password != nil {
		passwordHash, err := hashPassword(*input.Password)
		if err != nil {
			return Room{}, err
		}
		if _, err := tx.ExecContext(ctx, `update rooms set password_hash = ?, updated_at = ? where id = ?`, nullableString(passwordHash), nowText(), roomID); err != nil {
			return Room{}, fmt.Errorf("update room password: %w", err)
		}
	}
	if input.ClearPassword {
		if _, err := tx.ExecContext(ctx, `update rooms set password_hash = null, updated_at = ? where id = ?`, nowText(), roomID); err != nil {
			return Room{}, fmt.Errorf("clear room password: %w", err)
		}
	}
	if input.AllowParticipantMarkdown != nil {
		if _, err := tx.ExecContext(ctx, `update rooms set allow_participant_markdown = ?, updated_at = ? where id = ?`, *input.AllowParticipantMarkdown, nowText(), roomID); err != nil {
			return Room{}, fmt.Errorf("update participant markdown: %w", err)
		}
	}
	if input.SharedNavigationEnabled != nil {
		if _, err := tx.ExecContext(ctx, `update rooms set shared_navigation_enabled = ?, updated_at = ? where id = ?`, *input.SharedNavigationEnabled, nowText(), roomID); err != nil {
			return Room{}, fmt.Errorf("update shared navigation: %w", err)
		}
	}
	if input.RoomMode != nil {
		mode := strings.TrimSpace(*input.RoomMode)
		if !validRoomMode(mode) {
			return Room{}, ErrInvalidRoomMode
		}
		if _, err := tx.ExecContext(ctx, `update rooms set room_mode = ?, updated_at = ? where id = ?`, mode, nowText(), roomID); err != nil {
			return Room{}, fmt.Errorf("update room mode: %w", err)
		}
	}
	if input.AllowAudienceAudioUpload != nil {
		if _, err := tx.ExecContext(ctx, `update rooms set allow_audience_audio_upload = ?, updated_at = ? where id = ?`, *input.AllowAudienceAudioUpload, nowText(), roomID); err != nil {
			return Room{}, fmt.Errorf("update audience audio upload: %w", err)
		}
	}
	if input.AllowAudienceAudioControl != nil {
		if _, err := tx.ExecContext(ctx, `update rooms set allow_audience_audio_control = ?, updated_at = ? where id = ?`, *input.AllowAudienceAudioControl, nowText(), roomID); err != nil {
			return Room{}, fmt.Errorf("update audience audio control: %w", err)
		}
	}
	if input.RaiseHandMode != nil {
		mode := strings.TrimSpace(*input.RaiseHandMode)
		if mode != "off" && mode != "manual" && mode != "queue" {
			return Room{}, ErrInvalidRaiseHandMode
		}
		if _, err := tx.ExecContext(ctx, `update rooms set raise_hand_mode = ?, updated_at = ? where id = ?`, mode, nowText(), roomID); err != nil {
			return Room{}, fmt.Errorf("update raise hand mode: %w", err)
		}
		if mode == "off" {
			if _, err := tx.ExecContext(ctx, `delete from raised_hands where room_id = ?`, roomID); err != nil {
				return Room{}, fmt.Errorf("clear hands: %w", err)
			}
		}
	}

	var room Room
	var passwordHash sql.NullString
	var expiresAt sql.NullString
	if err := tx.QueryRowContext(ctx, `select id, title, password_hash, expires_at from rooms where id = ?`, roomID).Scan(&room.ID, &room.Title, &passwordHash, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Room{}, ErrNotFound
		}
		return Room{}, fmt.Errorf("read updated room: %w", err)
	}
	room.HasPassword = passwordHash.Valid
	if expiresAt.Valid {
		room.ExpiresAt = &expiresAt.String
	} else {
		room.NeverExpires = true
	}
	if err := tx.Commit(); err != nil {
		return Room{}, fmt.Errorf("commit room settings: %w", err)
	}
	return room, nil
}

// UpdateRetention changes when a room's uploaded content is eligible for cleanup.
func (s *Service) UpdateRetention(ctx context.Context, roomID string, requesterUserID string, requesterIsAdmin bool, expiresAt time.Time, neverExpires bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update retention: %w", err)
	}
	defer rollback(tx)
	role, err := memberRole(ctx, tx, roomID, requesterUserID)
	if err != nil {
		return err
	}
	var current sql.NullString
	if err := tx.QueryRowContext(ctx, `select expires_at from rooms where id = ?`, roomID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read room retention: %w", err)
	}
	now := time.Now().UTC()
	if neverExpires {
		if !requesterIsAdmin {
			return ErrRetentionAdminOnly
		}
		if _, err := tx.ExecContext(ctx, `update rooms set expires_at = null, updated_at = ? where id = ?`, now.Format(time.RFC3339Nano), roomID); err != nil {
			return fmt.Errorf("set room never expires: %w", err)
		}
		return tx.Commit()
	}
	if role != RoleMod && !requesterIsAdmin {
		return ErrNotModerator
	}
	expiresAt = expiresAt.UTC()
	if requesterIsAdmin {
		if expiresAt.Before(now.Add(MinAdminRetention)) {
			return ErrRetentionTooSoon
		}
	} else {
		if expiresAt.After(now.Add(MaxModeratorRetention)) {
			return ErrRetentionTooLong
		}
		if current.Valid {
			currentTime, err := time.Parse(time.RFC3339Nano, current.String)
			if err != nil {
				return ErrInvalidRetention
			}
			if expiresAt.Before(currentTime) {
				return ErrRetentionShortening
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `update rooms set expires_at = ?, updated_at = ? where id = ?`, expiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), roomID); err != nil {
		return fmt.Errorf("update room retention: %w", err)
	}
	return tx.Commit()
}

// ListMembers returns non-kicked room members in display order.
func (s *Service) ListMembers(ctx context.Context, roomID string) ([]Member, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`select u.id, u.display_name, rm.role, rm.display_order, rm.allow_audio_upload, rm.allow_audio_control
		 from room_members rm
		 join users u on u.id = rm.user_id
		 where rm.room_id = ? and rm.kicked_at is null
		 order by rm.display_order asc, rm.joined_at asc`,
		roomID,
	)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()
	var members []Member
	for rows.Next() {
		var member Member
		if err := rows.Scan(&member.UserID, &member.DisplayName, &member.Role, &member.DisplayOrder, &member.AllowAudioUpload, &member.AllowAudioControl); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate members: %w", err)
	}
	return members, nil
}

// SetRole changes a member role while preserving at least one moderator.
func (s *Service) SetRole(ctx context.Context, roomID string, userID string, role string) error {
	if role != RoleMod && role != RoleParticipant && role != RoleObserver {
		return ErrInvalidRole
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin set role: %w", err)
	}
	defer rollback(tx)
	currentRole, err := memberRole(ctx, tx, roomID, userID)
	if err != nil {
		return err
	}
	if currentRole == RoleMod && role != RoleMod {
		count, err := modCount(ctx, tx, roomID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastMod
		}
	}
	if role == RoleObserver {
		if _, err := tx.ExecContext(ctx, `update room_members set role = ?, allow_audio_upload = 0, allow_audio_control = 0 where room_id = ? and user_id = ? and kicked_at is null`, role, roomID, userID); err != nil {
			return fmt.Errorf("update role: %w", err)
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `update room_members set role = ? where room_id = ? and user_id = ? and kicked_at is null`, role, roomID, userID); err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	return tx.Commit()
}

// Reorder writes contiguous display order for active participants and observers.
func (s *Service) Reorder(ctx context.Context, roomID string, participantIDs []string, observerIDs []string) error {
	members, err := s.ListMembers(ctx, roomID)
	if err != nil {
		return err
	}
	active := make(map[string]string, len(members))
	for _, member := range members {
		active[member.UserID] = member.Role
	}
	seen := make(map[string]bool, len(members))
	allIDs := append(append([]string{}, participantIDs...), observerIDs...)
	if len(allIDs) != len(members) {
		return ErrInvalidReorder
	}
	for _, userID := range allIDs {
		if active[userID] == "" || seen[userID] {
			return ErrInvalidReorder
		}
		seen[userID] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reorder: %w", err)
	}
	defer rollback(tx)
	order := 0
	for _, userID := range participantIDs {
		if _, err := tx.ExecContext(ctx, `update room_members set role = ?, display_order = ? where room_id = ? and user_id = ?`, participantRoleFor(active[userID]), order, roomID, userID); err != nil {
			return fmt.Errorf("update participant order: %w", err)
		}
		order++
	}
	for _, userID := range observerIDs {
		if _, err := tx.ExecContext(ctx, `update room_members set role = ?, display_order = ?, allow_audio_upload = 0, allow_audio_control = 0 where room_id = ? and user_id = ?`, RoleObserver, order, roomID, userID); err != nil {
			return fmt.Errorf("update observer order: %w", err)
		}
		order++
	}
	return tx.Commit()
}

// Kick marks a member removed while preserving at least one moderator.
func (s *Service) Kick(ctx context.Context, roomID string, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin kick: %w", err)
	}
	defer rollback(tx)
	role, err := memberRole(ctx, tx, roomID, userID)
	if err != nil {
		return err
	}
	if role == RoleMod {
		count, err := modCount(ctx, tx, roomID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastMod
		}
	}
	if _, err := tx.ExecContext(ctx, `update room_members set kicked_at = ? where room_id = ? and user_id = ?`, nowText(), roomID, userID); err != nil {
		return fmt.Errorf("kick member: %w", err)
	}
	return tx.Commit()
}

// MarkKickedForTest records a kicked member for join-path tests.
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

func hashBearerSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

var (
	ErrInvalidTitle         = errors.New("room title must be 1 to 120 characters")
	ErrInvalidRoomMode      = errors.New("invalid room mode")
	ErrDisplayNameRequired  = errors.New("display name required")
	ErrInvalidPassword      = errors.New("invalid room password")
	ErrKicked               = errors.New("member was kicked")
	ErrNotFound             = errors.New("not found")
	ErrNotMember            = errors.New("not a room member")
	ErrInvalidRole          = errors.New("invalid member role")
	ErrInvalidReorder       = errors.New("invalid member order")
	ErrLastMod              = errors.New("cannot remove the last moderator")
	ErrInvalidRaiseHandMode = errors.New("invalid raise hand mode")
	ErrNotModerator         = errors.New("not a room moderator")
	ErrInvalidRetention     = errors.New("invalid room retention")
	ErrRetentionAdminOnly   = errors.New("only site admins can set rooms to never expire")
	ErrRetentionTooSoon     = errors.New("room survival is too soon")
	ErrRetentionTooLong     = errors.New("room survival is too long")
	ErrRetentionShortening  = errors.New("room survival cannot be shortened by moderators")
)

func validRoomMode(mode string) bool {
	return mode == RoomModeSlides || mode == RoomModeMarkdown || mode == RoomModeAudio
}

func memberRole(ctx context.Context, tx *sql.Tx, roomID string, userID string) (string, error) {
	var role string
	err := tx.QueryRowContext(ctx, `select role from room_members where room_id = ? and user_id = ? and kicked_at is null`, roomID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotMember
	}
	if err != nil {
		return "", fmt.Errorf("read member role: %w", err)
	}
	return role, nil
}

func modCount(ctx context.Context, tx *sql.Tx, roomID string) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `select count(*) from room_members where room_id = ? and role = ? and kicked_at is null`, roomID, RoleMod).Scan(&count); err != nil {
		return 0, fmt.Errorf("count mods: %w", err)
	}
	return count, nil
}

func participantRoleFor(current string) string {
	if current == RoleMod {
		return RoleMod
	}
	return RoleParticipant
}
