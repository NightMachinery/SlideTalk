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

// Admin is public admin membership metadata.
type Admin struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	CreatedAt   string `json:"createdAt"`
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

// ListAdmins returns current site admins in creation order.
func (s *Service) ListAdmins(ctx context.Context) ([]Admin, error) {
	rows, err := s.db.QueryContext(ctx, `select id, display_name, created_at from users where is_admin = 1 order by created_at asc`)
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	defer rows.Close()
	var admins []Admin
	for rows.Next() {
		var admin Admin
		if err := rows.Scan(&admin.ID, &admin.DisplayName, &admin.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan admin: %w", err)
		}
		admins = append(admins, admin)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admins: %w", err)
	}
	return admins, nil
}

// DemoteAdmin removes one user's site-admin status while preserving recovery.
func (s *Service) DemoteAdmin(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin demote admin: %w", err)
	}
	defer rollback(tx)
	var adminCount int
	if err := tx.QueryRowContext(ctx, `select count(*) from users where is_admin = 1`).Scan(&adminCount); err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	var targetIsAdmin bool
	if err := tx.QueryRowContext(ctx, `select is_admin from users where id = ?`, userID).Scan(&targetIsAdmin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read target admin: %w", err)
	}
	if targetIsAdmin && adminCount <= 1 && !s.AdminTokenExists() {
		return ErrNoAdminRecovery
	}
	if _, err := tx.ExecContext(ctx, `update users set is_admin = 0, updated_at = ? where id = ?`, nowText(), userID); err != nil {
		return fmt.Errorf("demote admin: %w", err)
	}
	return tx.Commit()
}

// DemoteAllAdmins demotes all admins or all admins except caller.
func (s *Service) DemoteAllAdmins(ctx context.Context, callerUserID string, includeSelf bool) error {
	if includeSelf && !s.AdminTokenExists() {
		return ErrNoAdminRecovery
	}
	query := `update users set is_admin = 0, updated_at = ? where is_admin = 1 and id <> ?`
	args := []any{nowText(), callerUserID}
	if includeSelf {
		query = `update users set is_admin = 0, updated_at = ? where is_admin = 1`
		args = []any{nowText()}
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("demote admins: %w", err)
	}
	return nil
}

// AdminTokenExists reports whether the bootstrap token file can recover admin access.
func (s *Service) AdminTokenExists() bool {
	_, err := os.Stat(s.adminTokenPath())
	return err == nil
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
	ErrNoAdminRecovery    = errors.New("admin demotion would leave no recovery path")
)

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}
