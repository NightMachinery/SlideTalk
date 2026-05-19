// Package auth manages local-token users and bootstrap admin promotion.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/store"
)

// User is a SlideTalk browser identity.
type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	IsAdmin     bool   `json:"isAdmin"`
}

// Service provides user and bootstrap-admin operations.
type Service struct {
	db      *store.DB
	dataDir string
}

// NewService creates an auth service.
func NewService(db *store.DB, dataDir string) *Service {
	return &Service{db: db, dataDir: dataDir}
}

// EnsureAdminToken creates the bootstrap admin token file if missing.
func (s *Service) EnsureAdminToken(_ context.Context) error {
	path := s.adminTokenPath()
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat admin token: %w", err)
	}
	token, err := randomToken(32)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	return os.WriteFile(path, []byte(token), 0o600)
}

// EnsureUser returns the user for rawToken, creating it when first seen.
func (s *Service) EnsureUser(ctx context.Context, rawToken string) (User, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return User{}, ErrUnauthorized
	}
	tokenHash := hashToken(rawToken)
	now := nowText()
	id, err := randomToken(18)
	if err != nil {
		return User{}, err
	}
	_, err = s.db.ExecContext(
		ctx,
		`insert into users (id, token_hash, display_name, is_admin, created_at, updated_at)
		 values (?, ?, '', 0, ?, ?)
		 on conflict(token_hash) do nothing`,
		id,
		tokenHash,
		now,
		now,
	)
	if err != nil {
		return User{}, fmt.Errorf("ensure user: %w", err)
	}
	var user User
	if err := s.db.QueryRowContext(ctx, `select id, display_name, is_admin from users where token_hash = ?`, tokenHash).Scan(&user.ID, &user.DisplayName, &user.IsAdmin); err != nil {
		return User{}, fmt.Errorf("read user: %w", err)
	}
	return user, nil
}

// GetUser reads a user by ID.
func (s *Service) GetUser(ctx context.Context, id string) (User, error) {
	var user User
	err := s.db.QueryRowContext(ctx, `select id, display_name, is_admin from users where id = ?`, id).Scan(&user.ID, &user.DisplayName, &user.IsAdmin)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return user, nil
}

// UpdateDisplayName validates and saves a user's display name.
func (s *Service) UpdateDisplayName(ctx context.Context, userID string, displayName string) error {
	name := strings.TrimSpace(displayName)
	if name == "" || len([]rune(name)) > 80 {
		return ErrInvalidDisplayName
	}
	result, err := s.db.ExecContext(ctx, `update users set display_name = ?, updated_at = ? where id = ?`, name, nowText(), userID)
	if err != nil {
		return fmt.Errorf("update display name: %w", err)
	}
	if changed, err := result.RowsAffected(); err == nil && changed == 0 {
		return ErrNotFound
	}
	return nil
}

// PromoteWithAdminToken promotes a user when submittedToken matches the bootstrap token.
func (s *Service) PromoteWithAdminToken(ctx context.Context, userID string, submittedToken string) (bool, error) {
	expected, err := os.ReadFile(s.adminTokenPath())
	if err != nil {
		return false, fmt.Errorf("read admin token: %w", err)
	}
	submitted := strings.TrimSpace(submittedToken)
	expectedToken := strings.TrimSpace(string(expected))
	if subtle.ConstantTimeCompare([]byte(submitted), []byte(expectedToken)) != 1 {
		return false, nil
	}
	if _, err := s.db.ExecContext(ctx, `update users set is_admin = 1, updated_at = ? where id = ?`, nowText(), userID); err != nil {
		return false, fmt.Errorf("promote admin: %w", err)
	}
	return true, nil
}

func (s *Service) adminTokenPath() string {
	return filepath.Join(s.dataDir, "admin_token")
}

func hashToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
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

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrNotFound           = errors.New("not found")
	ErrInvalidDisplayName = errors.New("display name must be 1 to 80 characters")
)
